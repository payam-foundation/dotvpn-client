package mobile

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/payam-foundation/dotvpn-client/internal/config"
	"github.com/payam-foundation/dotvpn-client/internal/product"
)

// selectableCarriers drops reverse/dns: those are gated by UseRelays, not chips.
func selectableCarriers(s []string) []string {
	out := make([]string, 0, len(s))
	for _, kind := range s {
		if kind != "reverse" && kind != "dns" {
			out = append(out, kind)
		}
	}
	return out
}

// Settings returns the Android settings JSON (useRelays, carriers, relays,
// pin, dns, dnsPreset, doh, directDomains, directCidrs).
func Settings(cfgYAML string) (string, error) {
	c, err := config.Parse([]byte(cfgYAML))
	if err != nil {
		return "", err
	}
	preset := c.Tun.DNSPreset
	if preset == "" {
		preset = "cloudflare"
	}
	b, err := json.Marshal(map[string]any{
		"useRelays":     c.UseRelays,
		"carriers":      selectableCarriers(c.Carriers),
		"relays":        nonNil(c.Relays),
		"pin":           c.Pin,
		"dns":           c.Tun.DNS,
		"dnsPreset":     preset,
		"doh":           c.Tun.DoH,
		"directDomains": nonNil(c.Routing.DirectDomains),
		"directCidrs":   nonNil(c.Routing.DirectCIDRs),
	})
	return string(b), err
}

// ApplySettings merges a JSON patch into cfgYAML and returns the updated YAML.
// reverse/dns carriers are stripped. Known keys: useRelays, carriers, pin,
// dnsPreset, dns, doh, directDomains, directCidrs.
func ApplySettings(cfgYAML, patchJSON string) (string, error) {
	c, err := config.Parse([]byte(cfgYAML))
	if err != nil {
		return "", err
	}
	var p struct {
		UseRelays     *bool    `json:"useRelays"`
		Carriers      []string `json:"carriers"`
		Pin           *string  `json:"pin"`
		DNSPreset     *string  `json:"dnsPreset"`
		DNS           *string  `json:"dns"`
		DoH           *bool    `json:"doh"`
		DirectDomains []string `json:"directDomains"`
		DirectCIDRs   []string `json:"directCidrs"`
	}
	if err := json.Unmarshal([]byte(patchJSON), &p); err != nil {
		return "", err
	}
	if p.UseRelays != nil {
		c.UseRelays = *p.UseRelays
	}
	if p.Carriers != nil {
		c.Carriers = selectableCarriers(p.Carriers)
	}
	if p.Pin != nil {
		c.Pin = *p.Pin
	}
	if p.DoH != nil {
		c.Tun.DoH = *p.DoH
	}
	if p.DNSPreset != nil {
		c.Tun.DNSPreset = *p.DNSPreset
		c.Tun.DNS = dnsForPreset(*p.DNSPreset, ptrStr(p.DNS))
	} else if p.DNS != nil {
		c.Tun.DNS = *p.DNS
		c.Tun.DNSPreset = "custom"
	}
	if p.DirectDomains != nil {
		c.Routing.DirectDomains = p.DirectDomains
	}
	if p.DirectCIDRs != nil {
		c.Routing.DirectCIDRs = p.DirectCIDRs
	}
	b, err := config.Marshal(c)
	return string(b), err
}

// dnsForPreset maps a UI preset to the stored DNS field. "system" is empty
// (Android falls back to 1.1.1.1 in NewProtectedBypass).
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
		return custom
	default:
		return ""
	}
}

func ptrStr(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

// Account fetches GET /v1/account and returns the safe subscription JSON.
// 401 clears invite material and returns ("", err) with a message the UI can
// treat as auth; 404 returns "{}" (older control).
func Account(cfgYAML string) (string, error) {
	c, err := config.Parse([]byte(cfgYAML))
	if err != nil {
		return "", err
	}
	base, key, err := controlCreds(c)
	if err != nil {
		return "{}", nil
	}
	raw, code, err := controlGET(strings.TrimSuffix(base, "/")+"/v1/account", key, c.DeviceID)
	if err != nil {
		return "", err
	}
	switch code {
	case http.StatusUnauthorized:
		// Caller should Logout and wipe Store; returning auth matches helper.
		return "", errors.New("auth")
	case http.StatusNotFound:
		return "{}", nil
	case http.StatusOK:
		var v struct {
			Name           string `json:"name"`
			Package        string `json:"package"`
			QuotaLimit     int64  `json:"quota_limit"`
			QuotaUsed      int64  `json:"quota_used"`
			QuotaRemaining int64  `json:"quota_remaining"`
			QuotaReset     string `json:"quota_reset"`
			Expires        int64  `json:"expires"`
			MaxDevices     int    `json:"max_devices"`
			DevicesUsed    int    `json:"devices_used"`
			Mbps           int    `json:"mbps"`
			Disabled       bool   `json:"disabled"`
		}
		if err := json.Unmarshal(raw, &v); err != nil {
			return "", err
		}
		b, err := json.Marshal(map[string]any{
			"name": v.Name, "package": v.Package,
			"quotaLimit": v.QuotaLimit, "quotaUsed": v.QuotaUsed, "quotaRemaining": v.QuotaRemaining,
			"quotaReset": v.QuotaReset, "expires": v.Expires,
			"maxDevices": v.MaxDevices, "devicesUsed": v.DevicesUsed,
			"mbps": v.Mbps, "disabled": v.Disabled,
		})
		return string(b), err
	default:
		return "", fmt.Errorf("account: control replied %d", code)
	}
}

// Logout wipes invite/account material from cfgYAML. Tun/routing/carrier
// preferences stay so the next invite inherits them.
func Logout(cfgYAML string) (string, error) {
	c, err := config.Parse([]byte(cfgYAML))
	if err != nil {
		return "", err
	}
	c.Account = ""
	c.Token = ""
	c.Bootstrap = ""
	c.TrustedKey = nil
	c.Control = nil
	c.Endpoints = nil
	c.Relays = nil
	c.Pin = ""
	b, err := config.Marshal(c)
	return string(b), err
}

// AddRelay opts into a community/private relay by id or dvr:// URI.
func AddRelay(cfgYAML, idOrURI string) (string, error) {
	c, err := config.Parse([]byte(cfgYAML))
	if err != nil {
		return "", err
	}
	id, err := product.MergeRelayInvite(c, idOrURI)
	if err != nil || id == "" {
		return "", errors.New("invalid relay id")
	}
	for _, x := range c.Relays {
		if x == id {
			b, err := config.Marshal(c)
			return string(b), err
		}
	}
	c.Relays = append(c.Relays, id)
	b, err := config.Marshal(c)
	return string(b), err
}

// RemoveRelay drops an opted-in relay id from cfgYAML.
func RemoveRelay(cfgYAML, id string) (string, error) {
	id = product.RelayID(id)
	c, err := config.Parse([]byte(cfgYAML))
	if err != nil {
		return "", err
	}
	out := c.Relays[:0]
	for _, x := range c.Relays {
		if x != id {
			out = append(out, x)
		}
	}
	c.Relays = out
	b, err := config.Marshal(c)
	return string(b), err
}

// Configured reports whether cfgYAML still holds an invite (account, control
// or endpoints), matching the helper. False after ValidateAccount logs out.
func Configured(cfgYAML string) bool {
	c, err := config.Parse([]byte(cfgYAML))
	return err == nil && (c.Account != "" || len(c.Control) > 0 || len(c.Endpoints) > 0)
}

// NeedsRelay reports whether the control plane in cfgYAML cannot be reached
// from this network (any HTTP answer counts as reachable), so the UI can offer
// a bootstrap relay after an invite import. False when there is no control.
func NeedsRelay(cfgYAML string) bool {
	c, err := config.Parse([]byte(cfgYAML))
	if err != nil {
		return false
	}
	base, key, err := controlCreds(c)
	if err != nil {
		return false
	}
	req, err := http.NewRequest(http.MethodGet, strings.TrimSuffix(base, "/")+"/v1/account", nil)
	if err != nil {
		return false
	}
	req.Header.Set("Authorization", "Bearer "+key)
	resp, err := (&http.Client{Timeout: 5 * time.Second}).Do(req)
	if err != nil {
		return true
	}
	_ = resp.Body.Close()
	return false
}

// ValidateAccount asks control whether the account in cfgYAML is still valid
// and returns the config to keep: cfgYAML unchanged, or with invite material
// wiped when control answers 401 (deleted or revoked), like the helper.
func ValidateAccount(cfgYAML string) (string, error) {
	c, err := config.Parse([]byte(cfgYAML))
	if err != nil {
		return "", err
	}
	base, key, err := controlCreds(c)
	if err != nil {
		return cfgYAML, nil
	}
	_, code, err := controlGET(strings.TrimSuffix(base, "/")+"/v1/account", key, c.DeviceID)
	if err != nil {
		return "", err
	}
	switch code {
	case http.StatusUnauthorized:
		c.Account = ""
		c.Token = ""
		c.Bootstrap = ""
		c.TrustedKey = nil
		c.Control = nil
		c.Endpoints = nil
		c.Relays = nil
		c.Pin = ""
		b, err := config.Marshal(c)
		return string(b), err
	case http.StatusOK, http.StatusNotFound:
		return cfgYAML, nil
	}
	return "", fmt.Errorf("account: control replied %d", code)
}

// Relays fetches GET /v1/relays (with opted-in ids) and returns JSON rows
// including public and optedIn, matching the desktop Relay shape.
func Relays(cfgYAML string) (string, error) {
	c, err := config.Parse([]byte(cfgYAML))
	if err != nil {
		return "", err
	}
	base, key, err := controlCreds(c)
	if err != nil {
		return "[]", nil
	}
	u, err := url.Parse(strings.TrimSuffix(base, "/") + "/v1/relays")
	if err != nil {
		return "", err
	}
	if len(c.Relays) > 0 {
		q := u.Query()
		q.Set("ids", strings.Join(c.Relays, ","))
		u.RawQuery = q.Encode()
	}
	raw, code, err := controlGET(u.String(), key, "")
	if err != nil {
		return "", err
	}
	if code == http.StatusUnauthorized || code == http.StatusNotFound {
		return "[]", nil
	}
	if code != http.StatusOK {
		return "", fmt.Errorf("relays: control replied %d", code)
	}
	var rows []struct {
		ID      string `json:"id"`
		Name    string `json:"name"`
		Country string `json:"country"`
		Tier    string `json:"tier"`
		Health  string `json:"health"`
		Invite  string `json:"invite"`
		Public  bool   `json:"public"`
	}
	if err := json.Unmarshal(raw, &rows); err != nil {
		return "", err
	}
	opted := map[string]bool{}
	for _, id := range c.Relays {
		opted[id] = true
	}
	type outRow struct {
		ID      string `json:"id"`
		Name    string `json:"name"`
		Country string `json:"country,omitempty"`
		Tier    string `json:"tier"`
		Health  string `json:"health"`
		Invite  string `json:"invite,omitempty"`
		Public  bool   `json:"public"`
		OptedIn bool   `json:"optedIn"`
	}
	out := make([]outRow, 0, len(rows))
	for _, r := range rows {
		out = append(out, outRow{
			ID: r.ID, Name: r.Name, Country: r.Country, Tier: r.Tier,
			Health: r.Health, Invite: r.Invite, Public: r.Public, OptedIn: opted[r.ID],
		})
	}
	b, err := json.Marshal(out)
	return string(b), err
}

// Ads fetches GET /v1/ads?slot=&relay_id= and returns the JSON array.
func Ads(cfgYAML, slot, relayID string) (string, error) {
	c, err := config.Parse([]byte(cfgYAML))
	if err != nil {
		return "", err
	}
	base, key, err := controlCreds(c)
	if err != nil {
		return "[]", nil
	}
	u, err := url.Parse(strings.TrimSuffix(base, "/") + "/v1/ads")
	if err != nil {
		return "", err
	}
	q := u.Query()
	if slot != "" {
		q.Set("slot", slot)
	}
	if relayID != "" {
		q.Set("relay_id", relayID)
	}
	u.RawQuery = q.Encode()
	raw, code, err := controlGET(u.String(), key, "")
	if err != nil {
		return "", err
	}
	if code == http.StatusUnauthorized || code == http.StatusNotFound {
		return "[]", nil
	}
	if code != http.StatusOK {
		return "", fmt.Errorf("ads: control replied %d", code)
	}
	// Older control builds encoded an empty list as JSON null.
	if len(bytes.TrimSpace(raw)) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return "[]", nil
	}
	return string(raw), nil
}

func controlCreds(c *config.Config) (base, key string, err error) {
	if c.Account == "" || len(c.Control) == 0 || c.Control[0].URL == "" {
		return "", "", errors.New("not configured")
	}
	return c.Control[0].URL, c.Account, nil
}

func controlGET(rawURL, key, deviceID string) ([]byte, int, error) {
	req, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Authorization", "Bearer "+key)
	if deviceID != "" {
		req.Header.Set("X-Device-Id", deviceID)
	}
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	return raw, resp.StatusCode, err
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}
