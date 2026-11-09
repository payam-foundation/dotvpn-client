package daemon

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/payam-foundation/dotvpn-client/client"
	"github.com/payam-foundation/dotvpn-client/internal/config"
	"github.com/payam-foundation/dotvpn-client/internal/ipc"
	"github.com/payam-foundation/dotvpn-wire/transport"
)

type echoSession struct{ alive atomic.Bool }

func (s *echoSession) OpenStream(context.Context, string) (net.Conn, error) {
	a, b := net.Pipe()
	go func() { _, _ = io.Copy(b, b); _ = b.Close() }()
	return a, nil
}
func (s *echoSession) Alive() bool  { return s.alive.Load() }
func (s *echoSession) Close() error { s.alive.Store(false); return nil }

type dialerFunc func() (transport.Session, error)

func (f dialerFunc) Dial(context.Context, transport.Endpoint, transport.TokenSource) (transport.Session, error) {
	return f()
}

// fakeEngine builds an engine whose dialer fails failures times, then serves
// echo streams. The created session is sent on sessions.
func fakeEngine(failures int, sessions chan *echoSession) engineFunc {
	var n atomic.Int32
	return func(_ context.Context, _ Options, _ transport.Options, _ *http.Client, onState func(client.Info)) (*client.Engine, error) {
		d := dialerFunc(func() (transport.Session, error) {
			if int(n.Add(1)) <= failures {
				return nil, errors.New("blocked")
			}
			s := &echoSession{}
			s.alive.Store(true)
			sessions <- s
			return s, nil
		})
		return client.NewEngine(client.EngineConfig{
			Endpoints: []transport.Endpoint{{Kind: transport.KindH2, Name: "node", Addr: "n:443"}},
			Dialers:   map[transport.Kind]transport.Dialer{transport.KindH2: d},
			Token:     func() string { return "t" },
			Stagger:   time.Millisecond,
			Cooldown:  time.Millisecond,
			OnState:   onState,
		}), nil
	}
}

func testOptions() Options {
	c := &config.Config{Endpoints: []transport.Endpoint{{Kind: transport.KindH2, Addr: "n:443"}}}
	c.Defaults()
	c.Listen = "127.0.0.1:0"
	c.Tun.SetEnabled(false) // unit tests exercise the proxy path only
	c.Tun.DNSPreset = "custom" // marks intentional proxy-only (not legacy unset)
	return Options{Config: c, Dir: ""}
}

func waitState(t *testing.T, ev <-chan ipc.Status, want ipc.State) ipc.Status {
	t.Helper()
	timeout := time.After(10 * time.Second)
	for {
		select {
		case st := <-ev:
			if st.State == want {
				return st
			}
		case <-timeout:
			t.Fatalf("never reached %s", want)
		}
	}
}

func newSession(failures int) (*Session, chan *echoSession) {
	ch := make(chan *echoSession, 4)
	s := New(slog.New(slog.DiscardHandler))
	s.build = fakeEngine(failures, ch)
	return s, ch
}

func TestLifecycle(t *testing.T) {
	s, sessions := newSession(1)
	ev, cancel := s.Subscribe()
	defer cancel()
	if st := s.Snapshot(); st.State != ipc.StateOff || st.Running {
		t.Fatalf("idle = %+v", st)
	}
	if err := s.Start(testOptions()); err != nil {
		t.Fatal(err)
	}
	if err := s.Start(testOptions()); !errors.Is(err, ErrRunning) {
		t.Fatalf("second Start = %v", err)
	}
	// The first connect fails, the loop retries.
	st := waitState(t, ev, ipc.StateFailed)
	if st.Error == "" || !st.Running {
		t.Fatalf("failed = %+v", st)
	}
	st = waitState(t, ev, ipc.StateConnected)
	if st.Carrier != "h2" || st.Server != "node" || st.Error != "" {
		t.Fatalf("connected = %+v", st)
	}
	<-sessions

	c, err := s.dial(context.Background(), "tcp", "x:1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Write([]byte("hello")); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 5)
	if _, err := io.ReadFull(c, buf); err != nil {
		t.Fatal(err)
	}
	_ = c.Close()
	if st := s.Snapshot(); st.UpBytes != 5 || st.DownBytes != 5 {
		t.Fatalf("counters up=%d down=%d", st.UpBytes, st.DownBytes)
	}
	if sv := s.Servers(); len(sv) != 1 || !sv[0].Active || sv[0].Name != "node" {
		t.Fatalf("servers = %+v", sv)
	}

	s.Stop()
	if st := s.Snapshot(); st.State != ipc.StateOff || st.Running {
		t.Fatalf("after Stop = %+v", st)
	}
	if err := s.Wait(); err != nil {
		t.Fatalf("Wait after Stop = %v", err)
	}
	if _, err := s.dial(context.Background(), "tcp", "x:1"); err == nil {
		t.Fatal("dial after Stop must fail")
	}
	// A stopped session starts again.
	if err := s.Start(testOptions()); err != nil {
		t.Fatal(err)
	}
	waitState(t, ev, ipc.StateConnected)
	s.Stop()
}

// A dead carrier shows as switching, then connected again, with no caller
// action.
func TestReconnectAfterDrop(t *testing.T) {
	s, sessions := newSession(0)
	ev, cancel := s.Subscribe()
	defer cancel()
	if err := s.Start(testOptions()); err != nil {
		t.Fatal(err)
	}
	defer s.Stop()
	waitState(t, ev, ipc.StateConnected)
	(<-sessions).alive.Store(false)
	waitState(t, ev, ipc.StateSwitching)
	waitState(t, ev, ipc.StateConnected)
}

func TestStartRefusals(t *testing.T) {
	s, _ := newSession(0)
	o := testOptions()
	o.Config.Endpoints = nil
	if err := s.Start(o); !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("empty config = %v", err)
	}
	o = testOptions()
	o.Config.Listen = "0.0.0.0:1080"
	if err := s.Start(o); err == nil {
		t.Fatal("off-host proxy without auth must be refused")
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	o = testOptions()
	o.Config.Listen = ln.Addr().String()
	if err := s.Start(o); err == nil {
		t.Fatal("busy port must fail Start")
	}
	if st := s.Snapshot(); st.Running {
		t.Fatalf("failed Start left the session running: %+v", st)
	}
}

// A slow subscriber skips to the latest status and never blocks the session.
func TestSubscribeDoesNotBlock(t *testing.T) {
	s, _ := newSession(0)
	ev, cancel := s.Subscribe()
	for range 10 {
		s.changed()
	}
	<-ev
	cancel()
	s.changed() // after cancel: no delivery, no panic
}
