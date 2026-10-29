package tun

import (
	"fmt"
	"log/slog"
	"net"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"syscall"

	"golang.org/x/sys/unix"
	"golang.zx2c4.com/wireguard/tun"

	"github.com/payam-foundation/dotvpn-client/internal/leak"
)

var utunName = regexp.MustCompile(`^utun\d*$`)

func createDevice(name string, mtu int) (tun.Device, error) {
	if !utunName.MatchString(name) {
		name = "utun" // the kernel picks the next free number
	}
	return tun.CreateTUN(name, mtu)
}

// bindControl pins sockets to the physical interface (IP_BOUND_IF).
func bindControl(ifc *net.Interface) func(network, address string, c syscall.RawConn) error {
	return func(network, _ string, c syscall.RawConn) error {
		var serr error
		if err := c.Control(func(fd uintptr) {
			if strings.HasSuffix(network, "6") {
				serr = unix.SetsockoptInt(int(fd), unix.IPPROTO_IPV6, unix.IPV6_BOUND_IF, ifc.Index)
			} else {
				serr = unix.SetsockoptInt(int(fd), unix.IPPROTO_IP, unix.IP_BOUND_IF, ifc.Index)
			}
		}); err != nil {
			return err
		}
		return serr
	}
}

const pfAnchor = "dotvpn"

func configure(c sysConfig) (restore func(), err error) {
	var u undo
	restore = u.run
	defer func() {
		if err != nil {
			u.run()
			restore = func() {}
		}
	}()
	if err := u.step(c.Log, nil, "ifconfig", c.Name, "inet", tunAddr, tunDNS, "mtu", strconv.Itoa(c.MTU), "up"); err != nil {
		return nil, err
	}
	for _, r := range []struct {
		family, dst string
	}{{"-inet", "0.0.0.0/1"}, {"-inet", "128.0.0.0/1"}, {"-inet6", "::/1"}, {"-inet6", "8000::/1"}} {
		err := u.step(c.Log, []string{"route", "-n", "delete", r.family, "-net", r.dst, "-interface", c.Name},
			"route", "-n", "add", r.family, "-net", r.dst, "-interface", c.Name)
		// IPv6 may be absent; there is then nothing to leak.
		if err != nil && r.family == "-inet" {
			return nil, err
		}
	}
	if err = darwinDNS(&u, c); err != nil {
		return nil, err
	}
	if c.KillSwitch {
		if err = darwinKillSwitch(&u, c); err != nil {
			return nil, err
		}
	}
	return u.run, nil
}

// darwinDNS points the network service of the physical interface at the fake
// resolver and restores its previous servers on exit.
func darwinDNS(u *undo, c sysConfig) error {
	svc, err := networkService(c.Phys.Name)
	if err != nil {
		return err
	}
	out, err := exec.Command("networksetup", "-getdnsservers", svc).Output()
	if err != nil {
		return err
	}
	prev := []string{"Empty"} // networksetup's word for "use DHCP's servers"
	if s := strings.TrimSpace(string(out)); !strings.Contains(s, "aren't any") {
		prev = strings.Fields(s)
	}
	if err := run("networksetup", "-setdnsservers", svc, tunDNS); err != nil {
		return err
	}
	u.add(func() { _ = run("networksetup", append([]string{"-setdnsservers", svc}, prev...)...) })
	return nil
}

func networkService(dev string) (string, error) {
	out, err := exec.Command("networksetup", "-listallhardwareports").Output()
	if err != nil {
		return "", err
	}
	var port string
	for _, l := range strings.Split(string(out), "\n") {
		switch {
		case strings.HasPrefix(l, "Hardware Port: "):
			port = strings.TrimPrefix(l, "Hardware Port: ")
		case strings.TrimSpace(l) == "Device: "+dev:
			return port, nil
		}
	}
	return "", fmt.Errorf("no network service for %s", dev)
}

// darwinKillSwitch uses pf. The client runs as root, so root's sockets are the
// only ones allowed out of the physical interface besides local traffic.
//
// ponytail: other root processes are also allowed out; a per-socket mark
// does not exist in pf.
func darwinKillSwitch(u *undo, c sysConfig) error {
	rules := leak.Darwin(c.Name, c.Phys.Name)
	cmd := exec.Command("pfctl", "-a", pfAnchor, "-f", "-")
	cmd.Stdin = strings.NewReader(rules)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("pfctl: %v: %s", err, out)
	}
	// Anchors only run when pf is on; remember whether we turned it on.
	was := exec.Command("sh", "-c", "pfctl -s info 2>/dev/null | grep -q 'Status: Enabled'").Run() == nil
	if !was {
		_ = run("pfctl", "-e")
	}
	u.add(func() {
		_ = unblock(slog.Default())
		if !was {
			_ = run("pfctl", "-d")
		}
	})
	return nil
}

func unblock(*slog.Logger) error { return run("pfctl", "-a", pfAnchor, "-F", "all") }
