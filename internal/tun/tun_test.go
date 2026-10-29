package tun

import (
	"context"
	"io"
	"net"
	"net/netip"
	"sync"
	"testing"
	"time"

	wgnet "golang.zx2c4.com/wireguard/tun/netstack"

	"github.com/payam-foundation/dotvpn-client/internal/router"
)

// echoConn returns a connection whose far end echoes everything.
func echoConn() net.Conn {
	a, b := net.Pipe()
	go func() { _, _ = io.Copy(b, b); _ = b.Close() }()
	return a
}

func TestFlows(t *testing.T) {
	// The "OS": an in-memory TUN device backed by an independent TCP/IP stack.
	dev, client, err := wgnet.CreateNetTUN([]netip.Addr{netip.MustParseAddr("198.18.0.1")}, nil, 1380)
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var tunneled, direct []string
	rt, err := router.New(router.Config{DirectDomains: []string{"local.test"}})
	if err != nil {
		t.Fatal(err)
	}
	cfg := Config{
		MTU: 1380, Router: rt,
		Tunnel: func(_ context.Context, _, addr string) (net.Conn, error) {
			mu.Lock()
			tunneled = append(tunneled, addr)
			mu.Unlock()
			return echoConn(), nil
		},
		Bypass: &Bypass{
			resolver: &net.Resolver{PreferGo: true, Dial: func(context.Context, string, string) (net.Conn, error) {
				return nil, io.EOF
			}},
			dial: func(_ context.Context, _, addr string) (net.Conn, error) {
				mu.Lock()
				direct = append(direct, addr)
				mu.Unlock()
				return echoConn(), nil
			},
		},
	}
	ctx, cancel := context.WithCancel(context.Background())
	e, err := newEngine(ctx, cfg, dev)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- e.run(cancel) }()
	defer func() {
		cancel()
		if err := <-done; err != nil {
			t.Errorf("run: %v", err)
		}
	}()

	roundTrip := func(addr string) {
		t.Helper()
		dctx, c := context.WithTimeout(ctx, 5*time.Second)
		defer c()
		conn, err := client.DialContext(dctx, "tcp", addr)
		if err != nil {
			t.Fatalf("dial %s: %v", addr, err)
		}
		defer conn.Close()
		_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
		if _, err := conn.Write([]byte("ping")); err != nil {
			t.Fatal(err)
		}
		buf := make([]byte, 4)
		if _, err := io.ReadFull(conn, buf); err != nil || string(buf) != "ping" {
			t.Fatalf("echo via %s: %q %v", addr, buf, err)
		}
	}

	// Tunnelled name: DNS gives a fake address, the flow reaches the tunnel by name.
	r := &net.Resolver{PreferGo: true, Dial: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return client.DialContext(ctx, "udp", "198.18.0.2:53")
	}}
	ips, err := r.LookupNetIP(ctx, "ip4", "blocked.example")
	if err != nil || len(ips) != 1 || !e.dns.InPool(ips[0]) {
		t.Fatalf("lookup: %v %v", ips, err)
	}
	roundTrip(net.JoinHostPort(ips[0].String(), "443"))

	// Private destination: straight out of the physical interface.
	roundTrip("10.9.9.9:80")
	// Public address with no name: tunnelled by address.
	roundTrip("8.8.8.8:443")

	mu.Lock()
	defer mu.Unlock()
	if len(tunneled) != 2 || tunneled[0] != "blocked.example:443" || tunneled[1] != "8.8.8.8:443" {
		t.Errorf("tunneled = %v", tunneled)
	}
	if len(direct) != 1 || direct[0] != "10.9.9.9:80" {
		t.Errorf("direct = %v", direct)
	}
}

// Leak policy: UDP that is not DNS (STUN/WebRTC, QUIC) is dropped inside the
// tunnel, never forwarded out, and no tunnel or bypass dial is made for it.
func TestNonDNSUDPDroppedNoLeak(t *testing.T) {
	dev, client, err := wgnet.CreateNetTUN([]netip.Addr{netip.MustParseAddr("198.18.0.1")}, nil, 1380)
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var dials []string
	rec := func(_ context.Context, _, addr string) (net.Conn, error) {
		mu.Lock()
		dials = append(dials, addr)
		mu.Unlock()
		return nil, io.EOF
	}
	rt, _ := router.New(router.Config{})
	cfg := Config{MTU: 1380, Router: rt, Tunnel: rec, Bypass: &Bypass{
		resolver: &net.Resolver{PreferGo: true, Dial: func(context.Context, string, string) (net.Conn, error) { return nil, io.EOF }},
		dial:     rec,
	}}
	ctx, cancel := context.WithCancel(context.Background())
	e, err := newEngine(ctx, cfg, dev)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- e.run(cancel) }()
	defer func() { cancel(); <-done }()

	for _, dst := range []string{"stun.l.google.com:3478", "8.8.8.8:443", "1.1.1.1:19302"} {
		c, err := client.DialContext(ctx, "udp", dst)
		if err != nil {
			continue // refused outright is also fine
		}
		_ = c.SetDeadline(time.Now().Add(500 * time.Millisecond))
		_, _ = c.Write([]byte("\x00\x01\x00\x00\x21\x12\xa4\x42stunstunstun"))
		if n, err := c.Read(make([]byte, 64)); err == nil {
			t.Errorf("%s answered %d bytes: UDP leaked", dst, n)
		}
		_ = c.Close()
	}
	mu.Lock()
	defer mu.Unlock()
	if len(dials) != 0 {
		t.Errorf("non-DNS UDP caused dials: %v", dials)
	}
}

// Non-DNS UDP goes through Config.Packet (tunnel) or the bypass (direct
// destinations), by name when the address is a fake one, and nowhere else.
func TestUDPFlows(t *testing.T) {
	dev, client, err := wgnet.CreateNetTUN([]netip.Addr{netip.MustParseAddr("198.18.0.1")}, nil, 1380)
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var tunneled, direct []string
	echo := func() net.Conn {
		a, b := net.Pipe()
		go func() {
			buf := make([]byte, 2048)
			for {
				n, err := b.Read(buf)
				if err != nil {
					return
				}
				if _, err := b.Write(buf[:n]); err != nil {
					return
				}
			}
		}()
		return a
	}
	rt, _ := router.New(router.Config{})
	cfg := Config{
		MTU: 1380, Router: rt,
		Tunnel: func(context.Context, string, string) (net.Conn, error) { return nil, io.EOF },
		Packet: func(_ context.Context, addr string) (net.Conn, error) {
			mu.Lock()
			tunneled = append(tunneled, addr)
			mu.Unlock()
			return echo(), nil
		},
		Bypass: &Bypass{
			resolver: &net.Resolver{PreferGo: true, Dial: func(context.Context, string, string) (net.Conn, error) { return nil, io.EOF }},
			dial: func(_ context.Context, network, addr string) (net.Conn, error) {
				mu.Lock()
				direct = append(direct, network+" "+addr)
				mu.Unlock()
				return echo(), nil
			},
		},
	}
	ctx, cancel := context.WithCancel(context.Background())
	e, err := newEngine(ctx, cfg, dev)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- e.run(cancel) }()
	defer func() { cancel(); <-done }()

	exchange := func(dst string) {
		t.Helper()
		c, err := client.DialContext(ctx, "udp", dst)
		if err != nil {
			t.Fatalf("dial %s: %v", dst, err)
		}
		defer c.Close()
		for _, m := range []string{"quic-initial", "x"} {
			_ = c.SetDeadline(time.Now().Add(5 * time.Second))
			if _, err := c.Write([]byte(m)); err != nil {
				t.Fatal(err)
			}
			buf := make([]byte, 64)
			if n, err := c.Read(buf); err != nil || string(buf[:n]) != m {
				t.Fatalf("%s: got %q, %v", dst, buf[:n], err)
			}
		}
	}
	exchange("8.8.8.8:443")   // by address through the tunnel
	exchange("10.9.9.9:3478") // private: direct
	r := &net.Resolver{PreferGo: true, Dial: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return client.DialContext(ctx, "udp", "198.18.0.2:53")
	}}
	ips, err := r.LookupNetIP(ctx, "ip4", "stun.example")
	if err != nil || len(ips) != 1 {
		t.Fatalf("lookup: %v %v", ips, err)
	}
	exchange(net.JoinHostPort(ips[0].String(), "3478")) // by name through the tunnel

	mu.Lock()
	defer mu.Unlock()
	if len(tunneled) != 2 || tunneled[0] != "8.8.8.8:443" || tunneled[1] != "stun.example:3478" {
		t.Errorf("tunneled = %v", tunneled)
	}
	if len(direct) != 1 || direct[0] != "udp 10.9.9.9:3478" {
		t.Errorf("direct = %v", direct)
	}
}
