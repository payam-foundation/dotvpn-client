// Package client is the user-side engine: it keeps one live Session to a node,
// choosing the carrier with a fallback ladder, and exposes it as a plain
// DialContext to the local proxy and TUN fronts.
//
// Interface: NewEngine(Config) *Engine; Engine.DialContext; Engine.SetEndpoints;
// Engine.Status. Probing, ranking, cooldowns, reconnects and token plumbing are
// hidden.
package client

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"sync"
	"time"

	"github.com/payam-foundation/dotvpn-wire/transport"
)

// EngineConfig configures an Engine.
type EngineConfig struct {
	Endpoints []transport.Endpoint
	Dialers   map[transport.Kind]transport.Dialer
	Token     transport.TokenSource
	// Stagger is the delay before the next ladder rung starts racing while
	// earlier rungs are still connecting. Default 400ms.
	Stagger time.Duration
	// Cooldown is how long a failed endpoint is tried last. Default 30s.
	Cooldown time.Duration
	// ProbeTarget ("host:port") enables data-plane verification: a new
	// session counts as connected only after GET /generate_204 through it
	// answers 204. Empty disables the check.
	ProbeTarget string
	// Store remembers the last working endpoint across restarts; nil keeps it
	// in memory only.
	Store StateStore
	// OnUnauthorized is called when a node rejects the token, so the token
	// source can refresh. It must not block for long.
	OnUnauthorized func()
	Log            *slog.Logger
}

// Engine multiplexes tunnels over one live session.
type Engine struct {
	cfg EngineConfig

	mu       sync.Mutex
	eps      []transport.Endpoint
	sess     transport.Session
	cur      transport.Endpoint
	prefer   string
	fast     bool // prefer came from the store and has not been tried yet
	cooldown map[string]time.Time

	connMu sync.Mutex
	closed bool
}

// NewEngine builds an Engine; call Run to start the fallback ladder.
func NewEngine(c EngineConfig) *Engine {
	if c.Stagger == 0 {
		c.Stagger = 400 * time.Millisecond
	}
	if c.Cooldown == 0 {
		c.Cooldown = 30 * time.Second
	}
	if c.Log == nil {
		c.Log = slog.Default()
	}
	e := &Engine{cfg: c, eps: c.Endpoints, cooldown: map[string]time.Time{}}
	if c.Store != nil {
		if st := c.Store.Load(); st.Key != "" {
			e.prefer, e.fast = st.Key, true
		}
	}
	return e
}

// SetEndpoints replaces the ladder, e.g. after a server list refresh. The
// current session stays until it fails.
func (e *Engine) SetEndpoints(eps []transport.Endpoint) {
	e.mu.Lock()
	e.eps = eps
	e.mu.Unlock()
}

// Status describes the active carrier, or "" when disconnected.
func (e *Engine) Status() string {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.sess != nil && e.sess.Alive() {
		return e.cur.Label()
	}
	return ""
}

// Close ends the live session and stops reconnecting.
func (e *Engine) Close() error {
	e.mu.Lock()
	e.closed = true
	s := e.sess
	e.sess = nil
	e.mu.Unlock()
	if s != nil {
		return s.Close()
	}
	return nil
}

// DialContext opens a tunnelled connection to addr ("host:port"). Only tcp is
// supported; UDP flows use DialPacket.
func (e *Engine) DialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	switch network {
	case "tcp", "tcp4", "tcp6":
	default:
		return nil, fmt.Errorf("client: network %q not supported", network)
	}
	var lastErr error
	for range 2 {
		s, err := e.session(ctx)
		if err != nil {
			return nil, err
		}
		c, err := s.OpenStream(ctx, addr)
		if err == nil {
			return c, nil
		}
		lastErr = err
		switch {
		case errors.Is(err, transport.ErrUnauthorized):
			if e.cfg.OnUnauthorized != nil {
				e.cfg.OnUnauthorized()
			}
			return nil, err
		case errors.Is(err, transport.ErrForbidden), errors.Is(err, transport.ErrBusy),
			errors.Is(err, transport.ErrBadGateway), ctx.Err() != nil:
			return nil, err
		}
		e.drop(s)
	}
	return nil, lastErr
}

// Connect establishes the session now instead of at the first dial. It is a
// no-op while a session is live, so callers can retry it to reconnect.
func (e *Engine) Connect(ctx context.Context) error {
	_, err := e.session(ctx)
	return err
}

// ErrNoUDP means the live carrier or node cannot carry UDP flows.
var ErrNoUDP = errors.New("client: node does not offer UDP")

// DialPacket opens a tunnelled UDP flow to addr ("host:port", or "ip:port").
// The conn preserves datagram boundaries. It fails with ErrNoUDP unless the
// connected endpoint advertises UDP (older nodes would splice it as TCP).
func (e *Engine) DialPacket(ctx context.Context, addr string) (net.Conn, error) {
	var lastErr error
	for range 2 {
		s, err := e.session(ctx)
		if err != nil {
			return nil, err
		}
		e.mu.Lock()
		udp := e.cur.UDP()
		e.mu.Unlock()
		us, ok := s.(transport.UDPSession)
		if !ok || !udp {
			return nil, ErrNoUDP
		}
		c, err := us.OpenUDP(ctx, addr)
		if err == nil {
			return c, nil
		}
		lastErr = err
		switch {
		case errors.Is(err, transport.ErrUnauthorized):
			if e.cfg.OnUnauthorized != nil {
				e.cfg.OnUnauthorized()
			}
			return nil, err
		case errors.Is(err, transport.ErrForbidden), errors.Is(err, transport.ErrBusy),
			errors.Is(err, transport.ErrBadGateway), ctx.Err() != nil:
			return nil, err
		}
		e.drop(s)
	}
	return nil, lastErr
}

func (e *Engine) drop(s transport.Session) {
	e.mu.Lock()
	if e.sess == s {
		e.sess = nil
		e.cooldown[key(e.cur)] = time.Now().Add(e.cfg.Cooldown)
		e.cfg.Log.Warn("carrier failed", "endpoint", e.cur.Label())
	}
	e.mu.Unlock()
	_ = s.Close()
}

func (e *Engine) session(ctx context.Context) (transport.Session, error) {
	if s := e.live(); s != nil {
		return s, nil
	}
	e.connMu.Lock()
	defer e.connMu.Unlock()
	if s := e.live(); s != nil {
		return s, nil
	}
	e.mu.Lock()
	closed := e.closed
	e.mu.Unlock()
	if closed {
		return nil, errors.New("client: engine closed")
	}
	s, ep, err := e.race(ctx)
	if err != nil {
		return nil, err
	}
	e.mu.Lock()
	e.sess, e.cur, e.prefer = s, ep, key(ep)
	e.mu.Unlock()
	e.cfg.Log.Info("connected", "endpoint", ep.Label())
	if e.cfg.Store != nil {
		if err := e.cfg.Store.Save(State{Key: key(ep), Addr: ep.Addr, At: time.Now()}); err != nil {
			e.cfg.Log.Debug("state not saved", "err", err)
		}
	}
	return s, nil
}

func (e *Engine) live() transport.Session {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.sess != nil && e.sess.Alive() {
		return e.sess
	}
	return nil
}

func key(ep transport.Endpoint) string {
	return string(ep.Kind) + "|" + ep.Addr + "|" + ep.Host + "|" + ep.Path
}

// expand replaces endpoints that carry Params["ips"] with one endpoint per
// candidate address. With probe set, a candidate group that does not already
// contain the remembered winner is ranked by Probe, so the fastest clean IP
// leads the ladder.
func (e *Engine) expand(ctx context.Context, probe bool) []transport.Endpoint {
	e.mu.Lock()
	eps, prefer := e.eps, e.prefer
	e.mu.Unlock()
	var out []transport.Endpoint
	for _, ep := range eps {
		cands := expandCandidates(ep)
		if probe && len(cands) > 1 && !hasKey(cands, prefer) {
			if r := Probe(ctx, cands, e.cfg.Dialers, e.cfg.Token, e.cfg.ProbeTarget, 4); len(r) > 0 {
				ranked := make([]transport.Endpoint, 0, len(cands))
				for _, x := range r {
					ranked = append(ranked, x.Endpoint)
				}
				for _, c := range cands { // candidates that failed the probe go last
					if !hasKey(ranked, key(c)) {
						ranked = append(ranked, c)
					}
				}
				cands = ranked
			}
		}
		out = append(out, cands...)
	}
	return out
}

func hasKey(eps []transport.Endpoint, k string) bool {
	for _, ep := range eps {
		if key(ep) == k {
			return true
		}
	}
	return false
}

// ladder orders endpoints: last winner, then configured order, then those in
// cooldown. Endpoints whose carrier has no dialer in this build (for example
// UDP carriers behind an upstream proxy) are left out.
func (e *Engine) ladder() []transport.Endpoint {
	return e.arrange(e.expand(context.Background(), false))
}

func (e *Engine) arrange(eps []transport.Endpoint) []transport.Endpoint {
	e.mu.Lock()
	defer e.mu.Unlock()
	now := time.Now()
	var first, mid, last []transport.Endpoint
	for _, ep := range eps {
		if _, ok := e.cfg.Dialers[ep.Kind]; !ok {
			continue
		}
		switch k := key(ep); {
		case k == e.prefer && now.After(e.cooldown[k]):
			first = append(first, ep)
		case now.Before(e.cooldown[k]):
			last = append(last, ep)
		default:
			mid = append(mid, ep)
		}
	}
	return append(append(first, mid...), last...)
}

// fastPath tries the endpoint remembered from the last run on its own, before
// the full race. Only the first connect after start uses it; a miss costs at
// most fastPathBudget.
func (e *Engine) fastPath(ctx context.Context, order []transport.Endpoint) (transport.Session, transport.Endpoint, bool) {
	e.mu.Lock()
	use := e.fast
	e.fast = false
	e.mu.Unlock()
	if !use || len(order) == 0 || key(order[0]) != e.prefer {
		return nil, transport.Endpoint{}, false
	}
	fctx, cancel := context.WithTimeout(ctx, fastPathBudget)
	defer cancel()
	s, err := e.dialVerified(fctx, order[0])
	if err != nil {
		e.cfg.Log.Debug("remembered endpoint failed", "endpoint", order[0].Label(), "err", err)
		return nil, transport.Endpoint{}, false
	}
	return s, order[0], true
}

const fastPathBudget = 6 * time.Second

// dialVerified dials ep and, when ProbeTarget is set, proves the data plane
// before reporting success.
func (e *Engine) dialVerified(ctx context.Context, ep transport.Endpoint) (transport.Session, error) {
	d, ok := e.cfg.Dialers[ep.Kind]
	if !ok {
		return nil, fmt.Errorf("no dialer for %q", ep.Kind)
	}
	s, err := d.Dial(ctx, ep, e.cfg.Token)
	if err != nil {
		return nil, err
	}
	if e.cfg.ProbeTarget == "" {
		return s, nil
	}
	if _, err := verifyDataPlane(ctx, s, e.cfg.ProbeTarget); err != nil {
		_ = s.Close()
		if errors.Is(err, transport.ErrUnauthorized) && e.cfg.OnUnauthorized != nil {
			e.cfg.OnUnauthorized()
		}
		return nil, err
	}
	return s, nil
}

type attempt struct {
	s   transport.Session
	ep  transport.Endpoint
	err error
}

// race connects along the ladder happy-eyeballs style: rung N+1 starts when
// the Stagger delay passes or rung N fails, first established session wins.
func (e *Engine) race(ctx context.Context) (transport.Session, transport.Endpoint, error) {
	order := e.arrange(e.expand(ctx, true))
	if len(order) == 0 {
		return nil, transport.Endpoint{}, errors.New("client: no endpoints configured")
	}
	if s, ep, ok := e.fastPath(ctx, order); ok {
		return s, ep, nil
	}
	rctx, cancel := context.WithCancel(ctx)
	defer cancel()
	results := make(chan attempt, len(order))
	launch := func(ep transport.Endpoint) {
		go func() {
			s, err := e.dialVerified(rctx, ep)
			results <- attempt{s: s, ep: ep, err: err}
		}()
	}

	launched, received := 0, 0
	next := func() {
		if launched < len(order) {
			launch(order[launched])
			launched++
		}
	}
	next()
	timer := time.NewTimer(e.cfg.Stagger)
	defer timer.Stop()
	var errs []error
	for {
		select {
		case <-timer.C:
			next()
			timer.Reset(e.cfg.Stagger)
		case r := <-results:
			received++
			if r.err == nil {
				go func(pending int) {
					for range pending {
						if l := <-results; l.s != nil {
							_ = l.s.Close()
						}
					}
				}(launched - received)
				return r.s, r.ep, nil
			}
			e.cfg.Log.Debug("endpoint failed", "endpoint", r.ep.Label(), "err", r.err)
			e.mu.Lock()
			e.cooldown[key(r.ep)] = time.Now().Add(e.cfg.Cooldown)
			e.mu.Unlock()
			errs = append(errs, fmt.Errorf("%s: %w", r.ep.Label(), r.err))
			if received == len(order) {
				return nil, transport.Endpoint{}, fmt.Errorf("client: all endpoints failed: %w", errors.Join(errs...))
			}
			if received == launched {
				next()
			}
		case <-ctx.Done():
			go func(pending int) {
				for range pending {
					if l := <-results; l.s != nil {
						_ = l.s.Close()
					}
				}
			}(launched - received)
			return nil, transport.Endpoint{}, ctx.Err()
		}
	}
}
