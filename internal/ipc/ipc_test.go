package ipc

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

type fake struct {
	mu       sync.Mutex
	st       Status
	subs     []chan Status
	imported string
	settings Settings
	calls    []string
}

func (f *fake) Status() Status { f.mu.Lock(); defer f.mu.Unlock(); return f.st }
func (f *fake) Subscribe() (<-chan Status, func()) {
	ch := make(chan Status, 4)
	f.mu.Lock()
	f.subs = append(f.subs, ch)
	f.mu.Unlock()
	return ch, func() {}
}
func (f *fake) set(st Status) {
	f.mu.Lock()
	f.st = st
	subs := f.subs
	f.mu.Unlock()
	for _, c := range subs {
		c <- st
	}
}
func (f *fake) Connect() error {
	f.mu.Lock()
	f.calls = append(f.calls, "connect")
	f.mu.Unlock()
	if !f.st.Configured {
		return Coded(CodeNotConfigured, errors.New("no invite"))
	}
	f.set(Status{State: StateConnected, Running: true, Configured: true})
	return nil
}
func (f *fake) Disconnect() { f.set(Status{State: StateOff, Configured: true}) }
func (f *fake) ImportInvite(u string) error {
	if u != "dotvpn://ok" {
		return Coded(CodeBadInvite, errors.New("bad"))
	}
	f.mu.Lock()
	f.imported = u
	f.st.Configured = true
	f.mu.Unlock()
	return nil
}
func (f *fake) Servers() []Server { return []Server{{ID: "a", Name: "n", Carrier: "h3", Active: true}} }
func (f *fake) Settings() Settings {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.settings
}
func (f *fake) SetSettings(s Settings) error {
	f.mu.Lock()
	f.settings = s
	f.mu.Unlock()
	return nil
}
func (f *fake) Unblock() error { return nil }
func (f *fake) Account() (Account, error) {
	return Account{Name: "test", QuotaLimit: 0, MaxDevices: 1}, nil
}
func (f *fake) Relays() ([]Relay, error) { return []Relay{{ID: "r1", Name: "relay", Tier: "curated", Health: "ok"}}, nil }
func (f *fake) AddRelay(string) error    { return nil }
func (f *fake) RemoveRelay(string) error { return nil }
func (f *fake) Logout() error            { f.imported = ""; f.set(Status{State: StateOff, Configured: false}); return nil }

func start(t *testing.T) (*Client, *fake) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("unix socket test")
	}
	// Unix socket paths are short-limited; t.TempDir can exceed it on macOS.
	dir, err := os.MkdirTemp("", "ipc")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	addr := filepath.Join(dir, "s.sock")
	ln, err := Listen(addr, "")
	if err != nil {
		t.Fatal(err)
	}
	f := &fake{}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { _ = Serve(ctx, ln, f); close(done) }()
	t.Cleanup(func() { cancel(); <-done })
	return &Client{Addr: addr, Timeout: 5 * time.Second}, f
}

func TestCalls(t *testing.T) {
	c, f := start(t)
	ctx := context.Background()
	if err := c.Connect(ctx); !errors.Is(err, &Error{Code: CodeNotConfigured}) {
		t.Fatalf("connect before import = %v", err)
	}
	if err := c.ImportInvite(ctx, "junk"); !errors.Is(err, &Error{Code: CodeBadInvite}) {
		t.Fatalf("bad invite = %v", err)
	}
	if err := c.ImportInvite(ctx, "dotvpn://ok"); err != nil || f.imported != "dotvpn://ok" {
		t.Fatalf("import = %v %q", err, f.imported)
	}
	if err := c.Connect(ctx); err != nil {
		t.Fatal(err)
	}
	st, err := c.Status(ctx)
	if err != nil || st.State != StateConnected {
		t.Fatalf("status = %+v %v", st, err)
	}
	sv, err := c.Servers(ctx)
	if err != nil || len(sv) != 1 || sv[0].Carrier != "h3" {
		t.Fatalf("servers = %+v %v", sv, err)
	}
	want := Settings{FullTunnel: true, KillSwitch: true, DNS: "9.9.9.9:53", DirectDomains: []string{"a.ir"}}
	if err := c.SetSettings(ctx, want); err != nil {
		t.Fatal(err)
	}
	got, err := c.Settings(ctx)
	if err != nil || got.DNS != want.DNS || !got.KillSwitch || len(got.DirectDomains) != 1 {
		t.Fatalf("settings = %+v %v", got, err)
	}
	if err := c.Disconnect(ctx); err != nil {
		t.Fatal(err)
	}
	if err := c.Unblock(ctx); err != nil {
		t.Fatal(err)
	}
	a, err := c.Account(ctx)
	if err != nil || a.Name != "test" || a.MaxDevices != 1 {
		t.Fatalf("account = %+v %v", a, err)
	}
	rs, err := c.Relays(ctx)
	if err != nil || len(rs) != 1 || rs[0].ID != "r1" || rs[0].Tier != "curated" {
		t.Fatalf("relays = %+v %v", rs, err)
	}
	if err := c.AddRelay(ctx, "dotvpn-relay://abc"); err != nil {
		t.Fatal(err)
	}
	if err := c.RemoveRelay(ctx, "abc"); err != nil {
		t.Fatal(err)
	}
}

func TestSubscribe(t *testing.T) {
	c, f := start(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ev, err := c.Subscribe(ctx)
	if err != nil {
		t.Fatal(err)
	}
	next := func() Status {
		select {
		case st := <-ev:
			return st
		case <-time.After(5 * time.Second):
			t.Fatal("no event")
			return Status{}
		}
	}
	if st := next(); st.State != "" { // current status first
		t.Fatalf("initial = %+v", st)
	}
	f.set(Status{State: StateConnecting, Running: true})
	if st := next(); st.State != StateConnecting {
		t.Fatalf("event = %+v", st)
	}
	cancel()
	select {
	case _, ok := <-ev:
		if ok {
			for range ev {
			}
		}
	case <-time.After(5 * time.Second):
		t.Fatal("channel not closed after cancel")
	}
}

func TestVersionMismatch(t *testing.T) {
	c, _ := start(t)
	conn, err := net.Dial("unix", c.Addr)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_, _ = conn.Write([]byte(`{"v":99,"method":"Status"}` + "\n"))
	buf := make([]byte, 512)
	n, _ := conn.Read(buf)
	if !strings.Contains(string(buf[:n]), CodeVersion) {
		t.Fatalf("response = %s", buf[:n])
	}
}

func TestSocketIsPrivate(t *testing.T) {
	c, _ := start(t)
	fi, err := os.Stat(c.Addr)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Fatalf("socket mode = %v, want 0600", fi.Mode().Perm())
	}
}

func TestSecondListenerRefused(t *testing.T) {
	c, _ := start(t)
	if ln, err := Listen(c.Addr, ""); err == nil {
		_ = ln.Close()
		t.Fatal("a second helper must not take over a live socket")
	}
}

func TestStaleSocketIsReplaced(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip()
	}
	dir, _ := os.MkdirTemp("", "ipc")
	defer os.RemoveAll(dir)
	addr := filepath.Join(dir, "s.sock")
	ln, err := net.Listen("unix", addr)
	if err != nil {
		t.Fatal(err)
	}
	ln.(*net.UnixListener).SetUnlinkOnClose(false)
	_ = ln.Close() // a crash leaves the file behind
	l2, err := Listen(addr, "")
	if err != nil {
		t.Fatalf("stale socket blocked startup: %v", err)
	}
	_ = l2.Close()
}

func TestNoHelper(t *testing.T) {
	c := &Client{Addr: filepath.Join(os.TempDir(), "dotvpn-absent.sock")}
	if _, err := c.Status(context.Background()); err == nil {
		t.Fatal("expected error")
	}
}
