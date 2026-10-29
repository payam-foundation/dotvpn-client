// dotvpn is the client: a local SOCKS5/HTTP proxy and an optional full tunnel.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/payam-foundation/dotvpn-client/client"
	"github.com/payam-foundation/dotvpn-client/internal/app"
	"github.com/payam-foundation/dotvpn-client/internal/config"
	"github.com/payam-foundation/dotvpn-client/internal/router"
	"github.com/payam-foundation/dotvpn-client/internal/tun"
	"github.com/payam-foundation/dotvpn-wire/invite"
	"github.com/payam-foundation/dotvpn-wire/loopback"
	"github.com/payam-foundation/dotvpn-wire/transport"
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
	fs := flag.NewFlagSet("dotvpn", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	cfgFlag := fs.String("config", "", "config file (default: $DOTVPN_CONFIG or the user config dir)")
	verbose := fs.Bool("v", false, "debug logging")
	inv := fs.String("invite", "", "dotvpn:// invite string (saved to the config)")
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
	if err := checkListen(c); err != nil {
		return err
	}
	c.Tun.Enabled = c.Tun.Enabled || *full || *ks
	c.Tun.KillSwitch = c.Tun.KillSwitch || *ks
	return serve(ctx, c, path, log)
}

// checkListen refuses a proxy address reachable off-host unless proxy
// authentication is configured. A unix socket is never valid (TCP listener).
func checkListen(c *config.Config) error {
	_, _, authed, err := c.ProxyAuth()
	if err != nil {
		return err
	}
	if strings.HasPrefix(c.Listen, "unix:") {
		return fmt.Errorf("listen %q: local proxy needs a TCP address", c.Listen)
	}
	if loopback.Check(c.Listen) != nil && !authed {
		return fmt.Errorf("listen %q: a non-loopback proxy needs listen_auth or listen_auth_file", c.Listen)
	}
	return nil
}

func serve(ctx context.Context, c *config.Config, path string, log *slog.Logger) error {
	var opts transport.Options
	var bypass *tun.Bypass
	var ctl *http.Client
	if c.Tun.Enabled {
		// Must exist before the tunnel takes the default route: every socket
		// the client opens itself is pinned to the physical interface.
		var err error
		if bypass, err = tun.NewBypass(c.Tun.DNS); err != nil {
			return err
		}
		opts.Control, opts.Resolver, opts.DNS = bypass.Control, bypass.Resolver(), c.Tun.DNS
		ctl = &http.Client{Timeout: 15 * time.Second, Transport: &http.Transport{
			DialContext: bypass.DialContext, Proxy: nil, ForceAttemptHTTP2: true,
			TLSHandshakeTimeout: 10 * time.Second,
		}}
	}
	eng, err := app.NewEngine(ctx, app.Params{Config: c, Dir: filepath.Dir(path), Opts: opts, HTTP: ctl, Log: log})
	if err != nil {
		return err
	}
	defer eng.Close()

	ln, err := net.Listen("tcp", c.Listen)
	if err != nil {
		return err
	}
	log.Info("local proxy ready (SOCKS5 and HTTP)", "addr", ln.Addr().String())
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	errc := make(chan error, 2)
	lp := &client.LocalProxy{Dial: eng.DialContext, Log: log}
	if u, p, ok, _ := c.ProxyAuth(); ok { // validated by checkListen
		lp.Auth = client.StaticAuth(u, p)
	}
	go func() { errc <- lp.Serve(ctx, ln) }()
	if bypass != nil {
		rt, err := router.New(router.Config{
			DirectDomains: c.Routing.DirectDomains, DirectCIDRs: c.Routing.DirectCIDRs,
			GeoIP: c.Routing.GeoIP, GeoSite: c.Routing.GeoSite,
			DirectGeoIP: c.Routing.DirectGeoIP, DirectGeoSite: c.Routing.DirectGeoSite,
		})
		if err != nil {
			return err
		}
		go func() {
			errc <- tun.Run(ctx, tun.Config{
				Name: c.Tun.Name, MTU: c.Tun.MTU, Tunnel: eng.DialContext, Packet: eng.DialPacket,
				Bypass: bypass, Router: rt, KillSwitch: c.Tun.KillSwitch, Log: log,
			})
		}()
	}
	err = <-errc
	cancel()
	if bypass != nil {
		<-errc // let the tunnel restore routes, DNS and firewall before exiting
	}
	return err
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
