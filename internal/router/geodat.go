package router

import (
	"errors"
	"fmt"
	"net/netip"
	"os"
	"slices"
	"strings"

	"google.golang.org/protobuf/encoding/protowire"
)

// The v2ray geoip.dat / geosite.dat files are protobuf. Only the few fields
// needed are decoded, so no generated code or schema is carried.
//
//	GeoIPList   { repeated GeoIP entry = 1 }
//	GeoIP       { string country_code = 1; repeated CIDR cidr = 2 }
//	CIDR        { bytes ip = 1; uint32 prefix = 2 }
//	GeoSiteList { repeated GeoSite entry = 1 }
//	GeoSite     { string country_code = 1; repeated Domain domain = 2 }
//	Domain      { Type type = 1 (0 keyword, 1 regex, 2 domain, 3 full); string value = 2 }

var errDat = errors.New("router: malformed .dat file")

// fields calls fn for every length-delimited field with the given number.
func fields(b []byte, num protowire.Number, fn func([]byte) error) error {
	for len(b) > 0 {
		n, typ, l := protowire.ConsumeTag(b)
		if l < 0 {
			return errDat
		}
		b = b[l:]
		if typ != protowire.BytesType || n != num {
			if l = protowire.ConsumeFieldValue(n, typ, b); l < 0 {
				return errDat
			}
			b = b[l:]
			continue
		}
		v, l := protowire.ConsumeBytes(b)
		if l < 0 {
			return errDat
		}
		b = b[l:]
		if err := fn(v); err != nil {
			return err
		}
	}
	return nil
}

func varintField(b []byte, num protowire.Number) (uint64, error) {
	var out uint64
	for len(b) > 0 {
		n, typ, l := protowire.ConsumeTag(b)
		if l < 0 {
			return 0, errDat
		}
		b = b[l:]
		if typ == protowire.VarintType && n == num {
			v, l := protowire.ConsumeVarint(b)
			if l < 0 {
				return 0, errDat
			}
			out = v
		}
		if l = protowire.ConsumeFieldValue(n, typ, b); l < 0 {
			return 0, errDat
		}
		b = b[l:]
	}
	return out, nil
}

func wanted(codes []string) func(string) bool {
	lc := make([]string, len(codes))
	for i, c := range codes {
		lc[i] = strings.ToLower(c)
	}
	return func(code string) bool { return slices.Contains(lc, strings.ToLower(code)) }
}

// LoadGeoIP returns the CIDRs of the given country codes.
func LoadGeoIP(path string, codes []string) ([]netip.Prefix, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	want := wanted(codes)
	var out []netip.Prefix
	err = fields(data, 1, func(entry []byte) error {
		var code string
		if err := fields(entry, 1, func(v []byte) error { code = string(v); return nil }); err != nil {
			return err
		}
		if !want(code) {
			return nil
		}
		return fields(entry, 2, func(c []byte) error {
			var ip []byte
			if err := fields(c, 1, func(v []byte) error { ip = v; return nil }); err != nil {
				return err
			}
			bits, err := varintField(c, 2)
			if err != nil {
				return err
			}
			a, ok := netip.AddrFromSlice(ip)
			if !ok || bits > uint64(a.BitLen()) {
				return errDat
			}
			out = append(out, netip.PrefixFrom(a, int(bits)).Masked())
			return nil
		})
	})
	if err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("router: no geoip entries for %v in %s", codes, path)
	}
	return out, nil
}

func (r *Router) loadGeoSite(path string, codes []string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	want := wanted(codes)
	found := false
	err = fields(data, 1, func(entry []byte) error {
		var code string
		if err := fields(entry, 1, func(v []byte) error { code = string(v); return nil }); err != nil {
			return err
		}
		if !want(code) {
			return nil
		}
		found = true
		return fields(entry, 2, func(d []byte) error {
			typ, err := varintField(d, 1)
			if err != nil {
				return err
			}
			var val string
			if err := fields(d, 2, func(v []byte) error { val = string(v); return nil }); err != nil {
				return err
			}
			// Keyword and regex rules are skipped: they cannot be matched
			// by suffix and a wrong guess would leak or break traffic.
			switch typ {
			case 2:
				r.domains.add(val, false)
			case 3:
				r.domains.add(val, true)
			}
			return nil
		})
	})
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("router: no geosite entries for %v in %s", codes, path)
	}
	return nil
}
