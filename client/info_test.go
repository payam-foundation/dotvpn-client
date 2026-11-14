// SPDX-License-Identifier: AGPL-3.0-only

package client

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/payam-foundation/dotvpn-wire/transport"
)

func TestInfoHealthAndStateCallback(t *testing.T) {
	blocked := dialerFunc(func(context.Context, transport.Endpoint) (transport.Session, error) {
		return nil, errors.New("reset")
	})
	var sess *fakeSession
	ok := dialerFunc(func(context.Context, transport.Endpoint) (transport.Session, error) {
		sess = &fakeSession{alive: true}
		return sess, nil
	})
	var mu sync.Mutex
	var states []bool
	e := NewEngine(EngineConfig{
		Endpoints: []transport.Endpoint{
			{Kind: transport.KindH2, Addr: "a:443"},
			{Kind: transport.KindCDNWS, Addr: "b:443"},
		},
		Dialers: map[transport.Kind]transport.Dialer{transport.KindH2: blocked, transport.KindCDNWS: ok},
		Token:   func() string { return "t" },
		Stagger: 5 * time.Millisecond,
		OnState: func(i Info) { mu.Lock(); states = append(states, i.Connected); mu.Unlock() },
	})
	if e.Info().Connected {
		t.Fatal("connected before Connect")
	}
	if err := e.Connect(context.Background()); err != nil {
		t.Fatal(err)
	}
	info := e.Info()
	if !info.Connected || info.Endpoint.Kind != transport.KindCDNWS || info.Since.IsZero() {
		t.Fatalf("info = %+v", info)
	}
	byKind := map[transport.Kind]Health{}
	for _, h := range e.Health() {
		byKind[h.Endpoint.Kind] = h
	}
	if h := byKind[transport.KindCDNWS]; !h.Active || h.Err != "" {
		t.Fatalf("cdnws health = %+v", h)
	}
	if h := byKind[transport.KindH2]; h.Active || h.Err != "reset" || h.Cooldown.IsZero() {
		t.Fatalf("h2 health = %+v", h)
	}
	sess.alive = false
	if e.Info().Connected {
		t.Fatal("dead session reported as connected")
	}
	_ = e.Close()
	mu.Lock()
	defer mu.Unlock()
	if len(states) == 0 || !states[0] {
		t.Fatalf("states = %v", states)
	}
}

// A pin restricts the ladder to one endpoint; an unknown pin is ignored.
func TestPin(t *testing.T) {
	eps := []transport.Endpoint{{Kind: transport.KindH2, Addr: "a:443"}, {Kind: transport.KindCDNWS, Addr: "b:443"}}
	d := dialerFunc(func(context.Context, transport.Endpoint) (transport.Session, error) {
		return &fakeSession{alive: true}, nil
	})
	dialers := map[transport.Kind]transport.Dialer{transport.KindH2: d, transport.KindCDNWS: d}
	e := NewEngine(EngineConfig{Endpoints: eps, Dialers: dialers, Pin: EndpointID(eps[1])})
	if l := e.ladder(); len(l) != 1 || l[0].Kind != transport.KindCDNWS {
		t.Fatalf("ladder = %v", l)
	}
	e = NewEngine(EngineConfig{Endpoints: eps, Dialers: dialers, Pin: "stale"})
	if l := e.ladder(); len(l) != 2 {
		t.Fatalf("stale pin emptied the ladder: %v", l)
	}
}

func latencyEngine(eps []transport.Endpoint, d dialerFunc) *Engine {
	dialers := map[transport.Kind]transport.Dialer{}
	for _, ep := range eps {
		dialers[ep.Kind] = d
	}
	return NewEngine(EngineConfig{Endpoints: eps, Dialers: dialers, Token: func() string { return "t" }})
}

func kinds(eps []transport.Endpoint) []transport.Kind {
	var out []transport.Kind
	for _, ep := range eps {
		out = append(out, ep.Kind)
	}
	return out
}

// Fresh readings order the middle of the ladder; stale or other-generation
// readings are ignored, prefer stays first and cool stays last.
func TestOrderUsesFreshLatency(t *testing.T) {
	eps := []transport.Endpoint{
		{Kind: transport.KindH2, Addr: "a:443"},
		{Kind: transport.KindH3, Addr: "b:443"},
		{Kind: transport.KindCDNWS, Addr: "c:443"},
		{Kind: transport.KindWebTunnel, Addr: "d:443"},
	}
	e := latencyEngine(eps, nil)
	e.record(eps[1], 90*time.Millisecond, nil, 0)
	e.record(eps[2], 30*time.Millisecond, nil, 0)
	want := []transport.Kind{transport.KindCDNWS, transport.KindH3, transport.KindH2, transport.KindWebTunnel}
	if got := kinds(e.order(eps, true)); !slices.Equal(got, want) {
		t.Fatalf("fresh order = %v, want %v", got, want)
	}

	e.mu.Lock()
	e.health[key(eps[2])] = epHealth{latency: time.Millisecond, at: time.Now().Add(-2 * latencyTTL)}
	e.mu.Unlock()
	want = []transport.Kind{transport.KindH3, transport.KindH2, transport.KindCDNWS, transport.KindWebTunnel}
	if got := kinds(e.order(eps, true)); !slices.Equal(got, want) {
		t.Fatalf("stale reading must be ignored: %v, want %v", got, want)
	}

	e.roamed() // generation moves on; every reading is stale
	if got := kinds(e.order(eps, true)); !slices.Equal(got, kinds(eps)) {
		t.Fatalf("other-generation readings must be ignored: %v", got)
	}

	gen := e.generation()
	e.record(eps[3], 10*time.Millisecond, nil, gen)
	e.record(eps[1], 5*time.Millisecond, nil, gen)
	e.mu.Lock()
	e.prefer = key(eps[0])
	e.cooldown[key(eps[1])] = time.Now().Add(time.Minute)
	e.mu.Unlock()
	want = []transport.Kind{transport.KindH2, transport.KindWebTunnel, transport.KindCDNWS, transport.KindH3}
	if got := kinds(e.order(eps, false)); !slices.Equal(got, want) {
		t.Fatalf("prefer first, cool last: %v, want %v", got, want)
	}
}

// Warm probes only stale rungs, at most warmMax, never the live one, and not
// at all when everything is fresh.
func TestWarmCapAndFreshness(t *testing.T) {
	var eps []transport.Endpoint
	var mu sync.Mutex
	var dialed []string
	var inflight, peak int
	d := dialerFunc(func(_ context.Context, ep transport.Endpoint) (transport.Session, error) {
		mu.Lock()
		dialed = append(dialed, ep.Addr)
		inflight++
		peak = max(peak, inflight)
		mu.Unlock()
		time.Sleep(10 * time.Millisecond)
		mu.Lock()
		inflight--
		mu.Unlock()
		return &fakeSession{alive: true}, nil
	})
	for _, a := range []string{"a", "b", "c", "d", "e", "f"} {
		eps = append(eps, transport.Endpoint{Kind: transport.KindH2, Addr: a + ":443"})
	}
	e := NewEngine(EngineConfig{
		Endpoints: eps,
		Dialers:   map[transport.Kind]transport.Dialer{transport.KindH2: d},
		Token:     func() string { return "t" },
	})
	e.Warm(context.Background())
	if len(dialed) != warmMax || peak > warmConc {
		t.Fatalf("dialed %d (cap %d), peak concurrency %d (cap %d)", len(dialed), warmMax, peak, warmConc)
	}
	dialed = nil
	e.Warm(context.Background()) // the first four are fresh now, two remain
	if len(dialed) != 2 {
		t.Fatalf("second Warm dialed %v, want the 2 stale rungs", dialed)
	}
	dialed = nil
	e.Warm(context.Background())
	if len(dialed) != 0 {
		t.Fatalf("everything fresh, but Warm dialed %v", dialed)
	}
}

func TestWarmCancels(t *testing.T) {
	d := dialerFunc(func(ctx context.Context, _ transport.Endpoint) (transport.Session, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	})
	e := latencyEngine([]transport.Endpoint{{Kind: transport.KindH2, Addr: "a:443"}}, d)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { e.Warm(ctx); close(done) }()
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Warm ignored ctx")
	}
	if h := e.Health()[0]; h.Err != "" {
		t.Fatalf("a cancelled probe must not be recorded as a failure: %+v", h)
	}
}
