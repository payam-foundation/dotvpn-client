// SPDX-License-Identifier: AGPL-3.0-only

package mobile

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/payam-foundation/dotvpn-client/client"
	"github.com/payam-foundation/dotvpn-client/internal/config"
	"github.com/payam-foundation/dotvpn-client/internal/product"
	"github.com/payam-foundation/dotvpn-wire/discovery"
	"github.com/payam-foundation/dotvpn-wire/invite"
	"github.com/payam-foundation/dotvpn-wire/transport"
	"github.com/payam-foundation/dotvpn-wire/transport/reverse"
)

func testInviteYAML(t *testing.T) string {
	t.Helper()
	y, err := ImportInvite(invite.Invite{
		Endpoints:  []transport.Endpoint{{Kind: transport.KindH2, Addr: "127.0.0.1:1", Host: "x.example"}},
		TrustedKey: []string{"k"}, Token: "tok",
	}.Encode())
	if err != nil {
		t.Fatal(err)
	}
	return y
}

func TestApplySettingsUseRelaysCarriers(t *testing.T) {
	y := testInviteYAML(t)
	y, err := ApplySettings(y, `{"useRelays":true,"carriers":["h3","reverse","cdnws","dns"]}`)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := Settings(y)
	if err != nil {
		t.Fatal(err)
	}
	var s struct {
		UseRelays bool     `json:"useRelays"`
		Carriers  []string `json:"carriers"`
	}
	if err := json.Unmarshal([]byte(raw), &s); err != nil {
		t.Fatal(err)
	}
	if !s.UseRelays || len(s.Carriers) != 2 || s.Carriers[0] != "h3" || s.Carriers[1] != "cdnws" {
		t.Fatalf("settings = %+v", s)
	}
	c, err := config.Parse([]byte(y))
	if err != nil {
		t.Fatal(err)
	}
	if !c.UseRelays {
		t.Fatal("use_relays not persisted")
	}
}

func TestApplySettingsPinDNSBypass(t *testing.T) {
	y := testInviteYAML(t)
	y, err := ApplySettings(y, `{"pin":"ep1","dnsPreset":"quad9","doh":true,"directDomains":["example.com"],"directCidrs":["10.0.0.0/8"]}`)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := Settings(y)
	if err != nil {
		t.Fatal(err)
	}
	var s struct {
		Pin           string   `json:"pin"`
		DNS           string   `json:"dns"`
		DNSPreset     string   `json:"dnsPreset"`
		DoH           bool     `json:"doh"`
		DirectDomains []string `json:"directDomains"`
		DirectCIDRs   []string `json:"directCidrs"`
	}
	if err := json.Unmarshal([]byte(raw), &s); err != nil {
		t.Fatal(err)
	}
	if s.Pin != "ep1" || s.DNS != "9.9.9.9:53" || s.DNSPreset != "quad9" || !s.DoH ||
		len(s.DirectDomains) != 1 || s.DirectDomains[0] != "example.com" ||
		len(s.DirectCIDRs) != 1 || s.DirectCIDRs[0] != "10.0.0.0/8" {
		t.Fatalf("settings = %+v", s)
	}
	y, err = ApplySettings(y, `{"pin":""}`)
	if err != nil {
		t.Fatal(err)
	}
	c, _ := config.Parse([]byte(y))
	if c.Pin != "" {
		t.Fatalf("pin clear failed: %q", c.Pin)
	}
}

func TestLogout(t *testing.T) {
	y := controlInviteYAML(t, "http://127.0.0.1:9")
	y, err := ApplySettings(y, `{"useRelays":true,"carriers":["h3"],"directDomains":["a.ir"]}`)
	if err != nil {
		t.Fatal(err)
	}
	got, err := Logout(y)
	if err != nil {
		t.Fatal(err)
	}
	if Configured(got) {
		t.Fatal("logout must clear invite material")
	}
	c, _ := config.Parse([]byte(got))
	if !c.UseRelays || len(c.Carriers) != 1 || c.Carriers[0] != "h3" ||
		len(c.Routing.DirectDomains) != 1 {
		t.Fatalf("prefs not kept: useRelays=%v carriers=%v domains=%v", c.UseRelays, c.Carriers, c.Routing.DirectDomains)
	}
}

func TestAccountJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"name":"Dave","package":"Premium","quota_limit":0,"quota_remaining":0,"quota_reset":"2026-12-01","expires":1735689600,"max_devices":5,"devices_used":2,"mbps":100,"disabled":false}`))
	}))
	defer srv.Close()
	raw, err := Account(controlInviteYAML(t, srv.URL))
	if err != nil {
		t.Fatal(err)
	}
	var a struct {
		Name        string `json:"name"`
		Package     string `json:"package"`
		MaxDevices  int    `json:"maxDevices"`
		DevicesUsed int    `json:"devicesUsed"`
	}
	if err := json.Unmarshal([]byte(raw), &a); err != nil {
		t.Fatal(err)
	}
	if a.Name != "Dave" || a.Package != "Premium" || a.MaxDevices != 5 || a.DevicesUsed != 2 {
		t.Fatalf("account = %+v", a)
	}
}

func TestAddRemoveRelay(t *testing.T) {
	y := testInviteYAML(t)
	y, err := AddRelay(y, "dvr://abc123")
	if err != nil {
		t.Fatal(err)
	}
	y, err = AddRelay(y, "abc123")
	if err != nil {
		t.Fatal(err)
	}
	c, _ := config.Parse([]byte(y))
	if len(c.Relays) != 1 || c.Relays[0] != "abc123" {
		t.Fatalf("relays = %#v", c.Relays)
	}
	y, err = RemoveRelay(y, "abc123")
	if err != nil {
		t.Fatal(err)
	}
	c, _ = config.Parse([]byte(y))
	if len(c.Relays) != 0 {
		t.Fatalf("relays after remove = %#v", c.Relays)
	}
}

func TestFormatConnected(t *testing.T) {
	got := formatConnected(client.Info{Connected: true, Endpoint: transport.Endpoint{Kind: transport.KindH3, Name: "n1"}})
	if got != "connected h3||||0|0" {
		t.Fatalf("direct = %q", got)
	}
	got = formatConnected(client.Info{
		Connected: true,
		Endpoint: transport.Endpoint{
			Kind: reverse.KindReverse, Name: "Quiet",
			Params: map[string]string{"tier": "community", "relay_id": "r1"},
		},
		Latency: 42 * time.Millisecond,
		Since:   time.UnixMilli(1700000000000),
	})
	if got != "connected reverse|Quiet|community|r1|1700000000000|42" {
		t.Fatalf("relay = %q", got)
	}
}

func TestParseRelayID(t *testing.T) {
	if product.RelayID("  dvr://rid/extra?x=1 ") != "rid" {
		t.Fatal(product.RelayID("dvr://rid/extra?x=1"))
	}
	if product.RelayID("") != "" {
		t.Fatal("empty")
	}
}

func controlInviteYAML(t *testing.T, url string) string {
	t.Helper()
	y, err := ImportInvite(invite.Invite{
		Endpoints:  []transport.Endpoint{{Kind: transport.KindH2, Addr: "127.0.0.1:1", Host: "x.example"}},
		Control:    []discovery.ControlChannel{{URL: url}},
		TrustedKey: []string{"k"}, Account: "acct",
	}.Encode())
	if err != nil {
		t.Fatal(err)
	}
	return y
}

func TestRelaysPublic(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`[{"id":"a","name":"A","tier":"system","health":"ok","public":true},{"id":"b","name":"B","tier":"community","health":"ok"}]`))
	}))
	defer srv.Close()
	raw, err := Relays(controlInviteYAML(t, srv.URL))
	if err != nil {
		t.Fatal(err)
	}
	var rows []struct {
		ID     string `json:"id"`
		Public bool   `json:"public"`
	}
	if err := json.Unmarshal([]byte(raw), &rows); err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || !rows[0].Public || rows[1].Public {
		t.Fatalf("rows = %+v", rows)
	}
}

func TestNeedsRelay(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	y := controlInviteYAML(t, srv.URL)
	if NeedsRelay(y) {
		t.Fatal("reachable control must not need a relay")
	}
	srv.Close()
	if !NeedsRelay(y) {
		t.Fatal("unreachable control must need a relay")
	}
	if NeedsRelay(testInviteYAML(t)) {
		t.Fatal("no control plane must not need a relay")
	}
}

func TestValidateAccountLogout(t *testing.T) {
	code := http.StatusOK
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(code)
	}))
	defer srv.Close()
	y := controlInviteYAML(t, srv.URL)
	got, err := ValidateAccount(y)
	if err != nil || got != y || !Configured(got) {
		t.Fatalf("valid account changed config: err=%v", err)
	}
	code = http.StatusUnauthorized
	got, err = ValidateAccount(y)
	if err != nil {
		t.Fatal(err)
	}
	if Configured(got) {
		t.Fatal("401 must clear invite material")
	}
	c, _ := config.Parse([]byte(got))
	if c.Account != "" || len(c.Control) != 0 || len(c.Endpoints) != 0 || len(c.TrustedKey) != 0 {
		t.Fatalf("config not cleared: %+v", c)
	}
	code = http.StatusInternalServerError
	if _, err := ValidateAccount(y); err == nil {
		t.Fatal("5xx must error")
	}
}
