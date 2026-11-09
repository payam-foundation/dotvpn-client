package client

import (
	"context"
	"errors"
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
