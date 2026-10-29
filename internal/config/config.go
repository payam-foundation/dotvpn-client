// Package config is the client's YAML configuration.
package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/payam-foundation/dotvpn-wire/atomicfile"
	"github.com/payam-foundation/dotvpn-wire/discovery"
	"github.com/payam-foundation/dotvpn-wire/invite"
	"github.com/payam-foundation/dotvpn-wire/transport"
)

// Config is the client's YAML file.
type Config struct {
	// Listen is the local mixed SOCKS5 + HTTP proxy address. A non-loopback
	// address needs ListenAuth or ListenAuthFile.
	Listen string `yaml:"listen"`
	// ListenAuth is "user:pass" required from proxy clients.
	ListenAuth string `yaml:"listen_auth"`
	// ListenAuthFile holds "user:pass" (first line); use it to keep the secret
	// out of the main config.
	ListenAuthFile string               `yaml:"listen_auth_file"`
	Token          string               `yaml:"token"`
	TokenFile      string               `yaml:"token_file"`
	Account        string               `yaml:"account_key"`
	Endpoints      []transport.Endpoint `yaml:"endpoints"`
	// Control is how to reach the control plane, in discovery order.
	Control    []discovery.ControlChannel `yaml:"control"`
	TrustedKey []string                   `yaml:"trusted_keys"`
	// Bootstrap is a signed server list embedded at install time.
	Bootstrap string `yaml:"bootstrap"`
	Fragment  bool   `yaml:"fragment"`
	// UpstreamProxy ("socks5://host:port" or "http://host:port") carries every
	// TCP carrier; UDP carriers (h3, dns) are disabled while it is set.
	UpstreamProxy string `yaml:"upstream_proxy"`
	// ProbeTarget ("host:port", default connectivitycheck.gstatic.com:80) is
	// fetched through each new session to prove the tunnel carries data. "off"
	// disables the check.
	ProbeTarget string `yaml:"probe_target"`
	// StateFile remembers the last working endpoint (default state.json beside
	// the config). "off" disables persistence.
	StateFile string        `yaml:"state_file"`
	Tun       TunConfig     `yaml:"tun"`
	Routing   RoutingConfig `yaml:"routing"`
}

// TunConfig configures full-tunnel mode.
type TunConfig struct {
	Enabled bool   `yaml:"enabled"`
	Name    string `yaml:"name"`
	MTU     int    `yaml:"mtu"`
	// KillSwitch blocks all non-tunnel traffic if the tunnel drops.
	KillSwitch bool `yaml:"kill_switch"`
	// DNS is the upstream resolver ("ip:port", default 1.1.1.1:53) the client
	// itself uses for node and control hostnames, outside the tunnel.
	DNS string `yaml:"dns"`
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
