//go:build linux && !android

package tun

import (
	"crypto/rand"
	"encoding/binary"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"strconv"
	"syscall"

	"golang.org/x/sys/unix"
	"golang.zx2c4.com/wireguard/tun"

	"github.com/payam-foundation/dotvpn-client/internal/leak"
)

// fwMark tags the client's own sockets so the kill switch lets them out. It is
// random per process: a fixed value would let any local process that knows it
// skip the kill switch. ponytail: root can still read it with `iptables -S`;
// a cgroup or uid match would close that, at the cost of a systemd dependency.
var fwMark = newMark()

// newMark returns a non-zero mark above the small values other tools use.
func newMark() int {
	var b [4]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return int(binary.BigEndian.Uint32(b[:])&0x7fffffff | 0x10000)
}

// inInitNetNS reports whether we share a network namespace with PID 1.
// resolvectl talks to the host's systemd-resolved and names the link by
// interface index, so inside another namespace (ip netns, a container with
// the host's D-Bus) it would reconfigure an unrelated host link.
func inInitNetNS() bool { return sameNS("/proc/self/ns/net", "/proc/1/ns/net") }

// sameNS compares two namespace links. Unreadable means unknown, which is
// treated as the same: the previous behaviour.
func sameNS(a, b string) bool {
	x, err1 := os.Readlink(a)
	y, err2 := os.Readlink(b)
	return err1 != nil || err2 != nil || x == y
}

func createDevice(name string, mtu int) (tun.Device, error) { return tun.CreateTUN(name, mtu) }

// bindControl pins sockets to the physical interface and marks them. With the
// interface fixed, the kernel ignores the tunnel's catch-all routes.
func bindControl(ifc *net.Interface) func(network, address string, c syscall.RawConn) error {
	return func(_, _ string, c syscall.RawConn) error {
		var serr error
		if err := c.Control(func(fd uintptr) {
			if serr = unix.BindToDevice(int(fd), ifc.Name); serr == nil {
				serr = unix.SetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_MARK, fwMark)
			}
		}); err != nil {
			return err
		}
		return serr
	}
}

const ksChain = "DOTVPN_KS"

func configure(c sysConfig) (restore func(), err error) {
	var u undo
	restore = u.run
	defer func() {
		if err != nil {
			u.run()
			restore = func() {}
		}
	}()
	// Two /1 routes beat the default route without replacing it.
	for _, args := range [][]string{
		{"addr", "add", tunAddr + "/15", "dev", c.Name},
		{"link", "set", c.Name, "mtu", strconv.Itoa(c.MTU), "up"},
		{"route", "add", "0.0.0.0/1", "dev", c.Name},
		{"route", "add", "128.0.0.0/1", "dev", c.Name},
		{"-6", "route", "add", "::/1", "dev", c.Name},
		{"-6", "route", "add", "8000::/1", "dev", c.Name},
	} {
		// IPv6 may be disabled; that is not an error, there is nothing to leak.
		if err := run("ip", args...); err != nil && args[0] != "-6" {
			return nil, err
		}
	}
	if err = linuxDNS(&u, c); err != nil {
		return nil, err
	}
	if c.KillSwitch {
		if err = linuxKillSwitch(&u, c); err != nil {
			return nil, err
		}
	}
	return u.run, nil
}

func linuxDNS(u *undo, c sysConfig) error {
	if _, err := exec.LookPath("resolvectl"); err == nil && inInitNetNS() {
		if err := run("resolvectl", "dns", c.Name, tunDNS); err == nil {
			_ = run("resolvectl", "domain", c.Name, "~.")
			u.add(func() { _ = run("resolvectl", "revert", c.Name) })
			return nil
		}
	}
	const path = "/etc/resolv.conf"
	old, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, []byte("nameserver "+tunDNS+"\n"), 0o644); err != nil {
		return err
	}
	u.add(func() { _ = os.WriteFile(path, old, 0o644) })
	return nil
}

// linuxKillSwitch rejects everything that leaves the machine unless it goes
// into the tunnel, carries our mark, or is local. It outlives a crash on
// purpose; `dotvpn -unblock` removes it.
func linuxKillSwitch(u *undo, c sysConfig) error {
	_ = unblock(slog.Default())
	rules4, rules6 := leak.Linux(c.Name, fwMark)
	for _, t := range []struct {
		bin   string
		rules [][]string
	}{{"iptables", rules4}, {"ip6tables", rules6}} {
		if _, err := exec.LookPath(t.bin); err != nil {
			if t.bin == "iptables" {
				return err
			}
			continue
		}
		if err := run(t.bin, "-N", ksChain); err != nil {
			return err
		}
		for _, r := range t.rules {
			if err := run(t.bin, append([]string{"-A", ksChain}, r...)...); err != nil {
				return err
			}
		}
		if err := run(t.bin, "-I", "OUTPUT", "-j", ksChain); err != nil {
			return err
		}
	}
	u.add(func() { _ = unblock(slog.Default()) })
	return nil
}

func unblock(*slog.Logger) error {
	var first error
	for _, bin := range []string{"iptables", "ip6tables"} {
		if _, err := exec.LookPath(bin); err != nil {
			continue
		}
		for run(bin, "-D", "OUTPUT", "-j", ksChain) == nil {
		}
		_ = run(bin, "-F", ksChain)
		if err := run(bin, "-X", ksChain); err != nil && first == nil && chainExists(bin) {
			first = err
		}
	}
	return first
}

func chainExists(bin string) bool { return exec.Command(bin, "-L", ksChain, "-n").Run() == nil }
