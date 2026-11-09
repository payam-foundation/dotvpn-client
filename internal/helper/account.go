package helper

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/payam-foundation/dotvpn-client/internal/config"
	"github.com/payam-foundation/dotvpn-client/internal/ipc"
)

// Account fetches the safe subscription view from the control plane.
func (h *Helper) Account() (ipc.Account, error) {
	base, key, err := h.controlCreds()
	if err != nil {
		return ipc.Account{}, err
	}
	req, err := http.NewRequest(http.MethodGet, strings.TrimSuffix(base, "/")+"/v1/account", nil)
	if err != nil {
		return ipc.Account{}, err
	}
	req.Header.Set("Authorization", "Bearer "+key)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return ipc.Account{}, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusNotFound {
		// Stale invite or older control build (no GET /v1/account yet): keep
		// Account empty without failing the binding on every poll.
		h.log.Debug("account unavailable", "status", resp.StatusCode)
		return ipc.Account{}, nil
	}
	if resp.StatusCode != http.StatusOK {
		return ipc.Account{}, fmt.Errorf("account: control replied %d", resp.StatusCode)
	}
	var v struct {
		Name           string `json:"name"`
		QuotaLimit     int64  `json:"quota_limit"`
		QuotaRemaining int64  `json:"quota_remaining"`
		QuotaReset     string `json:"quota_reset"`
		Expires        int64  `json:"expires"`
		MaxDevices     int    `json:"max_devices"`
		Mbps           int    `json:"mbps"`
		Disabled       bool   `json:"disabled"`
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		return ipc.Account{}, err
	}
	used := 1 // this install counts as one seat until control reports live use
	if v.MaxDevices > 0 && used > v.MaxDevices {
		used = v.MaxDevices
	}
	return ipc.Account{
		Name: v.Name, QuotaLimit: v.QuotaLimit, QuotaRemaining: v.QuotaRemaining,
		QuotaReset: v.QuotaReset, Expires: v.Expires, MaxDevices: v.MaxDevices,
		DevicesUsed: used, Mbps: v.Mbps, Disabled: v.Disabled,
	}, nil
}

// Relays lists curated public relays plus opted-in ids.
func (h *Helper) Relays() ([]ipc.Relay, error) {
	base, key, err := h.controlCreds()
	if err != nil {
		return nil, err
	}
	h.mu.Lock()
	c, err := h.load()
	h.mu.Unlock()
	if err != nil {
		return nil, err
	}
	u, err := url.Parse(strings.TrimSuffix(base, "/") + "/v1/relays")
	if err != nil {
		return nil, err
	}
	if len(c.Relays) > 0 {
		q := u.Query()
		q.Set("ids", strings.Join(c.Relays, ","))
		u.RawQuery = q.Encode()
	}
	req, err := http.NewRequest(http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+key)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusNotFound {
		return []ipc.Relay{}, nil
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("relays: control replied %d", resp.StatusCode)
	}
	var rows []struct {
		ID      string `json:"id"`
		Name    string `json:"name"`
		Country string `json:"country"`
		Tier    string `json:"tier"`
		Health  string `json:"health"`
	}
	if err := json.Unmarshal(raw, &rows); err != nil {
		return nil, err
	}
	opted := map[string]bool{}
	for _, id := range c.Relays {
		opted[id] = true
	}
	out := make([]ipc.Relay, 0, len(rows))
	for _, r := range rows {
		out = append(out, ipc.Relay{
			ID: r.ID, Name: r.Name, Country: r.Country, Tier: r.Tier, Health: r.Health,
			OptedIn: opted[r.ID],
		})
	}
	return out, nil
}

// AddRelay opts into a community/private relay by id or dotvpn-relay:// URI.
func (h *Helper) AddRelay(idOrURI string) error {
	id := parseRelayID(idOrURI)
	if id == "" {
		return ipc.Coded(ipc.CodeBadInvite, fmt.Errorf("invalid relay id"))
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	c, err := h.load()
	if err != nil {
		return err
	}
	for _, x := range c.Relays {
		if x == id {
			return nil
		}
	}
	c.Relays = append(c.Relays, id)
	return config.Save(h.path, c)
}

// RemoveRelay drops an opted-in relay id.
func (h *Helper) RemoveRelay(id string) error {
	id = parseRelayID(id)
	h.mu.Lock()
	defer h.mu.Unlock()
	c, err := h.load()
	if err != nil {
		return err
	}
	out := c.Relays[:0]
	for _, x := range c.Relays {
		if x != id {
			out = append(out, x)
		}
	}
	c.Relays = out
	return config.Save(h.path, c)
}

func parseRelayID(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "dotvpn-relay://") {
		s = strings.TrimPrefix(s, "dotvpn-relay://")
		if i := strings.IndexAny(s, "/?#"); i >= 0 {
			s = s[:i]
		}
	}
	return strings.TrimSpace(s)
}

func (h *Helper) controlCreds() (base, key string, err error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	c, err := h.load()
	if err != nil {
		return "", "", err
	}
	if c.Account == "" || len(c.Control) == 0 || c.Control[0].URL == "" {
		return "", "", ipc.Coded(ipc.CodeNotConfigured, fmt.Errorf("no control plane"))
	}
	return c.Control[0].URL, c.Account, nil
}
