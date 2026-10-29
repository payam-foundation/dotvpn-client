package tun

import (
	"fmt"
	"log/slog"
	"net"
	"os/exec"
	"strings"
)

const (
	// tunAddr is the device address and tunDNS the resolver the OS is pointed
	// at; both sit in the reserved head of the fake DNS pool.
	tunAddr = "198.18.0.1"
	tunDNS  = "198.18.0.2"
)

// sysConfig is what the OS-specific configure needs.
type sysConfig struct {
	Name       string
	MTU        int
	Phys       *net.Interface
	KillSwitch bool
	Log        *slog.Logger
}

// undo collects cleanup actions and runs them in reverse order.
type undo []func()

func (u *undo) add(f func()) { *u = append(*u, f) }

func (u undo) run() {
	for i := len(u) - 1; i >= 0; i-- {
		u[i]()
	}
}

func run(name string, args ...string) error {
	out, err := exec.Command(name, args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s %s: %v: %s", name, strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}

// step runs a command now and registers its inverse (nil for none).
func (u *undo) step(log *slog.Logger, inverse []string, name string, args ...string) error {
	if err := run(name, args...); err != nil {
		return err
	}
	if inverse != nil {
		u.add(func() {
			if err := run(inverse[0], inverse[1:]...); err != nil {
				log.Debug("tun cleanup", "err", err)
			}
		})
	}
	return nil
}

// Unblock removes kill-switch rules left behind by a crashed client.
func Unblock(log *slog.Logger) error { return unblock(log) }
