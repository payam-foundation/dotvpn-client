// Package app builds the client Engine from a Config. The CLI and the mobile
// binding share it, so both speak the same carriers and follow the same rules.
package app

import (
	"context"
	"crypto/ed25519"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/payam-foundation/dotvpn-client/client"
	"github.com/payam-foundation/dotvpn-client/internal/config"
	"github.com/payam-foundation/dotvpn-wire/auth"
	"github.com/payam-foundation/dotvpn-wire/discovery"
	"github.com/payam-foundation/dotvpn-wire/transport"
	"github.com/payam-foundation/dotvpn-wire/transport/cdnws"
	"github.com/payam-foundation/dotvpn-wire/transport/h2connect"
	"github.com/payam-foundation/dotvpn-wire/transport/h3connect"
	"github.com/payam-foundation/dotvpn-wire/transport/reverse"
	"github.com/payam-foundation/dotvpn-wire/transport/snowflake"
	"github.com/payam-foundation/dotvpn-wire/transport/webtunnel"
)

// Params is what NewEngine needs besides the config.
type Params struct {
	Config *config.Config
	// Dir holds state.json and the server list cache (must be writable).
	Dir string
	// Opts is the transport policy (Control, Resolver, DNS for bypass sockets).
	Opts transport.Options
	// HTTP is the control-plane client; nil means the default.
	HTTP *http.Client
	Log  *slog.Logger
}

// NewEngine builds the engine. With an account and control channels it also
// starts the token manager (and fails if the first refresh does).
func NewEngine(ctx context.Context, p Params) (*client.Engine, error) {
	c := p.Config
	p.Opts.Fragment, p.Opts.Upstream = c.Fragment, c.UpstreamProxy
	ecfg := client.EngineConfig{Endpoints: c.Endpoints, Dialers: Dialers(p.Opts), Log: p.Log}
	if c.UpstreamProxy != "" { // UDP carriers cannot ride a TCP proxy
		delete(ecfg.Dialers, transport.KindH3)
		delete(ecfg.Dialers, reverse.KindDNS)
	}
	switch c.ProbeTarget {
	case "":
		ecfg.ProbeTarget = client.DefaultProbeTarget
	case "off":
	default:
		ecfg.ProbeTarget = c.ProbeTarget
	}
	switch c.StateFile {
	case "":
		ecfg.Store = client.FileStore{Path: filepath.Join(p.Dir, "state.json")}
	case "off":
	default:
		ecfg.Store = client.FileStore{Path: c.StateFile}
	}
	switch {
	case c.Account != "" && len(c.Control) > 0:
		pubs, err := trustedKeys(c)
		if err != nil {
			return nil, err
		}
		tm := &client.TokenManager{Log: p.Log, Source: &discovery.Client{
			Channels: c.Control, Verifier: auth.NewVerifier(pubs...), AccountKey: c.Account,
			CachePath: filepath.Join(p.Dir, "serverlist.cache"), Bootstrap: c.Bootstrap, HTTP: p.HTTP,
		}}
		ecfg.Token, ecfg.OnUnauthorized = tm.Token, tm.Refresh
		eng := client.NewEngine(ecfg)
		tm.OnEndpoints = func(r discovery.Result) { eng.SetEndpoints(r.Endpoints) }
		if err := tm.Start(ctx); err != nil {
			_ = eng.Close()
			return nil, fmt.Errorf("control: %w", err)
		}
		return eng, nil
	case len(c.Endpoints) == 0:
		return nil, errors.New("no endpoints: pass -invite or configure endpoints")
	default:
		tok, err := staticToken(c)
		if err != nil {
			return nil, err
		}
		ecfg.Token = func() string { return tok }
		return client.NewEngine(ecfg), nil
	}
}

// Dialers returns every carrier this build speaks. The ladder order comes
// from the endpoint list, not from here.
func Dialers(o transport.Options) map[transport.Kind]transport.Dialer {
	return map[transport.Kind]transport.Dialer{
		transport.KindH2:        h2connect.Dialer{Opts: o},
		transport.KindH3:        h3connect.Dialer{Opts: o},
		transport.KindCDNWS:     cdnws.Dialer{Opts: o},
		transport.KindWebTunnel: webtunnel.Dialer{Opts: o},
		transport.KindSnowflake: snowflake.Dialer{Opts: o},
		reverse.KindReverse:     reverse.Dialer{Opts: o},
		reverse.KindDNS:         reverse.Dialer{Opts: o},
	}
}

func staticToken(c *config.Config) (string, error) {
	if c.Token != "" {
		return c.Token, nil
	}
	if c.TokenFile != "" {
		b, err := os.ReadFile(c.TokenFile)
		if err != nil {
			return "", err
		}
		return strings.TrimSpace(string(b)), nil
	}
	return "", errors.New("no token: set token or token_file")
}

func trustedKeys(c *config.Config) ([]ed25519.PublicKey, error) {
	var out []ed25519.PublicKey
	for _, k := range c.TrustedKey {
		p, err := auth.DecodePublicKey(k)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	if len(out) == 0 {
		return nil, errors.New("trusted_keys empty")
	}
	return out, nil
}
