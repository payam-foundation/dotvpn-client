// SPDX-License-Identifier: AGPL-3.0-only

package tun

import "net"

// pinHostCompatible reports whether host can be pinned via gateway. Darwin
// (and typical IPv4-only default routes) cannot add an IPv6 -host route with
// an IPv4 gateway.
func pinHostCompatible(gateway, host net.IP) bool {
	if gateway == nil || host == nil || host.IsUnspecified() || host.IsLoopback() {
		return false
	}
	return (host.To4() != nil) == (gateway.To4() != nil)
}
