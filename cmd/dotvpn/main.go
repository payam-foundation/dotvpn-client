// dotvpn is the client: a local SOCKS5/HTTP proxy and an optional full tunnel.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"syscall"

	"github.com/payam-foundation/dotvpn-client/internal/config"
	"github.com/payam-foundation/dotvpn-client/internal/daemon"
	"github.com/payam-foundation/dotvpn-client/internal/ipc"
	"github.com/payam-foundation/dotvpn-client/internal/tun"
	"github.com/payam-foundation/dotvpn-wire/invite"
)

var version = "dev"

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	err := run(ctx, os.Args[1:])
	if errors.Is(err, flag.ErrHelp) {
		return
	}
	if err != nil {
		slog.Error("dotvpn failed", "err", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string) error {
	if len(args) > 0 && args[0] == "version" {
		fmt.Println(version)
		return nil
	}
	if ok, err := subcommand(ctx, args); ok {
		return err
	}
	fs := flag.NewFlagSet("dotvpn", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	cfgFlag := fs.String("config", "", "config file (default: $DOTVPN_CONFIG or the user config dir)")
	verbose := fs.Bool("v", false, "debug logging")
	inv := fs.String("invite", "", "dv:// invite string (saved to the config)")
	listen := fs.String("listen", "", "local proxy address, loopback only (default 127.0.0.1:1080)")
	full := fs.Bool("tun", false, "full tunnel (needs root/Administrator)")
	ks := fs.Bool("kill-switch", false, "block all traffic outside the tunnel (implies -tun)")
	unblock := fs.Bool("unblock", false, "remove kill-switch rules left by a crashed client, then exit")
	if err := fs.Parse(args); err != nil {
		return err
	}
	lvl := slog.LevelInfo
	if *verbose {
		lvl = slog.LevelDebug
	}
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: lvl}))
	slog.SetDefault(log)
	if *unblock {
		return tun.Unblock(log)
	}

	path := configPath(*cfgFlag)
	c, err := config.Load(path)
	switch {
	case err == nil:
	case os.IsNotExist(err) && *inv != "":
		c = &config.Config{}
		c.Defaults()
	default:
		return err
	}
	if *inv != "" {
		i, err := invite.DecodeInvite(*inv)
		if err != nil {
			return err
		}
		c.Apply(i)
		if err := config.Save(path, c); err != nil {
			return err
		}
	}
	if *listen != "" {
		c.Listen = *listen
	}
	if err := c.CheckListen(); err != nil {
		return err
	}
	c.Tun.SetEnabled(c.Tun.TunnelOn() || *full || *ks)
	c.Tun.KillSwitch = c.Tun.KillSwitch || *ks
	return serve(ctx, c, path, log)
}

func serve(ctx context.Context, c *config.Config, path string, log *slog.Logger) error {
	sess := daemon.New(log)
	ev, cancel := sess.Subscribe()
	defer cancel()
	if err := sess.Start(daemon.Options{Config: c, Dir: filepath.Dir(path)}); err != nil {
		return err
	}
	go announce(ev, useColor(os.Stdout, os.Getenv("NO_COLOR")))
	done := make(chan error, 1)
	go func() { done <- sess.Wait() }()
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		sess.Stop() // restores routes, DNS and firewall before we exit
		return nil
	}
}

func configPath(flagVal string) string {
	if flagVal != "" {
		return flagVal
	}
	if v := os.Getenv("DOTVPN_CONFIG"); v != "" {
		return v
	}
	if runtime.GOOS != "windows" && os.Geteuid() == 0 {
		return "/etc/dotvpn/client.yaml"
	}
	d, err := os.UserConfigDir()
	if err != nil {
		d = "."
	}
	return filepath.Join(d, "dotvpn", "client.yaml")
}

// announce prints "connected via <carrier>" on stdout each time the active
// carrier changes, so a person at a terminal sees the state without log noise.
func announce(ev <-chan ipc.Status, color bool) {
	last := ""
	for st := range ev {
		cur := ""
		if st.State == ipc.StateConnected {
			cur = st.Carrier
		}
		if cur != last {
			last = cur
			if cur != "" {
				fmt.Println(connectedLine(cur, color))
			}
		}
	}
}

// connectedLine colours only the word that means connected: Dot VPN lime
// (#C6F432), per the design guidelines.
func connectedLine(carrier string, color bool) string {
	word := "connected"
	if color {
		word = "\x1b[38;2;198;244;50m" + word + "\x1b[0m"
	}
	return word + " via " + carrier
}

// useColor is true only for a terminal, and never when NO_COLOR is set to a
// non-empty value (https://no-color.org).
func useColor(f *os.File, noColor string) bool {
	if noColor != "" {
		return false
	}
	fi, err := f.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}
