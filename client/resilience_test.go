package client

import (
	"context"
	"errors"
	"net"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/payam-foundation/dotvpn-wire/transport"
)

// httpSession answers every stream like a web server returning status.
type httpSession struct{ status string }

func (s httpSession) OpenStream(context.Context, string) (net.Conn, error) {
	a, b := net.Pipe()
	go func() {
		defer b.Close()
		buf := make([]byte, 4096)
		_, _ = b.Read(buf)
		_, _ = b.Write([]byte("HTTP/1.1 " + s.status + "\r\nContent-Length: 0\r\n\r\n"))
	}()
	return a, nil
}
func (httpSession) Alive() bool  { return true }
func (httpSession) Close() error { return nil }

type deadSession struct{ httpSession }

func (deadSession) OpenStream(context.Context, string) (net.Conn, error) {
	a, b := net.Pipe()
	go func() { time.Sleep(50 * time.Millisecond); _ = b.Close() }()
	return a, nil
}

func TestVerifyDataPlane(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		name string
		s    transport.Session
		ok   bool
	}{
		{"204", httpSession{"204 No Content"}, true},
		{"captive portal 200", httpSession{"200 OK"}, false},
		{"stream closes silently", deadSession{}, false},
	} {
		_, err := verifyDataPlane(ctx, tc.s, DefaultProbeTarget)
		if (err == nil) != tc.ok {
			t.Errorf("%s: err = %v, want ok=%v", tc.name, err, tc.ok)
		}
	}
}

// A session whose handshake passes but whose data plane is dead must not count
// as connected: the engine moves on to the next endpoint.
func TestEngineRejectsDeadDataPlane(t *testing.T) {
	dead := dialerFunc(func(context.Context, transport.Endpoint) (transport.Session, error) { return deadSession{}, nil })
	good := dialerFunc(func(context.Context, transport.Endpoint) (transport.Session, error) {
		return httpSession{"204 No Content"}, nil
	})
	e := NewEngine(EngineConfig{
		Endpoints:   []transport.Endpoint{{Kind: transport.KindH2, Addr: "a:443"}, {Kind: transport.KindCDNWS, Addr: "b:443"}},
		Dialers:     map[transport.Kind]transport.Dialer{transport.KindH2: dead, transport.KindCDNWS: good},
		Token:       func() string { return "t" },
		Stagger:     5 * time.Millisecond,
		ProbeTarget: DefaultProbeTarget,
	})
	defer e.Close()
	c, err := e.DialContext(context.Background(), "tcp", "example.com:443")
	if err != nil {
		t.Fatal(err)
	}
	_ = c.Close()
	if got := e.Status(); got != "cdnws@b:443" {
		t.Fatalf("status = %q", got)
	}
}

func TestFileStoreRoundTripAndStale(t *testing.T) {
	p := filepath.Join(t.TempDir(), "state.json")
	f := FileStore{Path: p}
	if f.Load().Key != "" {
		t.Fatal("missing file must load empty")
	}
	if err := f.Save(State{Key: "k", Addr: "1.2.3.4:443", At: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if got := f.Load(); got.Key != "k" || got.Addr != "1.2.3.4:443" {
		t.Fatalf("load = %+v", got)
	}
	if err := f.Save(State{Key: "k", At: time.Now().Add(-2 * stateMaxAge)}); err != nil {
		t.Fatal(err)
	}
	if f.Load().Key != "" {
		t.Fatal("stale state must be ignored")
	}
}

// A remembered endpoint is dialled alone on start, skipping the ladder race.
func TestPersistedEndpointSkipsRace(t *testing.T) {
	var dialed atomic.Int32
	var seen = make(chan string, 8)
	d := dialerFunc(func(_ context.Context, ep transport.Endpoint) (transport.Session, error) {
		dialed.Add(1)
		seen <- ep.Addr
		return httpSession{"204 No Content"}, nil
	})
	eps := []transport.Endpoint{{Kind: transport.KindH2, Addr: "a:443"}, {Kind: transport.KindH2, Addr: "b:443"}}
	st := &MemStore{}
	cfg := EngineConfig{
		Endpoints: eps, Dialers: map[transport.Kind]transport.Dialer{transport.KindH2: d},
		Token: func() string { return "t" }, Stagger: time.Hour, Store: st,
	}
	// Seed as if a previous run ended on the second endpoint.
	_ = st.Save(State{Key: key(eps[1]), Addr: "b:443", At: time.Now()})
	e := NewEngine(cfg)
	defer e.Close()
	c, err := e.DialContext(context.Background(), "tcp", "x:1")
	if err != nil {
		t.Fatal(err)
	}
	_ = c.Close()
	if n := dialed.Load(); n != 1 || <-seen != "b:443" {
		t.Fatalf("dialed %d times, want one dial to the remembered endpoint", n)
	}
	if st.Load().Addr != "b:443" {
		t.Fatalf("state = %+v", st.Load())
	}
}

func TestPersistedEndpointFailsOverToLadder(t *testing.T) {
	d := dialerFunc(func(_ context.Context, ep transport.Endpoint) (transport.Session, error) {
		if ep.Addr == "b:443" {
			return nil, errors.New("gone")
		}
		return httpSession{"204 No Content"}, nil
	})
	eps := []transport.Endpoint{{Kind: transport.KindH2, Addr: "a:443"}, {Kind: transport.KindH2, Addr: "b:443"}}
	st := &MemStore{}
	_ = st.Save(State{Key: key(eps[1]), At: time.Now()})
	e := NewEngine(EngineConfig{
		Endpoints: eps, Dialers: map[transport.Kind]transport.Dialer{transport.KindH2: d},
		Token: func() string { return "t" }, Stagger: 5 * time.Millisecond, Store: st,
	})
	defer e.Close()
	c, err := e.DialContext(context.Background(), "tcp", "x:1")
	if err != nil {
		t.Fatal(err)
	}
	_ = c.Close()
	if !strings.HasSuffix(e.Status(), "a:443") {
		t.Fatalf("status = %q", e.Status())
	}
}

func TestExpandCandidatesKeepsServerName(t *testing.T) {
	ep := transport.Endpoint{Kind: transport.KindH2, Addr: "cdn.example:443", Host: "front.example",
		Params: map[string]string{"ips": "1.1.1.1:443, 2.2.2.2:443", "x": "y"}}
	got := expandCandidates(ep)
	if len(got) != 2 || got[0].Addr != "1.1.1.1:443" || got[1].Addr != "2.2.2.2:443" {
		t.Fatalf("expand = %+v", got)
	}
	for _, c := range got {
		if c.ServerName() != ep.ServerName() || c.Params["ips"] != "" || c.Params["x"] != "y" {
			t.Fatalf("candidate = %+v", c)
		}
	}
	if one := expandCandidates(transport.Endpoint{Kind: transport.KindH2, Addr: "a:443"}); len(one) != 1 {
		t.Fatalf("no ips must pass through, got %+v", one)
	}
}

func TestProbeRanksFastestAndDropsDead(t *testing.T) {
	d := dialerFunc(func(_ context.Context, ep transport.Endpoint) (transport.Session, error) {
		switch ep.Addr {
		case "slow:443":
			time.Sleep(80 * time.Millisecond)
		case "dead:443":
			return nil, errors.New("reset")
		}
		return httpSession{"204 No Content"}, nil
	})
	eps := []transport.Endpoint{{Kind: transport.KindH2, Addr: "slow:443"}, {Kind: transport.KindH2, Addr: "dead:443"}, {Kind: transport.KindH2, Addr: "fast:443"}}
	r := Probe(context.Background(), eps, map[transport.Kind]transport.Dialer{transport.KindH2: d}, func() string { return "t" }, DefaultProbeTarget, 2)
	if len(r) != 2 || r[0].Endpoint.Addr != "fast:443" || r[1].Endpoint.Addr != "slow:443" {
		t.Fatalf("ranked = %+v", r)
	}
}
