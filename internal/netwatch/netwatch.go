// Package netwatch reports changes of the physical network on desktop: the
// default gateway or the set of usable IPv4 addresses. Our own tunnel does not
// count: it claims the default route with two /1 routes (leaving the physical
// default in place) and its device sits in the fake-IP range.
package netwatch

import (
	"context"
	"net"
	"net/netip"
	"sort"
	"strings"
	"time"

	"github.com/payam-foundation/dotvpn-client/internal/tun"
)

// Watch calls onChange whenever Signature changes, polling every 2s until ctx
// ends. The first reading is the baseline and raises nothing.
//
// ponytail: polling costs one `route` exec per tick on darwin. Upgrade to a
// route socket (darwin), netlink (linux) or NotifyIpInterfaceChange (windows)
// if that latency or cost matters.
func Watch(ctx context.Context, onChange func()) {
	watch(ctx, 2*time.Second, Signature, onChange)
}

func watch(ctx context.Context, every time.Duration, sig func() string, onChange func()) {
	last := sig()
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		if cur := sig(); cur != last {
			last = cur
			onChange()
		}
	}
}

// Signature identifies the current physical network. It is empty when offline.
func Signature() string {
	ifs, err := net.Interfaces()
	if err != nil {
		return ""
	}
	return signature(ifs, addrsOf, tun.DefaultGateway())
}

func addrsOf(i net.Interface) []net.Addr {
	a, _ := i.Addrs()
	return a
}

func signature(ifs []net.Interface, addrs func(net.Interface) []net.Addr, gw net.IP) string {
	var parts []string
	for _, i := range ifs {
		if i.Flags&net.FlagUp == 0 || i.Flags&(net.FlagLoopback|net.FlagPointToPoint) != 0 {
			continue
		}
		for _, a := range addrs(i) {
			if n, ok := a.(*net.IPNet); ok && usable(n.IP) {
				parts = append(parts, i.Name+"="+n.IP.String())
			}
		}
	}
	if len(parts) == 0 {
		return ""
	}
	sort.Strings(parts)
	return gw.String() + "|" + strings.Join(parts, ",")
}

// tunnelNet is the range of our own TUN device, which can look like an
// ordinary interface (Windows).
var tunnelNet = netip.MustParsePrefix("198.18.0.0/15")

func usable(ip net.IP) bool {
	a, ok := netip.AddrFromSlice(ip.To4())
	return ok && !ip.IsLinkLocalUnicast() && !tunnelNet.Contains(a)
}
