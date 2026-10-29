package leak

import (
	"slices"
	"strings"
	"testing"
)

func TestLinuxDefaultDenyAndIPv6Closed(t *testing.T) {
	v4, v6 := Linux("dotvpn0", 0x6476)
	for name, rules := range map[string][][]string{"v4": v4, "v6": v6} {
		last := rules[len(rules)-1]
		if !slices.Equal(last, []string{"-j", "REJECT"}) {
			t.Errorf("%s: last rule must reject everything, got %v", name, last)
		}
		for _, r := range rules[:len(rules)-1] {
			if r[len(r)-1] != "RETURN" {
				t.Errorf("%s: only RETURN before the final reject, got %v", name, r)
			}
		}
	}
	// IPv6 may only leave through loopback or the tunnel device: no mark, no LAN.
	for _, r := range v6[:len(v6)-1] {
		if r[0] != "-o" {
			t.Errorf("v6 rule beyond lo/tun: %v", r)
		}
	}
	// IPv4 destinations allowed are exactly the private ranges.
	var dst []string
	for _, r := range v4 {
		if r[0] == "-d" {
			dst = append(dst, r[1])
		}
	}
	if !slices.Equal(dst, Private) {
		t.Errorf("v4 destinations = %v", dst)
	}
}

func TestDarwinOrderingNoPublicPass(t *testing.T) {
	rules := Darwin("utun9", "en0")
	lines := strings.Split(strings.TrimSpace(rules), "\n")
	if lines[len(lines)-1] != "block drop out quick on en0 all" {
		t.Errorf("last line = %q", lines[len(lines)-1])
	}
	v6 := slices.Index(lines, "block drop out quick on en0 inet6 all")
	if v6 < 0 {
		t.Fatal("IPv6 on the physical interface is not blocked")
	}
	for i, l := range lines[:v6] {
		if strings.Contains(l, "on en0") {
			t.Errorf("rule %d passes physical traffic before the IPv6 block: %q", i, l)
		}
	}
	for _, l := range lines {
		if strings.HasPrefix(l, "pass out quick on en0") &&
			!strings.Contains(l, "user root") && !strings.Contains(l, "port 68") && !strings.Contains(l, "10.0.0.0/8") {
			t.Errorf("unrestricted pass on physical interface: %q", l)
		}
	}
}

func TestWindowsOnlyLocalSubnet(t *testing.T) {
	for _, r := range Windows("dvpn-ks-", `C:\dotvpn.exe`, "198.18.0.1") {
		for _, a := range r {
			if strings.HasPrefix(a, "remoteip=") && a != "remoteip=LocalSubnet" {
				t.Errorf("rule allows remote %s", a)
			}
			if a == "action=block" {
				t.Errorf("unexpected block rule %v", r)
			}
		}
	}
}
