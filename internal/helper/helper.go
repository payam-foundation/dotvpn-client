// Package helper is the privileged side of the app: it owns the config file
// and one daemon.Session, and implements ipc.Backend so the GUI and CLI can
// drive them without ever seeing a token or a node address.
package helper

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/payam-foundation/dotvpn-client/client"
	"github.com/payam-foundation/dotvpn-client/internal/app"
	"github.com/payam-foundation/dotvpn-client/internal/config"
	"github.com/payam-foundation/dotvpn-client/internal/crash"
	"github.com/payam-foundation/dotvpn-client/internal/daemon"
	"github.com/payam-foundation/dotvpn-client/internal/diag"
	"github.com/payam-foundation/dotvpn-client/internal/ipc"
	"github.com/payam-foundation/dotvpn-client/internal/tun"
	"github.com/payam-foundation/dotvpn-wire/invite"
	"github.com/payam-foundation/dotvpn-wire/transport"
)

// AppVersion is set by the helper process (ldflags / main).
var AppVersion = "dev"

// Helper implements ipc.Backend.
type Helper struct {
	log   *slog.Logger
	path  string
	sess  *daemon.Session
	crash crash.Reporter
	ring  *diag.Ring
	// unblock removes leftover firewall rules; tests replace it.
	unblock func(*slog.Logger) error

	mu sync.Mutex // guards the config file

	probeMu  sync.Mutex
	probeMS  map[string]int
	probeErr map[string]string
}

// New returns a Helper keeping its config at path.
func New(log *slog.Logger, path string) *Helper {
	h := &Helper{
		log: log, path: path, sess: daemon.New(log), unblock: tun.Unblock,
		ring: diag.NewRing("desktop", AppVersion),
	}
	h.crash = crash.New(crash.Options{
		Release: AppVersion,
		Load: func() bool {
			c, err := config.Load(path)
			return err == nil && c.CrashReporting
		},
		// Persist is nil: SetSettings owns the config write so we never
		// re-enter h.mu from inside SetEnabled (non-reentrant mutex).
	})
	return h
}

// Run clears kill-switch rules a crashed helper left behind, then serves ln
// until ctx ends. It stops the session on the way out so routes, DNS and
// firewall are restored.
func (h *Helper) Run(ctx context.Context, ln net.Listener) error {
	defer crash.Recover()
	if err := h.unblock(h.log); err != nil {
		h.log.Warn("startup unblock failed", "err", err)
	}
	err := ipc.Serve(ctx, ln, h)
	h.sess.Stop()
	return err
}

// load reads the config; a missing file is an empty, unconfigured one.
// Callers that mutate must hold h.mu. Migrate may rewrite the file.
func (h *Helper) load() (*config.Config, error) {
	c, err := config.Load(h.path)
	if errors.Is(err, os.ErrNotExist) {
		c = &config.Config{}
		c.Defaults()
		_ = c.Migrate()
		return c, nil
	}
	if err != nil {
		return nil, err
	}
	if c.Migrate() {
		if err := config.Save(h.path, c); err != nil {
			h.log.Warn("config migrate save failed", "err", err)
		} else {
			h.log.Info("ready", "schema", c.Schema, "full", c.Tun.TunnelOn())
		}
	}
	return c, nil
}

func configured(c *config.Config) bool {
	return c.Account != "" || len(c.Control) > 0 || len(c.Endpoints) > 0
}

func (h *Helper) isConfigured() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	c, err := h.load()
	return err == nil && configured(c)
}

func (h *Helper) Status() ipc.Status {
	st := h.sess.Snapshot()
	st.Configured = h.isConfigured()
	st.PublicIP = h.cachedPublicIP()
	if st.Country == "" && (st.State == ipc.StateConnected || st.State == ipc.StateConnecting) {
		for _, sv := range h.Servers() {
			if sv.Country == "" {
				continue
			}
			if sv.Active || (st.Server != "" && strings.EqualFold(sv.Name, st.Server)) {
				st.Country = sv.Country
				break
			}
		}
		if st.Country == "" {
			for _, sv := range h.Servers() {
				if sv.Country != "" {
					st.Country = sv.Country
					break
				}
			}
		}
	}
	return st
}

// Subscribe forwards session events with Configured filled in.
func (h *Helper) Subscribe() (<-chan ipc.Status, func()) {
	in, stop := h.sess.Subscribe()
	out := make(chan ipc.Status, 1)
	done := make(chan struct{})
	var once sync.Once
	go func() {
		defer close(out)
		for {
			select {
			case st := <-in:
				st.Configured = h.isConfigured()
				st.PublicIP = h.cachedPublicIP()
				if st.Country == "" && (st.State == ipc.StateConnected || st.State == ipc.StateConnecting) {
					for _, sv := range h.Servers() {
						if sv.Country == "" {
							continue
						}
						if sv.Active || (st.Server != "" && strings.EqualFold(sv.Name, st.Server)) {
							st.Country = sv.Country
							break
						}
					}
					if st.Country == "" {
						for _, sv := range h.Servers() {
							if sv.Country != "" {
								st.Country = sv.Country
								break
							}
						}
					}
				}
				select {
				case out <- st:
				default:
					select {
					case <-out:
					default:
					}
					out <- st
				}
			case <-done:
				return
			}
		}
	}()
	return out, func() { once.Do(func() { stop(); close(done) }) }
}

func (h *Helper) Connect() error {
	h.mu.Lock()
	c, err := h.load()
	if err != nil {
		h.mu.Unlock()
		return err
	}
	on := c.Tun.TunnelOn()
	// Resolve "system" (and empty) to a real dial address before Start. The
	// session/bypass dial path cannot handle the preset keyword.
	cfg := *c
	cfg.Tun.DNS = bypassDNS(c.Tun.DNSPreset, c.Tun.DNS)
	h.mu.Unlock()
	h.log.Info("connect", "full", on, "guard", c.Tun.KillSwitch, "schema", c.Schema, "n", len(c.Endpoints))
	if !on {
		h.log.Warn("connect", "mode", "proxy")
	}
	err = h.sess.Start(daemon.Options{Config: &cfg, Dir: filepath.Dir(h.path), Diag: h.ring})
	switch {
	case errors.Is(err, daemon.ErrNotConfigured):
		return ipc.Coded(ipc.CodeNotConfigured, err)
	case errors.Is(err, daemon.ErrRunning):
		return ipc.Coded(ipc.CodeBusy, err)
	}
	if err != nil {
		h.log.Warn("connect failed", "err", err)
	}
	return err
}

func (h *Helper) Disconnect() {
	h.log.Info("disconnect")
	h.sess.Stop()
}

func (h *Helper) ImportInvite(uri string) (ipc.ImportResult, error) {
	i, err := invite.DecodeInvite(uri)
	if err != nil {
		return ipc.ImportResult{}, ipc.Coded(ipc.CodeBadInvite, err)
	}
	h.mu.Lock()
	c, err := h.load()
	if err != nil {
		h.mu.Unlock()
		return ipc.ImportResult{}, err
	}
	c.Apply(i)
	c.Pin = "" // the old ladder is gone
	if err := config.Save(h.path, c); err != nil {
		h.mu.Unlock()
		return ipc.ImportResult{}, err
	}
	base, key := "", ""
	if c.Account != "" && len(c.Control) > 0 {
		base, key = c.Control[0].URL, c.Account
	}
	h.mu.Unlock()
	if base == "" || key == "" {
		return ipc.ImportResult{}, nil
	}
	if !probeControl(base, key) {
		h.log.Info("import saved; control unreachable")
		return ipc.ImportResult{NeedsRelay: true}, nil
	}
	return ipc.ImportResult{}, nil
}

// Logout disconnects and wipes invite/account material from this device.
// Tun/routing/carrier preferences stay so the next invite inherits them.
func (h *Helper) Logout() error {
	h.Disconnect()
	h.mu.Lock()
	defer h.mu.Unlock()
	c, err := h.load()
	if err != nil {
		return err
	}
	c.Account = ""
	c.Token = ""
	c.Bootstrap = ""
	c.TrustedKey = nil
	c.Control = nil
	c.Endpoints = nil
	c.Relays = nil
	c.Pin = ""
	return config.Save(h.path, c)
}

func (h *Helper) Servers() []ipc.Server {
	h.mu.Lock()
	c, err := h.load()
	h.mu.Unlock()
	ccByID, ccByName := map[string]string{}, map[string]string{}
	soleCC, mixedCC := "", false
	if err == nil {
		for _, ep := range c.Endpoints {
			if ep.Params == nil {
				continue
			}
			cc := ep.Params["cc"]
			if cc == "" {
				continue
			}
			ccByID[client.EndpointID(ep)] = cc
			name := ep.Name
			if name == "" {
				name = string(ep.Kind)
			}
			ccByName[strings.ToLower(name)] = cc
			if soleCC == "" {
				soleCC = cc
			} else if soleCC != cc {
				mixedCC = true
			}
		}
		if mixedCC {
			soleCC = ""
		}
	}
	fillCountry := func(sv *ipc.Server) {
		if sv.Country != "" {
			return
		}
		if cc := ccByID[sv.ID]; cc != "" {
			sv.Country = cc
			return
		}
		if cc := ccByName[strings.ToLower(sv.Name)]; cc != "" {
			sv.Country = cc
			return
		}
		// Discovery renames endpoints and drops Params["cc"]; when the invite
		// only knows one country, stamp it so the flag still shows.
		sv.Country = soleCC
	}
	if s := h.sess.Servers(); s != nil {
		for i := range s {
			fillCountry(&s[i])
		}
		// Active session country fills any rung the signed list left blank
		// (stale cache / invite without Params["cc"]).
		if st := h.sess.Snapshot(); st.Country != "" {
			for i := range s {
				if s[i].Country == "" {
					s[i].Country = st.Country
				}
			}
		}
		h.applyProbe(s)
		return s
	}
	if err != nil {
		return nil
	}
	var out []ipc.Server
	for _, ep := range c.Endpoints {
		name := ep.Name
		if name == "" {
			name = string(ep.Kind)
		}
		cc := ""
		if ep.Params != nil {
			cc = ep.Params["cc"]
		}
		sv := ipc.Server{ID: client.EndpointID(ep), Name: name, Country: cc, Carrier: string(ep.Kind)}
		fillCountry(&sv)
		out = append(out, sv)
	}
	h.applyProbe(out)
	return out
}

// ProbeServers dials each ladder rung once and returns servers with latency.
func (h *Helper) ProbeServers() ([]ipc.Server, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	h.log.Info("probe servers")
	if h.sess.ProbeAll(ctx) {
		return h.Servers(), nil
	}
	h.mu.Lock()
	c, err := h.load()
	h.mu.Unlock()
	if err != nil {
		return nil, err
	}
	if !configured(c) {
		return nil, ipc.Coded(ipc.CodeNotConfigured, daemon.ErrNotConfigured)
	}
	cfg := *c
	cfg.Tun.DNS = bypassDNS(c.Tun.DNSPreset, c.Tun.DNS)
	eng, err := app.NewEngine(ctx, app.Params{
		Config: &cfg, Dir: filepath.Dir(h.path),
		Opts: transport.Options{}, Log: h.log,
	})
	if err != nil {
		h.log.Warn("probe skipped", "err", err)
		return h.Servers(), nil
	}
	defer eng.Close()
	eng.ProbeAll(ctx)
	var out []ipc.Server
	for _, x := range eng.Health() {
		name := x.Endpoint.Name
		if name == "" {
			name = string(x.Endpoint.Kind)
		}
		cc := ""
		if x.Endpoint.Params != nil {
			cc = x.Endpoint.Params["cc"]
		}
		out = append(out, ipc.Server{
			ID: x.ID, Name: name, Country: cc, Carrier: string(x.Endpoint.Kind),
			LatencyMS: int(x.Latency.Milliseconds()), Error: x.Err,
		})
	}
	h.storeProbe(out)
	return h.Servers(), nil
}

func (h *Helper) storeProbe(sv []ipc.Server) {
	h.probeMu.Lock()
	defer h.probeMu.Unlock()
	h.probeMS = map[string]int{}
	h.probeErr = map[string]string{}
	for _, s := range sv {
		h.probeMS[s.ID] = s.LatencyMS
		h.probeErr[s.ID] = s.Error
	}
}

func (h *Helper) applyProbe(sv []ipc.Server) {
	h.probeMu.Lock()
	defer h.probeMu.Unlock()
	if len(h.probeMS) == 0 {
		return
	}
	for i := range sv {
		if sv[i].LatencyMS == 0 {
			sv[i].LatencyMS = h.probeMS[sv[i].ID]
		}
		if sv[i].Error == "" {
			sv[i].Error = h.probeErr[sv[i].ID]
		}
	}
}

func (h *Helper) Settings() ipc.Settings {
	h.mu.Lock()
	defer h.mu.Unlock()
	c, err := h.load()
	if err != nil {
		return ipc.Settings{}
	}
	preset := c.Tun.DNSPreset
	if preset == "" {
		preset = "cloudflare"
	}
	return ipc.Settings{
		FullTunnel: c.Tun.TunnelOn(), KillSwitch: c.Tun.KillSwitch,
		DNS: c.Tun.DNS, DNSPreset: preset, DoH: c.Tun.DoH, SystemDNS: readSystemDNS(),
		Pin: c.Pin, UseRelays: c.UseRelays, Carriers: selectableCarriers(c.Carriers),
		DirectDomains: nonNil(c.Routing.DirectDomains), DirectCIDRs: nonNil(c.Routing.DirectCIDRs),
		CrashReporting: c.CrashReporting,
	}
}

// SetSettings saves s; a running session keeps its old settings until the
// next Connect.
func (h *Helper) SetSettings(s ipc.Settings) error {
	h.mu.Lock()
	c, err := h.load()
	if err != nil {
		h.mu.Unlock()
		return err
	}
	c.Tun.SetEnabled(s.FullTunnel || s.KillSwitch) // a kill switch needs the tunnel
	c.Tun.KillSwitch = s.KillSwitch
	c.Tun.DoH = s.DoH
	if s.DNSPreset != "" {
		c.Tun.DNSPreset = s.DNSPreset
		c.Tun.DNS = dnsForPreset(s.DNSPreset, s.DNS) // empty for system; resolved at Connect
	} else if s.DNS != "" {
		c.Tun.DNS = s.DNS
		c.Tun.DNSPreset = "custom"
	}
	c.Pin = s.Pin
	c.UseRelays = s.UseRelays
	c.Carriers = selectableCarriers(s.Carriers)
	c.Routing.DirectDomains, c.Routing.DirectCIDRs = s.DirectDomains, s.DirectCIDRs
	c.CrashReporting = s.CrashReporting
	err = config.Save(h.path, c)
	h.mu.Unlock()
	if err != nil {
		return err
	}
	if h.crash != nil {
		_ = h.crash.SetEnabled(s.CrashReporting)
	}
	return nil
}

// ReportDiagnostics submits a scrubbed L0 snapshot via control or browser.
func (h *Helper) ReportDiagnostics() (ipc.ReportResult, error) {
	base, key, _, err := h.controlCreds()
	if err != nil {
		// Still allow browser fallback without control.
		base, key = "", ""
	}
	st := h.sess.Snapshot()
	h.ring.SetState(string(st.State))
	b := h.ring.Snapshot()
	sub := &diag.ClientSubmit{
		ControlURL: base,
		AccountKey: key,
		OpenURL: func(u string) error {
			// Best-effort: GUI also shows the URL; helper may not open a browser.
			h.log.Info("ready", "report", "browser")
			_ = u
			return nil
		},
	}
	res, err := sub.Submit(context.Background(), b)
	if err != nil {
		return ipc.ReportResult{}, err
	}
	return ipc.ReportResult{IssueURL: res.IssueURL, Via: res.Via}, nil
}

// dnsForPreset maps a UI preset to the stored DNS field. For "system" the
// stored value is empty; Connect resolves it via bypassDNS so a leftover
// tunnel resolver is never persisted as the bypass upstream.
func dnsForPreset(preset, custom string) string {
	switch preset {
	case "system":
		return ""
	case "cloudflare":
		return "1.1.1.1:53"
	case "google":
		return "8.8.8.8:53"
	case "quad9":
		return "9.9.9.9:53"
	case "custom":
		if custom != "" {
			return custom
		}
		return ""
	default:
		return ""
	}
}

// Unblock removes kill-switch rules. It refuses while a session runs, because
// that would open the leak the user asked to prevent.
func (h *Helper) Unblock() error {
	if h.sess.Snapshot().Running {
		return ipc.Coded(ipc.CodeBusy, errors.New("disconnect first"))
	}
	return h.unblock(h.log)
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

func selectableCarriers(s []string) []string {
	out := make([]string, 0, len(s))
	for _, kind := range s {
		if kind != "reverse" && kind != "dns" {
			out = append(out, kind)
		}
	}
	return out
}
