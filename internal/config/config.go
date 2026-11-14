// SPDX-License-Identifier: AGPL-3.0-only

// Package config is the client's YAML configuration.
package config

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/payam-foundation/dotvpn-wire/atomicfile"
	"github.com/payam-foundation/dotvpn-wire/discovery"
	"github.com/payam-foundation/dotvpn-wire/invite"
	"github.com/payam-foundation/dotvpn-wire/loopback"
	"github.com/payam-foundation/dotvpn-wire/transport"
)

// SchemaCurrent is the config migration watermark. Bump when changing defaults
// that must rewrite existing files once.
const SchemaCurrent = 4

// Config is the client's YAML file.
type Config struct {
	// Schema tracks which defaults migrations have been applied.
	Schema int `yaml:"schema,omitempty"`
	// Listen is the local mixed SOCKS5 + HTTP proxy address. A non-loopback
	// address needs ListenAuth or ListenAuthFile.
	Listen string `yaml:"listen"`
	// ListenAuth is "user:pass" required from proxy clients.
	ListenAuth string `yaml:"listen_auth"`
	// ListenAuthFile holds "user:pass" (first line); use it to keep the secret
	// out of the main config.
	ListenAuthFile string `yaml:"listen_auth_file"`
	Token          string `yaml:"token"`
	TokenFile      string `yaml:"token_file"`
	Account        string `yaml:"account_key"`
	// DeviceID is a stable per-install seat id sent to control on token/account.
	DeviceID  string               `yaml:"device_id,omitempty"`
	Endpoints []transport.Endpoint `yaml:"endpoints"`
	// Control is how to reach the control plane, in discovery order.
	Control    []discovery.ControlChannel `yaml:"control"`
	TrustedKey []string                   `yaml:"trusted_keys"`
	// Bootstrap is a signed server list embedded at install time.
	Bootstrap string `yaml:"bootstrap"`
	// Mirrors are HTTPS fallbacks serving signed server lists.
	Mirrors  []string `yaml:"mirrors,omitempty"`
	Fragment bool     `yaml:"fragment"`
	// UpstreamProxy ("socks5://host:port" or "http://host:port") carries every
	// TCP carrier; UDP carriers (h3, dns) are disabled while it is set.
	UpstreamProxy string `yaml:"upstream_proxy"`
	// ProbeTarget ("host:port", default connectivitycheck.gstatic.com:80) is
	// fetched through each new session to prove the tunnel carries data. "off"
	// disables the check.
	ProbeTarget string `yaml:"probe_target"`
	// StateFile remembers the last working endpoint (default state.json beside
	// the config). "off" disables persistence.
	StateFile string `yaml:"state_file"`
	// Pin is a client.Health.ID: connect through that endpoint only.
	Pin string `yaml:"pin,omitempty"`
	// Relays are opted-in private/community relay ids sent on token refresh.
	Relays []string `yaml:"relays,omitempty"`
	// UseRelays enables reverse and DNS relay transports.
	UseRelays bool `yaml:"use_relays,omitempty"`
	// Carriers lists enabled transport kinds; empty means Auto (all).
	Carriers []string      `yaml:"carriers,omitempty"`
	Tun      TunConfig     `yaml:"tun"`
	Routing  RoutingConfig `yaml:"routing"`
	// CrashReporting enables opt-in Sentry crash reports (off by default).
	CrashReporting bool `yaml:"crash_reporting,omitempty"`
}

// TunConfig configures full-tunnel mode.
type TunConfig struct {
	// Enabled is full-tunnel mode. Omitted defaults to true (product default);
	// false is local proxy only. KillSwitch forces the tunnel on.
	Enabled *bool  `yaml:"enabled,omitempty"`
	Name    string `yaml:"name"`
	MTU     int    `yaml:"mtu"`
	// KillSwitch blocks all non-tunnel traffic if the tunnel drops.
	KillSwitch bool `yaml:"kill_switch"`
	// DNS is the upstream resolver ("ip:port", default 1.1.1.1:53) the client
	// itself uses for node and control hostnames, outside the tunnel.
	DNS string `yaml:"dns"`
	// DNSPreset is system|cloudflare|google|quad9|custom.
	DNSPreset string `yaml:"dns_preset,omitempty"`
	// DoH uses DNS-over-HTTPS for bypass/ECH lookups when supported.
	DoH bool `yaml:"doh,omitempty"`
}

// TunnelOn is whether Connect should bring up the system tunnel.
func (t TunConfig) TunnelOn() bool {
	if t.KillSwitch {
		return true
	}
	if t.Enabled == nil {
		return true
	}
	return *t.Enabled
}

// Migrate rewrites legacy defaults in place. Returns whether the caller should
// persist the config.
func (c *Config) Migrate() bool {
	changed := false
	if c.Schema < SchemaCurrent {
		orig := c.Schema
		// Schema 0→1: older builds wrote enabled:false (Go zero / buggy Settings)
		// even with dns_preset set, which left Connect in proxy-only mode and the
		// UI showing the real public IP. Product default is full tunnel; users who
		// want SOCKS-only can turn "Route all traffic" off after this lands.
		if orig < 1 {
			if c.Tun.Enabled == nil || !*c.Tun.Enabled {
				c.Tun.SetEnabled(true)
			}
			if c.Tun.DNSPreset == "" {
				c.Tun.DNSPreset = "cloudflare"
			}
		}
		// Schema 1→2: dns_preset "system" was persisted as the literal DNS address
		// "system", which cannot be dialed ("missing port in address").
		if orig < 2 && (c.Tun.DNS == "system" || c.Tun.DNSPreset == "system") {
			c.Tun.DNSPreset = "system"
			c.Tun.DNS = ""
		}
		// Schema 2→3: "system" as the bypass resolver races with the tunnel's
		// fake DNS (198.18.0.2) and often becomes unreachable; use Cloudflare
		// for the client's own physical-path lookups. Users can still pick
		// system in Settings afterward.
		if orig < 3 && (c.Tun.DNSPreset == "system" || c.Tun.DNS == "" || c.Tun.DNS == "system" || isFakeTunDNS(c.Tun.DNS)) {
			c.Tun.DNSPreset = "cloudflare"
			c.Tun.DNS = "1.1.1.1:53"
		}
		c.Schema = SchemaCurrent
		changed = true
	}
	// Schema 3→4 (and any empty file): mint a stable seat id once.
	if c.DeviceID == "" {
		if id, err := newDeviceID(); err == nil {
			c.DeviceID = id
			changed = true
		}
	}
	return changed
}

func newDeviceID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return "dv_" + base64.RawURLEncoding.EncodeToString(b[:]), nil
}

// isFakeTunDNS reports the reserved fakedns/CGNAT range we point the OS at.
func isFakeTunDNS(addr string) bool {
	host := addr
	if h, _, err := net.SplitHostPort(addr); err == nil {
		host = h
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return host == "system"
	}
	_, cidr, err := net.ParseCIDR("198.18.0.0/15")
	return err == nil && cidr.Contains(ip)
}

// SetEnabled stores the full-tunnel preference.
func (t *TunConfig) SetEnabled(on bool) {
	t.Enabled = &on
}

// RoutingConfig lists destinations that bypass the tunnel.
type RoutingConfig struct {
	DirectDomains []string `yaml:"direct_domains"`
	DirectCIDRs   []string `yaml:"direct_cidrs"`
	GeoIP         string   `yaml:"geoip"`
	GeoSite       string   `yaml:"geosite"`
	DirectGeoIP   []string `yaml:"direct_geoip"`
	DirectGeoSite []string `yaml:"direct_geosite"`
}

// Defaults fills unset fields without overwriting user values.
func (c *Config) Defaults() {
	if c.Listen == "" {
		c.Listen = "127.0.0.1:1080"
	}
	if c.Tun.MTU == 0 {
		c.Tun.MTU = 1380
	}
	if c.Tun.DNS == "" {
		c.Tun.DNS = "1.1.1.1:53"
	}
	if c.Tun.Name == "" {
		c.Tun.Name = "dotvpn0"
	}
}

// ProxyAuth returns the proxy credentials, or ok=false when auth is not
// configured.
func (c *Config) ProxyAuth() (user, pass string, ok bool, err error) {
	raw := c.ListenAuth
	if raw == "" && c.ListenAuthFile != "" {
		b, err := os.ReadFile(c.ListenAuthFile)
		if err != nil {
			return "", "", false, err
		}
		raw, _, _ = strings.Cut(strings.TrimSpace(string(b)), "\n")
	}
	if raw == "" {
		return "", "", false, nil
	}
	user, pass, found := strings.Cut(raw, ":")
	if !found || user == "" || pass == "" {
		return "", "", false, errors.New("listen_auth must be user:pass with both parts non-empty")
	}
	return user, pass, true, nil
}

// Apply merges an invite into the config.
func (c *Config) Apply(i invite.Invite) {
	if len(i.Endpoints) > 0 {
		c.Endpoints = i.Endpoints
	}
	if len(i.Control) > 0 {
		c.Control = i.Control
	}
	c.TrustedKey = i.TrustedKey
	if i.Account != "" {
		c.Account = i.Account
	}
	if i.Token != "" {
		c.Token = i.Token
	}
	if i.Bootstrap != "" {
		c.Bootstrap = i.Bootstrap
	}
	if len(i.Mirrors) > 0 {
		c.Mirrors = append([]string(nil), i.Mirrors...)
	}
	// Joining a network turns the system tunnel on. Proxy-only stays a
	// deliberate Settings choice after schema migration.
	c.Tun.SetEnabled(true)
}

// Load reads path and applies defaults; unknown keys are an error.
func Load(path string) (*Config, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	c, err := Parse(b)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return c, nil
}

// Parse decodes YAML (or JSON, its subset) and applies defaults; unknown keys
// are an error.
func Parse(b []byte) (*Config, error) {
	var c Config
	dec := yaml.NewDecoder(bytes.NewReader(b))
	dec.KnownFields(true)
	if err := dec.Decode(&c); err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	c.Defaults()
	return &c, nil
}

// Marshal renders the config as YAML, the form Parse reads.
func Marshal(c *Config) ([]byte, error) { return yaml.Marshal(c) }

// Save writes the config with mode 0600 (it holds the account key), atomically.
func Save(path string, c *Config) error {
	b, err := Marshal(c)
	if err != nil {
		return err
	}
	return atomicfile.Write(path, b, 0o700)
}

// CheckListen refuses a proxy address reachable off-host unless proxy
// authentication is configured. A unix socket is never valid (TCP listener).
func (c *Config) CheckListen() error {
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
