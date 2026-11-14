package client

import (
	"context"
	"errors"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"github.com/payam-foundation/dotvpn-wire/transport"
)

// liveSession is a race-safe Session that stays alive until closed.
type liveSession struct{ dead atomic.Bool }

func newLive() *liveSession { return &liveSession{} }

func (s *liveSession) OpenStream(context.Context, string) (net.Conn, error) {
	a, b := net.Pipe()
	_ = b.Close()
	return a, nil
}
func (s *liveSession) Alive() bool  { return !s.dead.Load() }
func (s *liveSession) Close() error { s.dead.Store(true); return nil }

func newKeepEngine(d dialerFunc) *Engine {
	return NewEngine(EngineConfig{
		Endpoints: []transport.Endpoint{{Kind: transport.KindH2, Addr: "a:443"}},
		Dialers:   map[transport.Kind]transport.Dialer{transport.KindH2: d},
		Token:     func() string { return "t" },
		Stagger:   time.Millisecond,
	})
}

func waitFor(t *testing.T, ch <-chan struct{}, d time.Duration, what string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(d):
		t.Fatalf("timed out waiting for %s", what)
	}
}

// A roam closes the live session without cooling its endpoint, and Keep
// reconnects after the debounce instead of waiting for the 2s Alive tick.
func TestNetworkChangedReconnectsWithoutCooling(t *testing.T) {
	var dials atomic.Int32
	e := newKeepEngine(func(context.Context, transport.Endpoint) (transport.Session, error) {
		dials.Add(1)
		return newLive(), nil
	})
	defer func() { _ = e.Close() }()
	ctx, cancel := context.WithCancel(context.Background())
	up := make(chan struct{}, 4)
	done := make(chan struct{})
	go func() {
		e.Keep(ctx, Hooks{OnUp: func(Info) { up <- struct{}{} }})
		close(done)
	}()
	waitFor(t, up, time.Second, "first connect")

	e.mu.Lock()
	e.fails["stale"] = 3
	e.cooldown["stale"] = time.Now().Add(time.Hour)
	e.mu.Unlock()

	start := time.Now()
	e.NetworkChanged()
	waitFor(t, up, 1800*time.Millisecond, "reconnect after roam")
	if d := time.Since(start); d < roamDebounce {
		t.Fatalf("reconnected in %v, before the %v debounce", d, roamDebounce)
	}
	e.mu.Lock()
	cool, fails, key0 := len(e.cooldown), len(e.fails), key(e.cur)
	e.mu.Unlock()
	if cool != 0 || fails != 0 {
		t.Fatalf("roam kept stale state: cooldown=%d fails=%d", cool, fails)
	}
	if !e.cooldownExpired(key0) {
		t.Fatal("the roamed-away endpoint was penalised")
	}
	if dials.Load() != 2 || e.generation() != 1 {
		t.Fatalf("dials=%d gen=%d, want 2 and 1", dials.Load(), e.generation())
	}
	cancel()
	waitFor(t, done, time.Second, "Keep to return on cancel")
}

func (e *Engine) cooldownExpired(k string) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return !time.Now().Before(e.cooldown[k])
}

// Callback bursts coalesce into one roam.
func TestNetworkChangedDebounces(t *testing.T) {
	e := newKeepEngine(func(context.Context, transport.Endpoint) (transport.Session, error) {
		return newLive(), nil
	})
	defer func() { _ = e.Close() }()
	for range 5 {
		e.NetworkChanged()
		time.Sleep(50 * time.Millisecond)
	}
	time.Sleep(roamDebounce + 200*time.Millisecond)
	if g := e.generation(); g != 1 {
		t.Fatalf("generation = %d, want 1 for one burst", g)
	}
}

// A failing connect backs off from 1s, and a roam restarts the backoff.
func TestKeepBackoffResetsOnRoam(t *testing.T) {
	e := newKeepEngine(func(context.Context, transport.Endpoint) (transport.Session, error) {
		return nil, errors.New("blocked")
	})
	defer func() { _ = e.Close() }()
	ctx, cancel := context.WithCancel(context.Background())
	retry := make(chan time.Duration, 8)
	done := make(chan struct{})
	go func() {
		e.Keep(ctx, Hooks{OnFail: func(_ error, d time.Duration) { retry <- d }})
		close(done)
	}()
	if d := <-retry; d != time.Second {
		t.Fatalf("first retry in %v, want 1s", d)
	}
	e.NetworkChanged()
	select {
	case d := <-retry:
		if d != time.Second {
			t.Fatalf("retry after roam in %v, want 1s", d)
		}
	case <-time.After(1900 * time.Millisecond):
		t.Fatal("no retry after roam")
	}
	cancel()
	waitFor(t, done, time.Second, "Keep to return on cancel")
}

// A roam during a connect aborts it without cooling the endpoint it was
// dialling.
func TestRoamDuringConnectIsNotAFault(t *testing.T) {
	started := make(chan struct{})
	var first atomic.Bool
	e := newKeepEngine(func(ctx context.Context, _ transport.Endpoint) (transport.Session, error) {
		if first.CompareAndSwap(false, true) {
			close(started)
			<-ctx.Done()
			return nil, ctx.Err()
		}
		return newLive(), nil
	})
	defer func() { _ = e.Close() }()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	up := make(chan struct{}, 1)
	go e.Keep(ctx, Hooks{OnUp: func(Info) { up <- struct{}{} }})
	waitFor(t, started, time.Second, "first dial")
	e.NetworkChanged()
	waitFor(t, up, 2*time.Second, "connect after roam")
	if !e.cooldownExpired(key(transport.Endpoint{Kind: transport.KindH2, Addr: "a:443"})) {
		t.Fatal("endpoint cooled by a roam-cancelled dial")
	}
}
