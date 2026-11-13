// SPDX-License-Identifier: AGPL-3.0-only

// Package app builds the client Engine from a Config. The CLI and the mobile
// binding share it, so both speak the same carriers and follow the same rules.
package app

import (
	"context"
	"crypto/ed25519"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

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
	// OnState is passed to the engine (see client.EngineConfig.OnState).
	OnState func(client.Info)
	Log     *slog.Logger
}

// NewEngine builds the engine. With an account and control channels it also
// starts the token manager (and fails if the first refresh does).
func NewEngine(ctx context.Context, p Params) (*client.Engine, error) {
	c := p.Config
	p.Opts.Fragment, p.Opts.Upstream = c.Fragment, c.UpstreamProxy
	ecfg := client.EngineConfig{
		Endpoints: eligibleEndpoints(c.Endpoints, c.UseRelays),
		Dialers:   Dialers(p.Opts, c.UseRelays),
		OnState:   p.OnState, Pin: c.Pin, Log: p.Log,
	}
	if c.UpstreamProxy != "" { // UDP carriers cannot ride a TCP proxy
		delete(ecfg.Dialers, transport.KindH3)
		delete(ecfg.Dialers, reverse.KindDNS)
	}
	if len(c.Carriers) > 0 {
		allow := map[string]bool{}
		for _, k := range c.Carriers {
			allow[k] = true
		}
		for k := range ecfg.Dialers {
			if c.UseRelays && (k == reverse.KindReverse || k == reverse.KindDNS) {
				continue
			}
			if !allow[string(k)] {
				delete(ecfg.Dialers, k)
			}
		}
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
		tm := &client.TokenManager{Log: p.Log}
		live := &liveEndpoints{eps: append([]transport.Endpoint(nil), c.Endpoints...)}
		source := &discovery.Client{
			Channels: c.Control, Verifier: auth.NewVerifier(pubs...), AccountKey: c.Account,
			DeviceID: c.DeviceID, Relays: c.Relays, CachePath: filepath.Join(p.Dir, "serverlist.cache"),
			Mirrors: c.Mirrors, Bootstrap: c.Bootstrap, HTTP: p.HTTP,
		}
		source.OnBootstrapEndpoints = func(eps []transport.Endpoint) {
			live.merge(eps)
		}
		if c.UseRelays {
			source.ChannelHTTP = relayChannelHTTP(live.get, p.Opts, func() string {
				if token := tm.Token(); token != "" {
					return token
				}
				return c.Account
			})
		}
		tm.Source = source
		ecfg.Token, ecfg.OnUnauthorized = tm.Token, tm.Refresh
		eng := client.NewEngine(ecfg)
		tm.OnEndpoints = func(r discovery.Result) {
			live.merge(r.Endpoints)
			eng.SetEndpoints(eligibleEndpoints(r.Endpoints, c.UseRelays))
		}
		if err := tm.Start(ctx); err != nil {
			_ = eng.Close()
			return nil, fmt.Errorf("control: %w", err)
		}
		// Secondary refresh after a successful path is available once VPN is up
		// (OnUnauthorized / timer already cover; kick once more shortly after start).
		go func() {
			select {
			case <-ctx.Done():
				return
			case <-time.After(15 * time.Second):
				tm.Refresh()
			}
		}()
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

// Dialers returns the enabled carrier implementations. The ladder order comes
// from the endpoint list, not from here.
func Dialers(o transport.Options, useRelays bool) map[transport.Kind]transport.Dialer {
	out := map[transport.Kind]transport.Dialer{
		transport.KindH2:        h2connect.Dialer{Opts: o},
		transport.KindH3:        h3connect.Dialer{Opts: o},
		transport.KindCDNWS:     cdnws.Dialer{Opts: o},
		transport.KindWebTunnel: webtunnel.Dialer{Opts: o},
		transport.KindSnowflake: snowflake.Dialer{Opts: o},
	}
	if useRelays {
		out[reverse.KindReverse] = reverse.Dialer{Opts: o}
		out[reverse.KindDNS] = reverse.Dialer{Opts: o}
	}
	return out
}

func eligibleEndpoints(endpoints []transport.Endpoint, useRelays bool) []transport.Endpoint {
	out := make([]transport.Endpoint, 0, len(endpoints))
	for _, ep := range endpoints {
		if !useRelays && (ep.Kind == reverse.KindReverse || ep.Kind == reverse.KindDNS) {
			continue
		}
		out = append(out, ep)
	}
	return out
}

type liveEndpoints struct {
	mu  sync.RWMutex
	eps []transport.Endpoint
}

func (l *liveEndpoints) get() []transport.Endpoint {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return append([]transport.Endpoint(nil), l.eps...)
}

func (l *liveEndpoints) merge(eps []transport.Endpoint) {
	if len(eps) == 0 {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	seen := map[string]bool{}
	for _, ep := range l.eps {
		seen[string(ep.Kind)+ep.Addr+ep.Name] = true
	}
	for _, ep := range eps {
		k := string(ep.Kind) + ep.Addr + ep.Name
		if seen[k] {
			continue
		}
		seen[k] = true
		l.eps = append(l.eps, ep)
	}
}

func relayChannelHTTP(endpoints func() []transport.Endpoint, opts transport.Options, token transport.TokenSource) func(discovery.ControlChannel) *http.Client {
	return func(ch discovery.ControlChannel) *http.Client {
		if ch.Relay == "" {
			return nil
		}
		var endpoint transport.Endpoint
		found := false
		for _, ep := range endpoints() {
			if (ep.Kind == reverse.KindReverse || ep.Kind == reverse.KindDNS) &&
				(ep.Addr == ch.Relay || ep.Name == ch.Relay) {
				endpoint, found = ep, true
				break
			}
		}
		if !found {
			return nil
		}
		dialer := reverse.Dialer{Opts: opts}
		tr := &http.Transport{}
		tr.DialContext = func(ctx context.Context, _, addr string) (net.Conn, error) {
			session, err := dialer.Dial(ctx, endpoint, token)
			if err != nil {
				return nil, err
			}
			conn, err := session.OpenStream(ctx, addr)
			if err != nil {
				_ = session.Close()
				return nil, err
			}
			return &sessionConn{Conn: conn, session: session}, nil
		}
		return &http.Client{Transport: tr, Timeout: 15 * time.Second}
	}
}

type sessionConn struct {
	net.Conn
	session transport.Session
}

func (c *sessionConn) Close() error {
	err := c.Conn.Close()
	if closeErr := c.session.Close(); err == nil {
		err = closeErr
	}
	return err
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
