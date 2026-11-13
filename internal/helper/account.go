package helper

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/payam-foundation/dotvpn-client/internal/config"
	"github.com/payam-foundation/dotvpn-client/internal/ipc"
	"github.com/payam-foundation/dotvpn-client/internal/product"
)

// Account fetches the safe subscription view from the control plane.
func (h *Helper) Account() (ipc.Account, error) {
	base, key, deviceID, err := h.controlCreds()
	if err != nil {
		return ipc.Account{}, err
	}
	req, err := http.NewRequest(http.MethodGet, strings.TrimSuffix(base, "/")+"/v1/account", nil)
	if err != nil {
		return ipc.Account{}, err
	}
	req.Header.Set("Authorization", "Bearer "+key)
	if deviceID != "" {
		req.Header.Set("X-Device-Id", deviceID)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return ipc.Account{}, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode == http.StatusUnauthorized {
		// Deleted or revoked invite: wipe local material so the UI returns to
		// invite entry instead of trapping users on empty account dashes.
		h.log.Info("account rejected; clearing local invite", "status", resp.StatusCode)
		if err := h.Logout(); err != nil {
			h.log.Warn("logout after invalid account", "err", err)
		}
		return ipc.Account{}, ipc.Coded(ipc.CodeAuth, fmt.Errorf("account no longer valid; add a new invite"))
	}
	if resp.StatusCode == http.StatusNotFound {
		// Older control build (no GET /v1/account yet): keep Account empty
		// without failing the binding on every poll.
		h.log.Debug("account unavailable", "status", resp.StatusCode)
		return ipc.Account{}, nil
	}
	if resp.StatusCode != http.StatusOK {
		return ipc.Account{}, fmt.Errorf("account: control replied %d", resp.StatusCode)
	}
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
		return ipc.Account{}, err
	}
	return ipc.Account{
		Name: v.Name, Package: v.Package, QuotaLimit: v.QuotaLimit, QuotaUsed: v.QuotaUsed,
		QuotaRemaining: v.QuotaRemaining, QuotaReset: v.QuotaReset, Expires: v.Expires,
		MaxDevices: v.MaxDevices, DevicesUsed: v.DevicesUsed, Mbps: v.Mbps, Disabled: v.Disabled,
	}, nil
}

// Ads fetches slot ads from control.
func (h *Helper) Ads(slot, relayID string) ([]ipc.Ad, error) {
	base, key, _, err := h.controlCreds()
	if err != nil {
		return nil, err
	}
	u, err := url.Parse(strings.TrimSuffix(base, "/") + "/v1/ads")
	if err != nil {
		return nil, err
	}
	q := u.Query()
	if slot != "" {
		q.Set("slot", slot)
	}
	if relayID != "" {
		q.Set("relay_id", relayID)
	}
	u.RawQuery = q.Encode()
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
		return []ipc.Ad{}, nil
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ads: control replied %d", resp.StatusCode)
	}
	var rows []struct {
		ID       string `json:"id"`
		Issuer   string `json:"issuer"`
		RelayID  string `json:"relay_id"`
		Format   string `json:"format"`
		ImageURL string `json:"image_url"`
		IconURL  string `json:"icon_url"`
		Title    string `json:"title"`
		Desc     string `json:"desc"`
		Link     string `json:"link"`
	}
	if err := json.Unmarshal(raw, &rows); err != nil {
		return nil, err
	}
	out := make([]ipc.Ad, 0, len(rows))
	for _, r := range rows {
		out = append(out, ipc.Ad{
			ID: r.ID, Issuer: r.Issuer, RelayID: r.RelayID, Format: r.Format,
			ImageURL: r.ImageURL, IconURL: r.IconURL, Title: r.Title, Desc: r.Desc, Link: r.Link,
		})
	}
	return out, nil
}

// Relays lists curated public relays plus opted-in ids.
func (h *Helper) Relays() ([]ipc.Relay, error) {
	base, key, _, err := h.controlCreds()
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
		Invite  string `json:"invite"`
		Public  bool   `json:"public"`
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
			Invite: r.Invite, Public: r.Public, OptedIn: opted[r.ID],
		})
	}
	return out, nil
}

// probeControl returns true when GET /v1/account answers (any HTTP status means reachable).
func probeControl(base, key string) bool {
	req, err := http.NewRequest(http.MethodGet, strings.TrimSuffix(base, "/")+"/v1/account", nil)
	if err != nil {
		return false
	}
	req.Header.Set("Authorization", "Bearer "+key)
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	_ = resp.Body.Close()
	return true
}

// AddRelay opts into a community/private relay by id or dvr:// URI.
// A signed dvr:// blob merges dialable reverse/dns endpoints for cold start.
func (h *Helper) AddRelay(idOrURI string) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	c, err := h.load()
	if err != nil {
		return err
	}
	id, err := product.MergeRelayInvite(c, idOrURI)
	if err != nil || id == "" {
		return ipc.Coded(ipc.CodeBadInvite, fmt.Errorf("invalid relay id"))
	}
	for _, x := range c.Relays {
		if x == id {
			return config.Save(h.path, c)
		}
	}
	c.Relays = append(c.Relays, id)
	return config.Save(h.path, c)
}

// RemoveRelay drops an opted-in relay id.
func (h *Helper) RemoveRelay(id string) error {
	id = product.RelayID(id)
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

func (h *Helper) controlCreds() (base, key, deviceID string, err error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	c, err := h.load()
	if err != nil {
		return "", "", "", err
	}
	if c.Account == "" || len(c.Control) == 0 || c.Control[0].URL == "" {
		return "", "", "", ipc.Coded(ipc.CodeNotConfigured, fmt.Errorf("no control plane"))
	}
	return c.Control[0].URL, c.Account, c.DeviceID, nil
}
