package fakedns

import (
	"context"
	"net/netip"
	"testing"

	"github.com/miekg/dns"
)

func query(t *testing.T, s *Server, name string, qt uint16) *dns.Msg {
	t.Helper()
	m := new(dns.Msg)
	m.SetQuestion(dns.Fqdn(name), qt)
	b, _ := m.Pack()
	out := s.Answer(context.Background(), b)
	r := new(dns.Msg)
	if err := r.Unpack(out); err != nil {
		t.Fatal(err)
	}
	return r
}

func TestAnswer(t *testing.T) {
	real := netip.MustParseAddr("5.6.7.8")
	s := New(Pool, func(n string) bool { return n == "local.ir" },
		func(context.Context, string) ([]netip.Addr, error) { return []netip.Addr{real}, nil })

	a := query(t, s, "Example.com", dns.TypeA).Answer[0].(*dns.A)
	ip, _ := netip.AddrFromSlice(a.A)
	if !s.InPool(ip) {
		t.Fatalf("%v not in pool", ip)
	}
	if n, _ := s.Name(ip); n != "example.com" {
		t.Fatalf("name = %q", n)
	}
	b := query(t, s, "example.com", dns.TypeA).Answer[0].(*dns.A)
	if !b.A.Equal(a.A) {
		t.Fatal("same name must keep its address")
	}
	if len(query(t, s, "example.com", dns.TypeAAAA).Answer) != 0 || len(query(t, s, "example.com", dns.TypeHTTPS).Answer) != 0 {
		t.Fatal("non-A must be empty")
	}

	d := query(t, s, "local.ir", dns.TypeA).Answer[0].(*dns.A)
	if got, _ := netip.AddrFromSlice(d.A); got != real || !s.IsDirectIP(real) || s.InPool(got) {
		t.Fatalf("direct name must resolve for real, got %v", got)
	}
}

func TestRecycle(t *testing.T) {
	s := New(netip.MustParsePrefix("198.18.0.0/28"), nil, nil)
	s.size = 20 // reserved(16) + 3 usable
	first := s.alloc("a")
	s.alloc("b")
	s.alloc("c")
	s.alloc("d") // wraps onto a
	if _, ok := s.byName["a"]; ok && s.byIP[first] != "a" {
		t.Fatal("stale mapping kept")
	}
	if s.byIP[first] == "a" {
		t.Fatal("a must have been recycled")
	}
}
