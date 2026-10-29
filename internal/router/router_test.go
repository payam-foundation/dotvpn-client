package router

import (
	"net/netip"
	"os"
	"path/filepath"
	"testing"

	"google.golang.org/protobuf/encoding/protowire"
)

func msg(parts ...func([]byte) []byte) []byte {
	var b []byte
	for _, p := range parts {
		b = p(b)
	}
	return b
}

func str(n protowire.Number, s string) func([]byte) []byte {
	return func(b []byte) []byte {
		return protowire.AppendString(protowire.AppendTag(b, n, protowire.BytesType), s)
	}
}

func raw(n protowire.Number, v []byte) func([]byte) []byte {
	return func(b []byte) []byte { return protowire.AppendBytes(protowire.AppendTag(b, n, protowire.BytesType), v) }
}

func num(n protowire.Number, v uint64) func([]byte) []byte {
	return func(b []byte) []byte {
		return protowire.AppendVarint(protowire.AppendTag(b, n, protowire.VarintType), v)
	}
}

func TestRules(t *testing.T) {
	dir := t.TempDir()
	cidr := msg(raw(1, []byte{5, 160, 0, 0}), num(2, 11))
	geoip := msg(raw(1, msg(str(1, "IR"), raw(2, cidr))), raw(1, msg(str(1, "US"), raw(2, msg(raw(1, []byte{8, 8, 8, 0}), num(2, 24))))))
	geosite := msg(raw(1, msg(str(1, "CATEGORY-IR"),
		raw(2, msg(num(1, 2), str(2, "ir"))),
		raw(2, msg(num(1, 3), str(2, "www.only.com"))),
		raw(2, msg(num(1, 0), str(2, "keyword"))))))
	ip, gs := filepath.Join(dir, "geoip.dat"), filepath.Join(dir, "geosite.dat")
	if err := os.WriteFile(ip, geoip, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(gs, geosite, 0o600); err != nil {
		t.Fatal(err)
	}
	r, err := New(Config{
		DirectDomains: []string{"example.ir", "full:exact.org"},
		DirectCIDRs:   []string{"9.9.9.9", "100.64.0.0/10"},
		GeoIP:         ip, DirectGeoIP: []string{"ir"},
		GeoSite: gs, DirectGeoSite: []string{"category-ir"},
	})
	if err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string]bool{
		"example.ir": true, "a.b.example.ir": true, "x.ir": true, "notexample.com": false,
		"exact.org": true, "sub.exact.org": false, "www.only.com": true, "only.com": false,
		"keyword.com": false, "google.com": false, "EXAMPLE.IR.": true,
	} {
		if got := r.DirectName(name); got != want {
			t.Errorf("DirectName(%q) = %v", name, got)
		}
	}
	for s, want := range map[string]bool{
		"5.160.1.1": true, "5.192.0.1": false, "8.8.8.8": false, "9.9.9.9": true,
		"100.100.0.1": true, "192.168.1.1": true, "127.0.0.1": true, "1.1.1.1": false,
	} {
		if got := r.DirectIP(netip.MustParseAddr(s)); got != want {
			t.Errorf("DirectIP(%s) = %v", s, got)
		}
	}
	if _, err := New(Config{DirectGeoIP: []string{"zz"}, GeoIP: ip}); err == nil {
		t.Error("unknown country code must fail")
	}
	if _, err := New(Config{DirectCIDRs: []string{"nope"}}); err == nil {
		t.Error("bad cidr must fail")
	}
}
