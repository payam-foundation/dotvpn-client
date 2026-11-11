// Package mobile is the gomobile surface of the client (ADR-019). The Android
// VpnService owns the TUN device, routes, DNS and kill switch; this package
// runs the engine and the gVisor front on the descriptor it hands over.
//
// Only gomobile-safe types cross the boundary: string, int, bool, error and the
// Protector interface.
package mobile

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"golang.zx2c4.com/wireguard/tun"

	"github.com/payam-foundation/dotvpn-client/client"
	"github.com/payam-foundation/dotvpn-client/internal/app"
	"github.com/payam-foundation/dotvpn-client/internal/config"
	"github.com/payam-foundation/dotvpn-client/internal/diag"
	"github.com/payam-foundation/dotvpn-client/internal/router"
	inttun "github.com/payam-foundation/dotvpn-client/internal/tun"
	"github.com/payam-foundation/dotvpn-wire/invite"
	"github.com/payam-foundation/dotvpn-wire/transport"
)

// AppVersion is set by the Android shell / gomobile build.
var AppVersion = "dev"

var mobileRing = diag.NewRing("android", AppVersion)

// SetAppVersion records the shell version for diagnostics bundles.
func SetAppVersion(v string) {
	v = strings.TrimSpace(v)
	if v == "" {
		return
	}
	AppVersion = v
	mobileRing.SetVersion(v)
}

// NormalizeConfig runs defaults/migrations (mints device_id) and returns YAML.
func NormalizeConfig(cfg string) (string, error) {
	c, err := config.Parse([]byte(cfg))
	if err != nil {
		return "", err
	}
	_ = c.Migrate()
	b, err := config.Marshal(c)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// Protector exempts a socket from the VPN (VpnService.protect). Every socket
// the client opens itself goes through it first, or it would loop into the
// tunnel.
type Protector interface {
	Protect(fd int) bool
}

// ImportInvite turns a dotvpn:// invite into a client config (YAML) with the
// full tunnel enabled. The app stores it and passes it to Start.
func ImportInvite(uri string) (string, error) {
	i, err := invite.DecodeInvite(uri)
	if err != nil {
		return "", err
	}
	c := &config.Config{}
	c.Defaults()
	c.Apply(i)
	c.Tun.SetEnabled(true)
	b, err := config.Marshal(c)
	return string(b), err
}

// Endpoints lists the carriers in cfg, one label per line, for the server list
// screen. Servers learned later from the control plane are not included.
func Endpoints(cfg string) (string, error) {
	c, err := config.Parse([]byte(cfg))
	if err != nil {
		return "", err
	}
	var b strings.Builder
	for _, ep := range c.Endpoints {
		b.WriteString(ep.Label())
		b.WriteByte('\n')
	}
	return b.String(), nil
}

type session struct {
	cancel context.CancelFunc
	done   chan struct{}
	eng    *client.Engine
}

var (
	mu     sync.Mutex
	cur    *session
	status = "off"
)

// newDevice wraps the VpnService descriptor; tests replace it.
var newDevice = deviceFromFD

// Start brings the tunnel up on tunFd (the descriptor from
// VpnService.Builder.establish, ownership passes to Go). cfg is the YAML from
// ImportInvite; dataDir is app-private storage for state.json and the server
// list cache. It returns once the engine is running; Status reports the
// carrier once connected.
func Start(cfg string, dataDir string, tunFd int, p Protector) error {
	dev, err := newDevice(tunFd)
	if err != nil {
		return err
	}
	if err := start(cfg, dataDir, dev, p); err != nil {
		_ = dev.Close()
		return err
	}
	return nil
}

func start(cfg, dataDir string, dev tun.Device, p Protector) error {
	if p == nil {
		return errors.New("mobile: protector required")
	}
	mu.Lock()
	defer mu.Unlock()
	if cur != nil {
		return errors.New("mobile: already started")
	}
	c, err := config.Parse([]byte(cfg))
	if err != nil {
		return fmt.Errorf("mobile: config: %w", err)
	}
	_ = c.Migrate() // mint device_id; Android should persist via NormalizeConfig
	// Warn and above only: logcat is readable by other tools on rooted or
	// debug devices, and Info lines name endpoints.
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	if c.Tun.KillSwitch {
		log.Warn("guard ignored on mobile; use system Always-on block")
	}
	mobileRing.Note(diag.Event{Code: diag.CodeStart})
	bypass := inttun.NewProtectedBypass(c.Tun.DNS, p.Protect)
	opts := transport.Options{Control: bypass.Control, Resolver: bypass.Resolver(), DNS: c.Tun.DNS}
	ctl := &http.Client{Timeout: 15 * time.Second, Transport: &http.Transport{
		DialContext: bypass.DialContext, Proxy: nil, ForceAttemptHTTP2: true,
		TLSHandshakeTimeout: 10 * time.Second,
	}}
	ctx, cancel := context.WithCancel(context.Background())
	eng, err := app.NewEngine(ctx, app.Params{Config: c, Dir: dataDir, Opts: opts, HTTP: ctl, Log: log})
	if err != nil {
		cancel()
		return err
	}
	rt, err := router.New(router.Config{
		DirectDomains: c.Routing.DirectDomains, DirectCIDRs: c.Routing.DirectCIDRs,
		GeoIP: c.Routing.GeoIP, GeoSite: c.Routing.GeoSite,
		DirectGeoIP: c.Routing.DirectGeoIP, DirectGeoSite: c.Routing.DirectGeoSite,
	})
	if err != nil {
		cancel()
		_ = eng.Close()
		return err
	}
	s := &session{cancel: cancel, done: make(chan struct{}), eng: eng}
	cur, status = s, "connecting"
	go func() {
		defer close(s.done)
		err := inttun.Run(ctx, inttun.Config{
			MTU: c.Tun.MTU, Tunnel: eng.DialContext, Packet: eng.DialPacket,
			Bypass: bypass, Router: rt, Device: dev, Log: log,
		})
		cancel()
		_ = eng.Close()
		mu.Lock()
		if cur == s {
			cur = nil
			status = "off"
			if err != nil {
				status = "error: " + err.Error()
			}
		}
		mu.Unlock()
	}()
	go keepConnected(ctx, eng)
	return nil
}

// keepConnected reconnects whenever the session drops (network change,
// airplane mode) instead of waiting for the next flow to trigger it.
func keepConnected(ctx context.Context, eng *client.Engine) {
	for ctx.Err() == nil {
		wait := 2 * time.Second
		if err := eng.Connect(ctx); err != nil {
			mobileRing.Note(diag.Event{Code: diag.CodeConnectFail, Detail: "retry"})
			wait = 5 * time.Second
		} else {
			info := eng.Info()
			mobileRing.Note(diag.Event{Code: diag.CodeConnectOK, EndpointID: client.EndpointID(info.Endpoint)})
		}
		select {
		case <-ctx.Done():
		case <-time.After(wait):
		}
	}
}

// Stop ends the tunnel and waits for it to close the descriptor.
func Stop() {
	mu.Lock()
	s := cur
	mu.Unlock()
	if s == nil {
		return
	}
	mobileRing.Note(diag.Event{Code: diag.CodeStop})
	s.cancel()
	<-s.done
}

// Status is "off", "connecting", "connected <carrier>" or "error: <reason>".
func Status() string {
	mu.Lock()
	defer mu.Unlock()
	if cur != nil {
		if l := cur.eng.Status(); l != "" {
			return "connected " + l
		}
	}
	return status
}

// ReportDiagnostics submits a scrubbed bundle to controlURL with accountKey
// (from the invite config). Returns "url|via" for the Android UI.
func ReportDiagnostics(cfgYAML, controlURL, accountKey string) (string, error) {
	mobileRing.SetVersion(AppVersion)
	mobileRing.SetState(coarseState(Status()))
	b := mobileRing.Snapshot()
	if controlURL == "" || accountKey == "" {
		if c, err := config.Parse([]byte(cfgYAML)); err == nil {
			accountKey = c.Account
			if len(c.Control) > 0 {
				controlURL = c.Control[0].URL
			}
		}
	}
	sub := &diag.ClientSubmit{ControlURL: controlURL, AccountKey: accountKey}
	res, err := sub.Submit(context.Background(), b)
	if err != nil {
		return "", err
	}
	return res.IssueURL + "|" + res.Via, nil
}

// coarseState strips carrier names and error text from Status() for L0.
func coarseState(s string) string {
	switch {
	case s == "off" || s == "connecting":
		return s
	case strings.HasPrefix(s, "connected"):
		return "connected"
	case strings.HasPrefix(s, "error:"):
		return "error"
	default:
		return "unknown"
	}
}
