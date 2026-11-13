// Package daemon owns one VPN session: the engine, the local proxy, the
// optional full tunnel with its kill switch, byte counters and the state
// machine a UI shows.
//
// Interface: New, Session.Start, Stop, Wait, Snapshot, Subscribe, Servers.
// Both the foreground CLI and the privileged helper call it, so there is one
// code path from config to tunnel. Reconnect backoff, ordering of kill switch
// before engine, counting and state derivation are hidden.
package daemon

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"sync"
	"sync/atomic"
	"time"

	"github.com/payam-foundation/dotvpn-client/client"
	"github.com/payam-foundation/dotvpn-client/internal/app"
	"github.com/payam-foundation/dotvpn-client/internal/config"
	"github.com/payam-foundation/dotvpn-client/internal/diag"
	"github.com/payam-foundation/dotvpn-client/internal/ipc"
	"github.com/payam-foundation/dotvpn-client/internal/router"
	"github.com/payam-foundation/dotvpn-client/internal/tun"
	"github.com/payam-foundation/dotvpn-wire/transport"
)

// Options is one run of the session.
type Options struct {
	Config *config.Config
	// Dir holds state.json and the server list cache (must be writable).
	Dir string
	// Diag is an optional L0 event ring (hashed endpoint ids only).
	Diag *diag.Ring
}

// ErrRunning is returned by Start while a session is active.
var ErrRunning = errors.New("daemon: already running")

// ErrNotConfigured is returned by Start when the config has no way to reach a
// node.
var ErrNotConfigured = errors.New("daemon: no endpoints: import an invite or configure endpoints")

// engineFunc builds the engine; tests replace it.
type engineFunc func(ctx context.Context, o Options, opts transport.Options, ctl *http.Client, onState func(client.Info)) (*client.Engine, error)

// Session runs at most one connection at a time and can be restarted.
type Session struct {
	log   *slog.Logger
	build engineFunc

	up, down atomic.Uint64
	tunUp    atomic.Bool
	wake     chan struct{}

	mu     sync.Mutex
	run    *runState // nil when not running
	link   ipc.State // engine side: connecting, failed, switching, connected
	lastEr string
	full   bool
	ks     bool
	eng    *client.Engine
	exitIP string
	// exitProbeDone is set after the exit-IP probe finishes (success or fail).
	// While full tunnel is up and the probe is still running, Snapshot stays
	// Connecting so the UI does not claim Connected with an empty Exit IP.
	exitProbeDone bool
	diag          *diag.Ring
	subs          map[chan ipc.Status]struct{}
}

func (s *Session) note(e diag.Event) {
	if s.diag != nil {
		s.diag.Note(e)
	}
}

type runState struct {
	cancel context.CancelFunc
	done   chan struct{}
	err    error
}

// New returns an idle Session.
func New(log *slog.Logger) *Session {
	return &Session{
		log:  log,
		wake: make(chan struct{}, 1),
		subs: map[chan ipc.Status]struct{}{},
		build: func(ctx context.Context, o Options, opts transport.Options, ctl *http.Client, onState func(client.Info)) (*client.Engine, error) {
			return app.NewEngine(ctx, app.Params{Config: o.Config, Dir: o.Dir, Opts: opts, HTTP: ctl, OnState: onState, Log: log})
		},
	}
}

// Start brings the session up and returns once the local proxy is bound. It
// does not wait for the node: connecting happens in the background and shows up
// as state changes. Config errors and a busy port are returned here. A full
// tunnel that cannot be set up (no privilege) ends the session; Wait returns
// the error and Snapshot reports StateFailed. Node failures are retried with
// backoff.
func (s *Session) Start(o Options) error {
	c := o.Config
	if err := c.CheckListen(); err != nil {
		return err
	}
	if c.Account == "" && len(c.Control) == 0 && len(c.Endpoints) == 0 {
		return ErrNotConfigured
	}
	ctx, cancel := context.WithCancel(context.Background())
	r := &runState{cancel: cancel, done: make(chan struct{})}
	s.mu.Lock()
	if s.run != nil {
		s.mu.Unlock()
		cancel()
		return ErrRunning
	}
	s.run, s.link, s.lastEr, s.eng = r, ipc.StateConnecting, "", nil
	s.full, s.ks = c.Tun.TunnelOn(), c.Tun.KillSwitch
	s.exitIP, s.exitProbeDone = "", false
	s.diag = o.Diag
	s.mu.Unlock()
	s.log.Info("start", "full", s.full, "guard", s.ks)
	s.note(diag.Event{Code: diag.CodeStart})
	fail := func(err error) error {
		cancel()
		s.mu.Lock()
		s.run = nil
		s.mu.Unlock()
		return err
	}

	var opts transport.Options
	var bypass *tun.Bypass
	var ctl *http.Client
	var rt *router.Router
	if c.Tun.TunnelOn() {
		// Must exist before the tunnel takes the default route: every socket
		// the client opens itself is pinned to the physical interface.
		var err error
		if bypass, err = tun.NewBypass(c.Tun.DNS); err != nil {
			s.log.Error("bypass setup failed", "err", err, "dns", c.Tun.DNS)
			return fail(err)
		}
		opts.Control, opts.Resolver, opts.DNS = bypass.Control, bypass.Resolver(), c.Tun.DNS
		ctl = &http.Client{Timeout: 15 * time.Second, Transport: &http.Transport{
			DialContext: bypass.DialContext, Proxy: nil, ForceAttemptHTTP2: true,
			TLSHandshakeTimeout: 10 * time.Second,
		}}
		if rt, err = router.New(router.Config{
			DirectDomains: c.Routing.DirectDomains, DirectCIDRs: c.Routing.DirectCIDRs,
			GeoIP: c.Routing.GeoIP, GeoSite: c.Routing.GeoSite,
			DirectGeoIP: c.Routing.DirectGeoIP, DirectGeoSite: c.Routing.DirectGeoSite,
		}); err != nil {
			return fail(err)
		}
	}
	ln, err := net.Listen("tcp", c.Listen)
	if err != nil {
		return fail(err)
	}
	s.log.Info("ready", "listen", "local")
	s.up.Store(0)
	s.down.Store(0)
	s.tunUp.Store(false)
	s.changed()
	go s.main(ctx, r, o, opts, ctl, bypass, rt, ln)
	return nil
}

// main runs the proxy, the tunnel and the connect loop until one fails or ctx
// ends, then tears everything down in order: tunnel first (it restores routes,
// DNS and firewall), then the engine.
func (s *Session) main(ctx context.Context, r *runState, o Options, opts transport.Options, ctl *http.Client, bypass *tun.Bypass, rt *router.Router, ln net.Listener) {
	c := o.Config
	errc := make(chan error, 3)
	n := 0
	spawn := func(f func() error) {
		n++
		go func() { errc <- f() }()
	}
	lp := &client.LocalProxy{Dial: s.dial, Log: s.log}
	if u, p, ok, _ := c.ProxyAuth(); ok { // validated by CheckListen
		lp.Auth = client.StaticAuth(u, p)
	}
	spawn(func() error { return lp.Serve(ctx, ln) })
	// Bring the engine up BEFORE installing TUN routes/DNS. Otherwise the
	// default route flips to utun while control/token and the first node
	// dial still need the physical path, and we get "network is unreachable"
	// (and OS DNS briefly points at 198.18.0.2). Bypass keeps later
	// client sockets on the physical iface once the tunnel is up.
	spawn(func() error { return s.connectLoop(ctx, o, opts, ctl) })
	if bypass != nil {
		spawn(func() error {
			if err := s.waitEngineLinked(ctx); err != nil {
				return nil
			}
			pins := s.pinHosts(ctx, bypass, c)
			s.log.Info("start", "pins", len(pins))
			err := tun.Run(ctx, tun.Config{
				Name: c.Tun.Name, MTU: c.Tun.MTU, Tunnel: s.dial, Packet: s.dialPacket,
				Bypass: bypass, Router: rt, KillSwitch: c.Tun.KillSwitch, PinHosts: pins, Log: s.log,
				OnUp: func() {
					s.log.Info("ready", "device", "up")
					s.tunUp.Store(true)
					s.changed()
				},
			})
			if err != nil && ctx.Err() == nil {
				s.log.Error("fail", "err", err)
			}
			return err
		})
	} else {
		s.log.Info("ready", "mode", "proxy")
	}

	var first error
	for range n {
		err := <-errc
		if first == nil {
			first = err
		}
		r.cancel()
	}
	s.mu.Lock()
	eng := s.eng
	s.eng = nil
	s.mu.Unlock()
	if eng != nil {
		_ = eng.Close()
	}
	s.tunUp.Store(false)
	s.mu.Lock()
	r.err = first
	if first != nil {
		s.lastEr = first.Error()
	}
	s.run = nil
	s.mu.Unlock()
	s.changed()
	close(r.done)
}

// connectLoop builds the engine and keeps it connected: first connect, retry
// after failure with backoff, reconnect after a drop.
func (s *Session) connectLoop(ctx context.Context, o Options, opts transport.Options, ctl *http.Client) error {
	backoff := time.Second
	retry := func(err error) bool {
		s.log.Warn("connect failed", "err", err, "retry_in", backoff)
		s.setLink(ipc.StateFailed, err.Error())
		s.note(diag.Event{Code: diag.CodeConnectFail, Detail: "retry"})
		select {
		case <-ctx.Done():
			return false
		case <-time.After(backoff):
		}
		backoff = min(backoff*2, 30*time.Second)
		return true
	}
	for ctx.Err() == nil {
		s.setLink(ipc.StateConnecting, s.lastError())
		eng := s.engine()
		if eng == nil {
			var err error
			if eng, err = s.build(ctx, o, opts, ctl, s.nudge); err != nil {
				if !retry(err) {
					return nil
				}
				continue
			}
			s.mu.Lock()
			s.eng = eng
			s.mu.Unlock()
		}
		if err := eng.Connect(ctx); err != nil {
			if ctx.Err() != nil || !retry(err) {
				return nil
			}
			continue
		}
		backoff = time.Second
		s.mu.Lock()
		s.exitIP, s.exitProbeDone = "", false
		s.mu.Unlock()
		s.setLink(ipc.StateConnected, "")
		info := eng.Info()
		epid := client.EndpointID(info.Endpoint)
		s.note(diag.Event{Code: diag.CodeConnectOK, EndpointID: epid})
		s.log.Info("ready", "id", epid, "full", s.full)
		// Probe the public address through the tunnel dial path. Skip when the
		// session is proxy-only (unit tests and local-proxy mode): a hanging
		// stream must not stall reconnect detection.
		if s.full {
			go s.probeExitIP(ctx)
		} else {
			s.mu.Lock()
			s.exitProbeDone = true
			s.mu.Unlock()
		}
		// ponytail: a session that dies without a dial noticing it has no
		// event, so Alive is polled every 2s. Add an Alive channel to
		// transport.Session if that latency matters.
		tick := time.NewTicker(2 * time.Second)
		for eng.Info().Connected {
			select {
			case <-ctx.Done():
				tick.Stop()
				return nil
			case <-s.wake:
			case <-tick.C:
			}
		}
		tick.Stop()
		s.mu.Lock()
		s.exitIP, s.exitProbeDone = "", false
		s.mu.Unlock()
		s.setLink(ipc.StateSwitching, "")
	}
	return nil
}

// probeExitIP fetches the public address through the engine dial path.
// It runs as soon as the engine is Connected (before or while TUN comes up):
// waiting for tunUp races the tunnel's first burst of streams and was hiding
// the exit IP behind "Checking…" when the node stream cap was already hit.
func (s *Session) probeExitIP(ctx context.Context) {
	defer func() {
		s.mu.Lock()
		s.exitProbeDone = true
		s.mu.Unlock()
		s.changed()
	}()
	s.log.Debug("probe start", "up", s.tunUp.Load())
	var last error
	for attempt := 1; attempt <= 3; attempt++ {
		if ctx.Err() != nil {
			return
		}
		ip, err := LookupPublicIP(ctx, s.dial)
		if err == nil && ip != "" {
			s.mu.Lock()
			if s.link == ipc.StateConnected {
				s.exitIP = ip
			}
			s.mu.Unlock()
			s.log.Debug("probe ok", "attempt", attempt)
			s.changed()
			return
		}
		last = err
		s.log.Debug("probe fail", "attempt", attempt)
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Duration(attempt) * time.Second):
		}
	}
	s.log.Warn("probe fail", "err", last)
}

// waitEngineLinked blocks until the engine reports Connected or ctx ends.
func (s *Session) waitEngineLinked(ctx context.Context) error {
	t := time.NewTicker(100 * time.Millisecond)
	defer t.Stop()
	for {
		s.mu.Lock()
		link := s.link
		s.mu.Unlock()
		if link == ipc.StateConnected {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-t.C:
		}
	}
}

// pinHosts collects IPs that must stay on the physical gateway after the
// tunnel steals the default route: control-plane hosts and the active node.
func (s *Session) pinHosts(ctx context.Context, bypass *tun.Bypass, c *config.Config) []net.IP {
	var out []net.IP
	seen := map[string]bool{}
	add := func(ip net.IP) {
		if ip == nil || ip.IsUnspecified() || ip.IsLoopback() {
			return
		}
		k := ip.String()
		if seen[k] {
			return
		}
		seen[k] = true
		out = append(out, ip)
	}
	resolve := func(host string) {
		if host == "" {
			return
		}
		if ip := net.ParseIP(host); ip != nil {
			add(ip)
			return
		}
		r := bypass.Resolver()
		if r == nil {
			r = net.DefaultResolver
		}
		ips, err := r.LookupIPAddr(ctx, host)
		if err != nil {
			return
		}
		for _, a := range ips {
			add(a.IP)
		}
	}
	for _, ch := range c.Control {
		if u, err := url.Parse(ch.URL); err == nil {
			resolve(u.Hostname())
		}
		if ch.Via != "" {
			if u, err := url.Parse(ch.Via); err == nil {
				resolve(u.Hostname())
			}
		}
	}
	if eng := s.engine(); eng != nil {
		if i := eng.Info(); i.Connected {
			for _, addr := range transport.CandidateAddrs(i.Endpoint) {
				host := addr
				if h, _, err := net.SplitHostPort(addr); err == nil {
					host = h
				}
				resolve(host)
			}
		}
	}
	return out
}

func (s *Session) lastError() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastEr
}

func (s *Session) engine() *client.Engine {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.eng
}

func (s *Session) nudge(client.Info) {
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

func (s *Session) setLink(l ipc.State, errText string) {
	s.mu.Lock()
	s.link, s.lastEr = l, errText
	s.mu.Unlock()
	s.changed()
}

var errNotConnected = errors.New("daemon: not connected")

func (s *Session) dial(ctx context.Context, network, addr string) (net.Conn, error) {
	eng := s.engine()
	if eng == nil {
		return nil, errNotConnected
	}
	c, err := eng.DialContext(ctx, network, addr)
	if err != nil {
		return nil, err
	}
	return &countConn{Conn: c, up: &s.up, down: &s.down}, nil
}

func (s *Session) dialPacket(ctx context.Context, addr string) (net.Conn, error) {
	eng := s.engine()
	if eng == nil {
		return nil, errNotConnected
	}
	c, err := eng.DialPacket(ctx, addr)
	if err != nil {
		return nil, err
	}
	return &countConn{Conn: c, up: &s.up, down: &s.down}, nil
}

// Stop ends the session and waits until routes, DNS and firewall rules are
// restored. It is a no-op when idle.
func (s *Session) Stop() {
	s.mu.Lock()
	r := s.run
	s.mu.Unlock()
	if r == nil {
		return
	}
	s.note(diag.Event{Code: diag.CodeStop})
	r.cancel()
	<-r.done
}

// Wait blocks until the session ends and returns why: nil after Stop or
// context end, an error after a fatal failure such as the tunnel dying. It
// returns nil at once when idle.
func (s *Session) Wait() error {
	s.mu.Lock()
	r := s.run
	s.mu.Unlock()
	if r == nil {
		return nil
	}
	<-r.done
	return r.err
}

// Snapshot is the current status.
func (s *Session) Snapshot() ipc.Status {
	s.mu.Lock()
	running, link, lastEr, full, ks, eng := s.run != nil, s.link, s.lastEr, s.full, s.ks, s.eng
	exitIP, exitDone := s.exitIP, s.exitProbeDone
	s.mu.Unlock()
	st := ipc.Status{At: time.Now(), Running: running, Error: lastEr, FullTunnel: full, KillSwitch: ks}
	tunUp := s.tunUp.Load()
	switch {
	case !running && lastEr != "":
		st.State = ipc.StateFailed
	case !running:
		st.State = ipc.StateOff
	case link == ipc.StateConnected && full && !tunUp:
		// Engine up but system route not installed yet — still connecting.
		st.State = ipc.StateConnecting
	case link == ipc.StateConnected && full && tunUp && exitIP == "" && !exitDone:
		// Tunnel up; exit IP still being verified.
		st.State = ipc.StateConnecting
	default:
		st.State = link
	}
	if running {
		st.UpBytes, st.DownBytes = s.up.Load(), s.down.Load()
	}
	if eng != nil && (st.State == ipc.StateConnected || st.State == ipc.StateConnecting) {
		if i := eng.Info(); i.Connected {
			st.Carrier, st.Server = string(i.Endpoint.Kind), serverName(i.Endpoint)
			st.LatencyMS, st.Since = int(i.Latency.Milliseconds()), i.Since
			if i.Endpoint.Params != nil {
				st.Country = i.Endpoint.Params["cc"]
			}
			switch i.Endpoint.Kind {
			case "reverse", "dns":
				st.ViaRelay = serverName(i.Endpoint)
				if i.Endpoint.Params != nil {
					st.ViaRelayTier = i.Endpoint.Params["tier"]
					st.ViaRelayID = i.Endpoint.Params["relay_id"]
				}
			}
		}
	}
	st.ExitIP = exitIP
	return st
}

// Subscribe delivers a Status on every state change. A slow reader skips to
// the latest; the channel never blocks the session. Call cancel to stop.
func (s *Session) Subscribe() (events <-chan ipc.Status, cancel func()) {
	ch := make(chan ipc.Status, 1)
	s.mu.Lock()
	s.subs[ch] = struct{}{}
	s.mu.Unlock()
	return ch, func() {
		s.mu.Lock()
		delete(s.subs, ch)
		s.mu.Unlock()
	}
}

// changed publishes the current status to subscribers.
func (s *Session) changed() {
	st := s.Snapshot()
	s.mu.Lock()
	defer s.mu.Unlock()
	for ch := range s.subs {
		select {
		case ch <- st:
		default:
			select {
			case <-ch:
			default:
			}
			ch <- st
		}
	}
}

// Servers lists the ladder with live health; nil while idle.
func (s *Session) Servers() []ipc.Server {
	eng := s.engine()
	if eng == nil {
		return nil
	}
	return healthServers(eng)
}

// ProbeAll dials each ladder rung once on the live engine.
func (s *Session) ProbeAll(ctx context.Context) bool {
	eng := s.engine()
	if eng == nil {
		return false
	}
	eng.ProbeAll(ctx)
	return true
}

func healthServers(eng *client.Engine) []ipc.Server {
	var out []ipc.Server
	for _, h := range eng.Health() {
		sv := ipc.Server{
			ID: h.ID, Name: serverName(h.Endpoint), Carrier: string(h.Endpoint.Kind),
			Active: h.Active, LatencyMS: int(h.Latency.Milliseconds()), Error: h.Err,
		}
		if h.Endpoint.Params != nil {
			sv.Country = h.Endpoint.Params["cc"]
		}
		if !h.Cooldown.IsZero() {
			sv.CooldownMS = int(time.Until(h.Cooldown).Milliseconds())
		}
		out = append(out, sv)
	}
	return out
}

func serverName(ep transport.Endpoint) string {
	if ep.Name != "" {
		return ep.Name
	}
	return fmt.Sprint(ep.Kind)
}

// countConn counts payload bytes. CloseWrite must pass through: the relay uses
// it to half-close.
type countConn struct {
	net.Conn
	up, down *atomic.Uint64
}

func (c *countConn) Read(p []byte) (int, error) {
	n, err := c.Conn.Read(p)
	c.down.Add(uint64(n))
	return n, err
}

func (c *countConn) Write(p []byte) (int, error) {
	n, err := c.Conn.Write(p)
	c.up.Add(uint64(n))
	return n, err
}

func (c *countConn) CloseWrite() error {
	if cw, ok := c.Conn.(interface{ CloseWrite() error }); ok {
		return cw.CloseWrite()
	}
	return nil
}
