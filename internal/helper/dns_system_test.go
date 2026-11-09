package helper

import (
	"net"
	"testing"
)

func TestIsFakeTunHost(t *testing.T) {
	if !isFakeTunHost("198.18.0.2") || !isFakeTunHost("198.18.0.2:53") {
		t.Fatal("expected fake tun host")
	}
	if isFakeTunHost("1.1.1.1") || isFakeTunHost("192.168.1.1") {
		t.Fatal("real resolvers must not look fake")
	}
}

func TestBypassDNS(t *testing.T) {
	if got := bypassDNS("cloudflare", ""); got != "1.1.1.1:53" {
		t.Fatalf("cloudflare = %q", got)
	}
	if got := bypassDNS("custom", "9.9.9.9"); got != "9.9.9.9:53" {
		t.Fatalf("host-only custom = %q", got)
	}
	if got := bypassDNS("custom", "198.18.0.2:53"); got != bypassDNSFallback {
		t.Fatalf("fake tun DNS must fall back, got %q", got)
	}
	got := bypassDNS("system", "system")
	if got == "" || got == "system" || isFakeTunHost(got) {
		t.Fatalf("system preset = %q", got)
	}
	if _, _, err := net.SplitHostPort(got); err != nil {
		t.Fatalf("system preset not dialable: %q: %v", got, err)
	}
}

func TestEnsureDNSPort(t *testing.T) {
	if got := ensureDNSPort("1.1.1.1"); got != "1.1.1.1:53" {
		t.Fatal(got)
	}
	if got := ensureDNSPort("1.1.1.1:53"); got != "1.1.1.1:53" {
		t.Fatal(got)
	}
}
