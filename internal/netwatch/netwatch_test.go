package netwatch

import (
	"context"
	"net"
	"sync/atomic"
	"testing"
	"time"
)

func TestWatchFiresOnlyOnChange(t *testing.T) {
	var cur atomic.Value
	cur.Store("wifi")
	var fired atomic.Int32
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		watch(ctx, 5*time.Millisecond, func() string { return cur.Load().(string) }, func() { fired.Add(1) })
		close(done)
	}()
	time.Sleep(40 * time.Millisecond)
	if n := fired.Load(); n != 0 {
		t.Fatalf("fired %d times on a stable network", n)
	}
	cur.Store("cell")
	time.Sleep(40 * time.Millisecond)
	if n := fired.Load(); n != 1 {
		t.Fatalf("fired %d times, want 1", n)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("watch ignored ctx")
	}
}

func TestSignatureIgnoresTunnelAndNoise(t *testing.T) {
	ip := func(s string) []net.Addr {
		return []net.Addr{&net.IPNet{IP: net.ParseIP(s), Mask: net.CIDRMask(24, 32)}}
	}
	ifs := []net.Interface{
		{Name: "en0", Flags: net.FlagUp},
		{Name: "utun4", Flags: net.FlagUp | net.FlagPointToPoint},
		{Name: "lo0", Flags: net.FlagUp | net.FlagLoopback},
		{Name: "en1", Flags: 0},
		{Name: "wintun", Flags: net.FlagUp},
	}
	addrs := map[string][]net.Addr{
		"en0":   append(ip("192.168.1.5"), &net.IPNet{IP: net.ParseIP("fe80::1")}),
		"utun4": ip("198.18.0.1"), "wintun": ip("198.18.0.1"), "lo0": ip("127.0.0.1"), "en1": ip("10.0.0.2"),
	}
	get := func(i net.Interface) []net.Addr { return addrs[i.Name] }
	gw := net.ParseIP("192.168.1.1")
	base := signature(ifs, get, gw)
	if base != "192.168.1.1|en0=192.168.1.5" {
		t.Fatalf("signature = %q", base)
	}
	addrs["utun4"] = ip("198.18.0.9") // tunnel churn is not a roam
	if signature(ifs, get, gw) != base {
		t.Fatal("tunnel address changed the signature")
	}
	addrs["en0"] = ip("10.1.1.7")
	if signature(ifs, get, gw) == base {
		t.Fatal("new physical address kept the signature")
	}
	addrs["en0"] = nil
	if signature(ifs, get, gw) != "" {
		t.Fatal("offline signature must be empty")
	}
}
