package client

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/payam-foundation/dotvpn-wire/transport"
)

type fakeSession struct{ alive bool }

func (s *fakeSession) OpenStream(context.Context, string) (net.Conn, error) {
	a, b := net.Pipe()
	go func() { _ = b.Close() }()
	return a, nil
}
func (s *fakeSession) Alive() bool  { return s.alive }
func (s *fakeSession) Close() error { s.alive = false; return nil }

type dialerFunc func(ctx context.Context, ep transport.Endpoint) (transport.Session, error)

func (f dialerFunc) Dial(ctx context.Context, ep transport.Endpoint, _ transport.TokenSource) (transport.Session, error) {
	return f(ctx, ep)
}

// The ladder falls through a blocked rung to the next carrier, and remembers
// the winner so the next connect starts there.
func TestLadderFallsBackAndPrefersWinner(t *testing.T) {
	blocked := dialerFunc(func(context.Context, transport.Endpoint) (transport.Session, error) {
		return nil, errors.New("connection reset")
	})
	ok := dialerFunc(func(context.Context, transport.Endpoint) (transport.Session, error) {
		return &fakeSession{alive: true}, nil
	})
	e := NewEngine(EngineConfig{
		Endpoints: []transport.Endpoint{
			{Kind: transport.KindH2, Addr: "a:443"},
			{Kind: transport.KindCDNWS, Addr: "b:443"},
		},
		Dialers: map[transport.Kind]transport.Dialer{transport.KindH2: blocked, transport.KindCDNWS: ok},
		Token:   func() string { return "t" },
		Stagger: 10 * time.Millisecond,
	})
	defer e.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, err := e.DialContext(ctx, "tcp", "example.com:443")
	if err != nil {
		t.Fatal(err)
	}
	_ = c.Close()
	if got := e.Status(); got != "cdnws@b:443" {
		t.Fatalf("status = %q", got)
	}
	// The failed rung is in cooldown: it is ordered last next time.
	if l := e.ladder(); l[0].Kind != transport.KindCDNWS {
		t.Fatalf("ladder = %v", l)
	}
}

func TestAllRungsBlockedIsAnError(t *testing.T) {
	blocked := dialerFunc(func(context.Context, transport.Endpoint) (transport.Session, error) {
		return nil, errors.New("blocked")
	})
	e := NewEngine(EngineConfig{
		Endpoints: []transport.Endpoint{{Kind: transport.KindH2, Addr: "a:443"}},
		Dialers:   map[transport.Kind]transport.Dialer{transport.KindH2: blocked},
		Token:     func() string { return "t" },
	})
	defer e.Close()
	if _, err := e.DialContext(context.Background(), "tcp", "x:1"); err == nil {
		t.Fatal("expected failure")
	}
	if _, err := e.DialContext(context.Background(), "udp", "x:1"); err == nil {
		t.Fatal("udp must be refused: the engine does not tunnel it")
	}
}
