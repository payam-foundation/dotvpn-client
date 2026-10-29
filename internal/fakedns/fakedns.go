// Package fakedns answers DNS queries for the full tunnel. Names that must go
// through the tunnel get a synthetic address from a reserved pool, so the
// flow that follows can be sent to the node by name and the (possibly
// poisoned) local resolver is never consulted. Names that go direct are
// resolved for real, and their addresses are remembered so the flow is also
// sent direct even when the address is not in any direct CIDR.
package fakedns

import (
	"context"
	"net/netip"
	"strings"
	"sync"

	"github.com/miekg/dns"
)

// Pool is the default synthetic range (RFC 2544 benchmarking space).
var Pool = netip.MustParsePrefix("198.18.0.0/15")

const (
	reserved   = 16 // first addresses of the pool are for the tunnel itself
	ttl        = 60
	maxRealIPs = 1 << 16
)

// Server maps names to synthetic addresses and back.
type Server struct {
	// Direct reports names that bypass the tunnel.
	Direct func(name string) bool
	// Resolve returns real IPv4 addresses for a direct name.
	Resolve func(ctx context.Context, name string) ([]netip.Addr, error)

	pool netip.Prefix
	size uint32

	mu     sync.Mutex
	next   uint32
	byName map[string]netip.Addr
	byIP   map[netip.Addr]string
	real   map[netip.Addr]struct{}
}

// New returns a Server allocating from pool.
func New(pool netip.Prefix, direct func(string) bool, resolve func(context.Context, string) ([]netip.Addr, error)) *Server {
	return &Server{
		Direct:  direct,
		Resolve: resolve,
		pool:    pool,
		size:    uint32(1) << (32 - pool.Bits()),
		next:    reserved,
		byName:  map[string]netip.Addr{},
		byIP:    map[netip.Addr]string{},
		real:    map[netip.Addr]struct{}{},
	}
}

// Name returns the name a synthetic address was handed out for.
func (s *Server) Name(ip netip.Addr) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	n, ok := s.byIP[ip]
	return n, ok
}

// InPool reports whether ip belongs to the synthetic range.
func (s *Server) InPool(ip netip.Addr) bool { return s.pool.Contains(ip) }

// IsDirectIP reports whether ip was returned for a direct name.
func (s *Server) IsDirectIP(ip netip.Addr) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.real[ip]
	return ok
}

// alloc returns the synthetic address of name. When the pool wraps, the oldest
// mapping is recycled.
func (s *Server) alloc(name string) netip.Addr {
	s.mu.Lock()
	defer s.mu.Unlock()
	if ip, ok := s.byName[name]; ok {
		return ip
	}
	b := s.pool.Addr().As4()
	base := uint32(b[0])<<24 | uint32(b[1])<<16 | uint32(b[2])<<8 | uint32(b[3])
	n := base + s.next
	ip := netip.AddrFrom4([4]byte{byte(n >> 24), byte(n >> 16), byte(n >> 8), byte(n)})
	if s.next++; s.next >= s.size-1 {
		s.next = reserved
	}
	if old, ok := s.byIP[ip]; ok {
		delete(s.byName, old)
	}
	s.byName[name] = ip
	s.byIP[ip] = name
	return ip
}

func (s *Server) markReal(ips []netip.Addr) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.real) > maxRealIPs {
		clear(s.real)
	}
	for _, ip := range ips {
		s.real[ip] = struct{}{}
	}
}

// Answer builds the reply to a DNS query packet. Only IPv4 is tunnelled, so
// AAAA and every other type get an empty NOERROR answer, which makes clients
// fall back to A.
func (s *Server) Answer(ctx context.Context, q []byte) []byte {
	req := new(dns.Msg)
	if req.Unpack(q) != nil || len(req.Question) != 1 {
		return nil
	}
	resp := new(dns.Msg)
	resp.SetReply(req)
	resp.RecursionAvailable = true
	qs := req.Question[0]
	if qs.Qtype == dns.TypeA && qs.Qclass == dns.ClassINET {
		name := strings.ToLower(strings.TrimSuffix(qs.Name, "."))
		ips, err := s.addrs(ctx, name)
		if err != nil {
			resp.Rcode = dns.RcodeServerFailure
		}
		for _, ip := range ips {
			resp.Answer = append(resp.Answer, &dns.A{
				Hdr: dns.RR_Header{Name: qs.Name, Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: ttl},
				A:   ip.AsSlice(),
			})
		}
	}
	out, err := resp.Pack()
	if err != nil {
		return nil
	}
	return out
}

func (s *Server) addrs(ctx context.Context, name string) ([]netip.Addr, error) {
	if s.Direct != nil && s.Direct(name) {
		ips, err := s.Resolve(ctx, name)
		if err != nil {
			return nil, err
		}
		s.markReal(ips)
		return ips, nil
	}
	return []netip.Addr{s.alloc(name)}, nil
}
