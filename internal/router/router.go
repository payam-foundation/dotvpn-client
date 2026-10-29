// Package router decides, per flow, whether traffic goes through the tunnel or
// straight out of the physical interface (Iranian domestic sites and IPs).
package router

import (
	"fmt"
	"net/netip"
	"strings"
)

// Router holds the direct rules. The zero value tunnels everything.
type Router struct {
	domains  suffixSet
	cidrs    []netip.Prefix
	geoCIDRs []netip.Prefix
}

type suffixSet struct {
	exact  map[string]struct{}
	suffix map[string]struct{}
}

func (s *suffixSet) add(name string, full bool) {
	name = strings.ToLower(strings.Trim(name, "."))
	if name == "" {
		return
	}
	m := &s.suffix
	if full {
		m = &s.exact
	}
	if *m == nil {
		*m = map[string]struct{}{}
	}
	(*m)[name] = struct{}{}
}

func (s *suffixSet) match(name string) bool {
	if _, ok := s.exact[name]; ok {
		return true
	}
	for n := name; n != ""; {
		if _, ok := s.suffix[n]; ok {
			return true
		}
		_, rest, found := strings.Cut(n, ".")
		if !found {
			break
		}
		n = rest
	}
	return false
}

// Config lists the rules. Domains match themselves and every subdomain; a
// "full:" prefix matches only the exact name.
type Config struct {
	DirectDomains []string
	DirectCIDRs   []string
	// GeoIP and GeoSite are v2ray .dat files; the Direct* lists pick the
	// country or category codes (for example "ir", "category-ir").
	GeoIP         string
	GeoSite       string
	DirectGeoIP   []string
	DirectGeoSite []string
}

// New compiles cfg. Unreadable data files and bad CIDRs are errors, so a typo
// never silently tunnels (or leaks) everything.
func New(cfg Config) (*Router, error) {
	r := &Router{}
	for _, d := range cfg.DirectDomains {
		if full, ok := strings.CutPrefix(d, "full:"); ok {
			r.domains.add(full, true)
		} else {
			r.domains.add(d, false)
		}
	}
	for _, c := range cfg.DirectCIDRs {
		p, err := parsePrefix(c)
		if err != nil {
			return nil, err
		}
		r.cidrs = append(r.cidrs, p)
	}
	if len(cfg.DirectGeoIP) > 0 {
		if cfg.GeoIP == "" {
			return nil, fmt.Errorf("router: direct_geoip needs routing.geoip")
		}
		ps, err := LoadGeoIP(cfg.GeoIP, cfg.DirectGeoIP)
		if err != nil {
			return nil, err
		}
		r.geoCIDRs = ps
	}
	if len(cfg.DirectGeoSite) > 0 {
		if cfg.GeoSite == "" {
			return nil, fmt.Errorf("router: direct_geosite needs routing.geosite")
		}
		if err := r.loadGeoSite(cfg.GeoSite, cfg.DirectGeoSite); err != nil {
			return nil, err
		}
	}
	return r, nil
}

func parsePrefix(s string) (netip.Prefix, error) {
	if p, err := netip.ParsePrefix(s); err == nil {
		return p.Masked(), nil
	}
	a, err := netip.ParseAddr(s)
	if err != nil {
		return netip.Prefix{}, fmt.Errorf("router: bad cidr %q", s)
	}
	return netip.PrefixFrom(a, a.BitLen()), nil
}

// DirectName reports whether a domain bypasses the tunnel.
func (r *Router) DirectName(name string) bool {
	if r == nil {
		return false
	}
	return r.domains.match(strings.ToLower(strings.TrimSuffix(name, ".")))
}

// DirectIP reports whether an address bypasses the tunnel. Private and
// loopback ranges always do: they are never the node's business.
func (r *Router) DirectIP(ip netip.Addr) bool {
	ip = ip.Unmap()
	if ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsMulticast() {
		return true
	}
	if r == nil {
		return false
	}
	for _, p := range r.cidrs {
		if p.Contains(ip) {
			return true
		}
	}
	for _, p := range r.geoCIDRs {
		if p.Contains(ip) {
			return true
		}
	}
	return false
}
