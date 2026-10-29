package tun

import (
	"encoding/binary"
	"fmt"
	"log/slog"
	"net"
	"os"
	"strconv"
	"syscall"

	"golang.org/x/sys/windows"
	"golang.zx2c4.com/wireguard/tun"

	"github.com/payam-foundation/dotvpn-client/internal/leak"
)

// ipUnicastIf is IP_UNICAST_IF and IPV6_UNICAST_IF (both 31): send out of the
// given interface regardless of the routing table.
const ipUnicastIf = 31

func createDevice(name string, mtu int) (tun.Device, error) {
	tun.WintunTunnelType = "dotvpn"
	return tun.CreateTUN(name, mtu)
}

func bindControl(ifc *net.Interface) func(network, address string, c syscall.RawConn) error {
	return func(network, _ string, c syscall.RawConn) error {
		var serr error
		if err := c.Control(func(fd uintptr) {
			h := windows.Handle(fd)
			if network == "tcp6" || network == "udp6" {
				serr = windows.SetsockoptInt(h, windows.IPPROTO_IPV6, ipUnicastIf, ifc.Index)
				return
			}
			// IPv4 wants the index in network byte order.
			var b [4]byte
			binary.BigEndian.PutUint32(b[:], uint32(ifc.Index))
			serr = windows.SetsockoptInt(h, windows.IPPROTO_IP, ipUnicastIf, int(binary.LittleEndian.Uint32(b[:])))
		}); err != nil {
			return err
		}
		return serr
	}
}

const ksPrefix = "dotvpn-ks-"

func configure(c sysConfig) (restore func(), err error) {
	var u undo
	restore = u.run
	defer func() {
		if err != nil {
			u.run()
			restore = func() {}
		}
	}()
	// The address is already set by the netsh call below; routes use the
	// on-link gateway of the tunnel address.
	if err := u.step(c.Log, nil, "netsh", "interface", "ip", "set", "address", "name="+c.Name, "static", tunAddr, "255.254.0.0"); err != nil {
		return nil, err
	}
	_ = run("netsh", "interface", "ipv4", "set", "subinterface", c.Name, "mtu="+strconv.Itoa(c.MTU), "store=active")
	for _, dst := range []string{"0.0.0.0/1", "128.0.0.0/1"} {
		if err := u.step(c.Log, []string{"netsh", "interface", "ipv4", "delete", "route", dst, c.Name},
			"netsh", "interface", "ipv4", "add", "route", dst, c.Name, tunDNS, "metric=1", "store=active"); err != nil {
			return nil, err
		}
	}
	for _, dst := range []string{"::/1", "8000::/1"} {
		// IPv6 may be absent; there is then nothing to leak.
		_ = u.step(c.Log, []string{"netsh", "interface", "ipv6", "delete", "route", dst, c.Name},
			"netsh", "interface", "ipv6", "add", "route", dst, c.Name, "metric=1", "store=active")
	}
	if err := u.step(c.Log, []string{"netsh", "interface", "ip", "set", "dnsservers", "name=" + c.Name, "source=dhcp"},
		"netsh", "interface", "ip", "set", "dnsservers", "name="+c.Name, "static", tunDNS, "validate=no"); err != nil {
		return nil, err
	}
	// Make the tunnel the preferred resolver path over the physical NIC.
	_ = run("netsh", "interface", "ipv4", "set", "interface", c.Name, "metric=1")
	if c.KillSwitch {
		if err = windowsKillSwitch(&u, c); err != nil {
			return nil, err
		}
	}
	return u.run, nil
}

// windowsKillSwitch flips the firewall to default-deny outbound and allows the
// client binary, the tunnel address, DHCP and the local subnet.
func windowsKillSwitch(u *undo, c sysConfig) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	_ = unblock(slog.Default())
	for _, r := range leak.Windows(ksPrefix, exe, tunAddr) {
		if err := run("netsh", append([]string{"advfirewall", "firewall", "add", "rule"}, r...)...); err != nil {
			return err
		}
	}
	if err := run("netsh", "advfirewall", "set", "allprofiles", "firewallpolicy", "blockinbound,blockoutbound"); err != nil {
		return fmt.Errorf("%w (run as Administrator)", err)
	}
	u.add(func() { _ = unblock(slog.Default()) })
	return nil
}

func unblock(*slog.Logger) error {
	for _, n := range []string{"app", "tun", "lan", "dhcp"} {
		_ = run("netsh", "advfirewall", "firewall", "delete", "rule", "name="+ksPrefix+n)
	}
	return run("netsh", "advfirewall", "set", "allprofiles", "firewallpolicy", "blockinbound,allowoutbound")
}
