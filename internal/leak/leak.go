// Package leak builds the kill-switch rule sets for every OS as plain data, so
// the invariants that stop leaks (default deny, IPv6 closed, only our own
// sockets and the LAN allowed out) are unit-tested on any host. The sys_*
// files only apply what this package returns.
package leak

import (
	"fmt"
	"strconv"
)

// Private is the LAN space the kill switch leaves open (router, printers).
var Private = []string{"10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16"}

// Linux returns iptables (v4) and ip6tables (v6) chain bodies. Sockets
// carrying mark are the client's own (bypass sockets); tun is the device.
func Linux(tun string, mark int) (v4, v6 [][]string) {
	v4 = [][]string{
		{"-o", "lo", "-j", "RETURN"},
		{"-o", tun, "-j", "RETURN"},
		{"-m", "mark", "--mark", strconv.Itoa(mark), "-j", "RETURN"},
		{"-p", "udp", "--sport", "68", "--dport", "67", "-j", "RETURN"},
	}
	for _, p := range Private {
		v4 = append(v4, []string{"-d", p, "-j", "RETURN"})
	}
	v4 = append(v4, []string{"-j", "REJECT"})
	// IPv6 is never tunnelled: only loopback and the (blackholed) device.
	v6 = [][]string{
		{"-o", "lo", "-j", "RETURN"},
		{"-o", tun, "-j", "RETURN"},
		{"-j", "REJECT"},
	}
	return v4, v6
}

// Darwin returns a pf anchor ruleset. pf has no per-socket mark, so root's
// sockets are the client's (see the ponytail note in sys_darwin.go).
func Darwin(tun, phys string) string {
	return fmt.Sprintf(`pass quick on lo0 all
pass out quick on %[1]s all
block drop out quick on %[2]s inet6 all
pass out quick on %[2]s proto udp from any port 68 to any port 67
pass out quick on %[2]s user root
pass out quick on %[2]s to { %[3]s }
block drop out quick on %[2]s all
`, tun, phys, "10.0.0.0/8 172.16.0.0/12 192.168.0.0/16")
}

// Windows returns netsh firewall rule argument lists. The caller also sets the
// profile policy to blockoutbound, which is what makes everything else deny.
func Windows(prefix, exe, tunAddr string) [][]string {
	return [][]string{
		{"name=" + prefix + "app", "dir=out", "action=allow", "program=" + exe},
		{"name=" + prefix + "tun", "dir=out", "action=allow", "localip=" + tunAddr},
		{"name=" + prefix + "lan", "dir=out", "action=allow", "remoteip=LocalSubnet"},
		{"name=" + prefix + "dhcp", "dir=out", "action=allow", "protocol=udp", "localport=68", "remoteport=67"},
	}
}
