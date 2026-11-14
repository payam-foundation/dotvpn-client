// SPDX-License-Identifier: AGPL-3.0-only

// Package mobile is the gomobile surface of the client (ADR-025). The Android
// VpnService owns the TUN device, routes, DNS and kill switch; this package
// runs the engine and the gVisor front on the descriptor it hands over.
//
// Only gomobile-safe types cross the boundary: string, int, bool, error and the
// Protector interface.
package mobile

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"golang.zx2c4.com/wireguard/tun"

	"github.com/payam-foundation/dotvpn-client/client"
	"github.com/payam-foundation/dotvpn-client/internal/app"
	"github.com/payam-foundation/dotvpn-client/internal/config"
	"github.com/payam-foundation/dotvpn-client/internal/diag"
	"github.com/payam-foundation/dotvpn-client/internal/report"
	"github.com/payam-foundation/dotvpn-client/internal/router"
	inttun "github.com/payam-foundation/dotvpn-client/internal/tun"
	"github.com/payam-foundation/dotvpn-wire/invite"
	"github.com/payam-foundation/dotvpn-wire/transport"
	"github.com/payam-foundation/dotvpn-wire/transport/reverse"
)

// appVersion is set by the Android shell via SetAppVersion.
var appVersion = "dev"

var mobileRing = diag.NewRing("android", appVersion)

// SetAppVersion records the shell version for diagnostics bundles.
func SetAppVersion(v string) {
	v = strings.TrimSpace(v)
	if v == "" {
		return
	}
	appVersion = v
	mobileRing.SetVersion(v)
}

// NormalizeConfig runs defaults/migrations (mints device_id) and returns YAML.
func NormalizeConfig(cfg string) (string, error) {
	c, err := config.Parse([]byte(cfg))
	if err != nil {
		return "", err
	}
	_ = c.Migrate()
	b, err := config.Marshal(c)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// Protector exempts a socket from the VPN (VpnService.protect). Every socket
// the client opens itself goes through it first, or it would loop into the
// tunnel.
type Protector interface {
	Protect(fd int) bool
}

// ImportInvite turns a dv:// invite into a client config (YAML) with the
// full tunnel enabled. The app stores it and passes it to Start.
func ImportInvite(uri string) (string, error) {
	i, err := invite.DecodeInvite(uri)
	if err != nil {
		return "", err
	}
	c := &config.Config{}
	c.Defaults()
	c.Apply(i)
	c.Tun.SetEnabled(true)
	b, err := config.Marshal(c)
	return string(b), err
}

// Endpoints lists the carriers in cfg, one label per line, for the server list
// screen. Servers learned later from the control plane are not included.
func Endpoints(cfg string) (string, error) {
	c, err := config.Parse([]byte(cfg))
	if err != nil {
		return "", err
	}
	var b strings.Builder
	for _, ep := range c.Endpoints {
		b.WriteString(ep.Label())
		b.WriteByte('\n')
	}
	return b.String(), nil
}

// ProbeServers dials each ladder rung once (non-recurring) and returns JSON
// [{"id","name","carrier","country","latencyMs","error"}]. p must protect sockets when a
// tunnel is up; a no-op protector is fine while the VPN is off.
func ProbeServers(cfgYAML, dataDir string, p Protector) (string, error) {
	if p == nil {
		return "", errors.New("mobile: protector required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	mu.Lock()
	eng := (*client.Engine)(nil)
	if cur != nil {
		eng = cur.eng
	}
	mu.Unlock()
	if eng != nil {
		eng.ProbeAll(ctx)
		return marshalProbeHealth(eng.Health())
	}

	c, err := config.Parse([]byte(cfgYAML))
	if err != nil {
		return "", err
	}
	_ = c.Migrate()
	dns := ensureDNSPort(c.Tun.DNS)
	bypass := inttun.NewProtectedBypass(dns, p.Protect)
	opts := transport.Options{Control: bypass.Control, Resolver: bypass.Resolver(), DNS: dns}
	if !c.Tun.DoH {
		opts.DoH = []transport.DoHServer{}
	}
	ctl := &http.Client{Timeout: 15 * time.Second, Transport: &http.Transport{
		DialContext: bypass.DialContext, Proxy: nil, ForceAttemptHTTP2: true,
		TLSHandshakeTimeout: 10 * time.Second,
	}}
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	tmp, err := app.NewEngine(ctx, app.Params{Config: c, Dir: dataDir, Opts: opts, HTTP: ctl, Log: log})
	if err != nil {
		return "", err
	}
	defer tmp.Close()
	tmp.ProbeAll(ctx)
	return marshalProbeHealth(tmp.Health())
}

func marshalProbeHealth(hs []client.Health) (string, error) {
	type row struct {
		ID        string `json:"id"`
		Name      string `json:"name"`
		Carrier   string `json:"carrier"`
		Country   string `json:"country,omitempty"`
		LatencyMS int    `json:"latencyMs"`
		Error     string `json:"error"`
	}
	out := make([]row, 0, len(hs))
	for _, x := range hs {
		name := x.Endpoint.Name
		if name == "" {
			name = string(x.Endpoint.Kind)
		}
		out = append(out, row{
			ID: x.ID, Name: name, Carrier: string(x.Endpoint.Kind), Country: x.Endpoint.Params["cc"],
			LatencyMS: int(x.Latency.Milliseconds()), Error: x.Err,
		})
	}
	b, err := json.Marshal(out)
	return string(b), err
}

type session struct {
	cancel context.CancelFunc
	done   chan struct{}
	eng    *client.Engine
}

var (
	mu     sync.Mutex
	cur    *session
	status = "off"
)

// newDevice wraps the VpnService descriptor; tests replace it.
var newDevice = deviceFromFD

// Start brings the tunnel up on tunFd (the descriptor from
// VpnService.Builder.establish, ownership passes to Go). cfg is the YAML from
// ImportInvite; dataDir is app-private storage for state.json and the server
// list cache. It returns once the engine is running; Status reports the
// carrier once connected.
func Start(cfg string, dataDir string, tunFd int, p Protector) error {
	dev, err := newDevice(tunFd)
	if err != nil {
		return err
	}
	if err := start(cfg, dataDir, dev, p); err != nil {
		_ = dev.Close()
		return err
	}
	return nil
}

func start(cfg, dataDir string, dev tun.Device, p Protector) error {
	if p == nil {
		return errors.New("mobile: protector required")
	}
	mu.Lock()
	defer mu.Unlock()
	if cur != nil {
		return errors.New("mobile: already started")
	}
	c, err := config.Parse([]byte(cfg))
	if err != nil {
		return fmt.Errorf("mobile: config: %w", err)
	}
	_ = c.Migrate() // mint device_id; Android should persist via NormalizeConfig
	// Warn and above only: logcat is readable by other tools on rooted or
	// debug devices, and Info lines name endpoints.
	log := report.Tee(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	if c.Tun.KillSwitch {
		log.Warn("guard ignored on mobile; use system Always-on block")
	}
	mobileRing.Note(diag.Event{Code: diag.CodeStart})
	dns := ensureDNSPort(c.Tun.DNS)
	bypass := inttun.NewProtectedBypass(dns, p.Protect)
	opts := transport.Options{Control: bypass.Control, Resolver: bypass.Resolver(), DNS: dns}
	// Nil DoH means transport defaults; empty non-nil disables DoH (plain DNS only).
	if !c.Tun.DoH {
		opts.DoH = []transport.DoHServer{}
	}
	ctl := &http.Client{Timeout: 15 * time.Second, Transport: &http.Transport{
		DialContext: bypass.DialContext, Proxy: nil, ForceAttemptHTTP2: true,
		TLSHandshakeTimeout: 10 * time.Second,
	}}
	ctx, cancel := context.WithCancel(context.Background())
	eng, err := app.NewEngine(ctx, app.Params{Config: c, Dir: dataDir, Opts: opts, HTTP: ctl, Log: log})
	if err != nil {
		cancel()
		return err
	}
	rt, err := router.New(router.Config{
		DirectDomains: c.Routing.DirectDomains, DirectCIDRs: c.Routing.DirectCIDRs,
		GeoIP: c.Routing.GeoIP, GeoSite: c.Routing.GeoSite,
		DirectGeoIP: c.Routing.DirectGeoIP, DirectGeoSite: c.Routing.DirectGeoSite,
	})
	if err != nil {
		cancel()
		_ = eng.Close()
		return err
	}
	s := &session{cancel: cancel, done: make(chan struct{}), eng: eng}
	cur, status = s, "connecting"
	go func() {
		defer close(s.done)
		err := inttun.Run(ctx, inttun.Config{
			MTU: c.Tun.MTU, Tunnel: eng.DialContext, Packet: eng.DialPacket,
			Bypass: bypass, Router: rt, Device: dev, Log: log,
		})
		cancel()
		_ = eng.Close()
		mu.Lock()
		if cur == s {
			cur = nil
			status = "off"
			if err != nil {
				status = "error: " + err.Error()
			}
		}
		mu.Unlock()
	}()
	go keepConnected(ctx, eng)
	return nil
}

// keepConnected reconnects whenever the session drops (network change,
// airplane mode) instead of waiting for the next flow to trigger it.
// Backoff grows from 1s to 30s on failure (aligned with desktop connectLoop).
func keepConnected(ctx context.Context, eng *client.Engine) {
	backoff := time.Second
	for ctx.Err() == nil {
		if err := eng.Connect(ctx); err != nil {
			mobileRing.Note(diag.Event{Code: diag.CodeConnectFail, Detail: "retry"})
			select {
			case <-ctx.Done():
			case <-time.After(backoff):
			}
			if backoff < 30*time.Second {
				backoff *= 2
				if backoff > 30*time.Second {
					backoff = 30 * time.Second
				}
			}
			continue
		}
		backoff = time.Second
		info := eng.Info()
		mobileRing.Note(diag.Event{Code: diag.CodeConnectOK, EndpointID: client.EndpointID(info.Endpoint)})
		select {
		case <-ctx.Done():
		case <-time.After(2 * time.Second):
		}
	}
}

// ensureDNSPort appends :53 when dns is a bare IP or hostname.
func ensureDNSPort(dns string) string {
	dns = strings.TrimSpace(dns)
	if dns == "" {
		return "1.1.1.1:53"
	}
	if _, _, err := net.SplitHostPort(dns); err == nil {
		return dns
	}
	return dns + ":53"
}

// Stop ends the tunnel and waits for it to close the descriptor.
func Stop() {
	mu.Lock()
	s := cur
	mu.Unlock()
	if s == nil {
		return
	}
	mobileRing.Note(diag.Event{Code: diag.CodeStop})
	s.cancel()
	<-s.done
}

// Status is "off", "connecting", "error: <reason>", or when connected:
//
//	connected <kind>|<viaName>|<viaTier>|<relayID>|<sinceUnixMs>|<latencyMs>
//
// via* fields are empty unless the active carrier is reverse/dns. since and
// latency are 0 when unknown.
func Status() string {
	mu.Lock()
	defer mu.Unlock()
	if cur != nil {
		if info := cur.eng.Info(); info.Connected {
			return formatConnected(info)
		}
	}
	return status
}

func formatConnected(info client.Info) string {
	ep := info.Endpoint
	kind := string(ep.Kind)
	via, tier, rid := "", "", ""
	switch ep.Kind {
	case reverse.KindReverse, reverse.KindDNS:
		via = ep.Name
		if via == "" {
			via = kind
		}
		if ep.Params != nil {
			tier = ep.Params["tier"]
			rid = ep.Params["relay_id"]
		}
	}
	var since int64
	if !info.Since.IsZero() {
		since = info.Since.UnixMilli()
	}
	return fmt.Sprintf("connected %s|%s|%s|%s|%d|%d", kind, via, tier, rid, since, info.Latency.Milliseconds())
}

// Report is a built report for the Android shell. Blob is the sealed file,
// Armor its pasteable text form, Preview what the blob contains.
type Report struct {
	ID      string
	Address string
	Blob    []byte
	Armor   string
	Preview string
}

// BuildReport seals the recent scrubbed logs. metaJSON is a flat string map
// of device facts (SDK, model, network type) from the shell; bad JSON is
// ignored. With previewOnly only ID and Preview are filled.
func BuildReport(metaJSON string, windowMin int, note string, previewOnly bool) (*Report, error) {
	var meta map[string]string
	_ = json.Unmarshal([]byte(metaJSON), &meta)
	w := time.Duration(windowMin) * time.Minute
	if w <= 0 || w > report.RingMaxAge {
		w = report.RingMaxAge
	}
	mobileRing.SetVersion(appVersion)
	mobileRing.SetState(coarseState(Status()))
	r, err := report.Build(report.Options{
		Window: w, Note: note, Meta: meta, Logs: report.Logs,
		Events: mobileRing.Snapshot(), PreviewOnly: previewOnly,
	})
	if err != nil {
		return nil, err
	}
	return &Report{ID: r.ID, Address: report.Address, Blob: r.Blob, Armor: r.Armor, Preview: r.Preview}, nil
}

// coarseState strips carrier names and error text from Status() for L0.
func coarseState(s string) string {
	switch {
	case s == "off" || s == "connecting":
		return s
	case strings.HasPrefix(s, "connected"):
		return "connected"
	case strings.HasPrefix(s, "error:"):
		return "error"
	default:
		return "unknown"
	}
}
