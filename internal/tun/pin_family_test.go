package tun

import (
	"net"
	"testing"
)

func TestPinHostCompatible(t *testing.T) {
	cases := []struct {
		gw, host string
		ok       bool
	}{
		{"192.168.0.1", "1.2.3.4", true},
		{"192.168.0.1", "2606:4700::1", false},
		{"fe80::1", "2606:4700::1", true},
		{"fe80::1", "1.2.3.4", false},
		{"192.168.0.1", "::1", false},
		{"192.168.0.1", "0.0.0.0", false},
	}
	for _, tc := range cases {
		got := pinHostCompatible(net.ParseIP(tc.gw), net.ParseIP(tc.host))
		if got != tc.ok {
			t.Fatalf("%s via %s: got %v want %v", tc.host, tc.gw, got, tc.ok)
		}
	}
}
