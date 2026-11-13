// SPDX-License-Identifier: AGPL-3.0-only

package helper

import (
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/payam-foundation/dotvpn-client/internal/config"
	"github.com/payam-foundation/dotvpn-client/internal/ipc"
	"github.com/payam-foundation/dotvpn-client/internal/product"
	"github.com/payam-foundation/dotvpn-wire/discovery"
	"github.com/payam-foundation/dotvpn-wire/invite"
	"github.com/payam-foundation/dotvpn-wire/transport"
)

func newHelper(t *testing.T) (*Helper, *int) {
	t.Helper()
	h := New(slog.New(slog.NewTextHandler(io.Discard, nil)), filepath.Join(t.TempDir(), "client.yaml"))
	n := new(int)
	h.unblock = func(*slog.Logger) error { *n++; return nil }
	return h, n
}

func testInvite() string {
	return invite.Invite{
		TrustedKey: []string{"k"},
		Endpoints:  []transport.Endpoint{{Kind: transport.KindH2, Name: "node", Addr: "n:443"}},
	}.Encode()
}

func TestImportAndSettings(t *testing.T) {
	h, _ := newHelper(t)
	if h.Status().Configured {
		t.Fatal("fresh helper reports configured")
	}
	if err := h.Connect(); !errors.Is(err, &ipc.Error{Code: ipc.CodeNotConfigured}) {
		t.Fatalf("Connect before import = %v, want not_configured", err)
	}
	if _, err := h.ImportInvite("nope"); !errors.Is(err, &ipc.Error{Code: ipc.CodeBadInvite}) {
		t.Fatalf("bad invite = %v", err)
	}
	if _, err := h.ImportInvite(testInvite()); err != nil {
		t.Fatal(err)
	}
	if !h.Status().Configured {
		t.Fatal("not configured after import")
	}
	if err := h.Logout(); err != nil {
		t.Fatal(err)
	}
	if h.Status().Configured {
		t.Fatal("still configured after logout")
	}
	if _, err := h.ImportInvite(testInvite()); err != nil {
		t.Fatal(err)
	}
	sv := h.Servers()
	if len(sv) != 1 || sv[0].Name != "node" || sv[0].ID == "" {
		t.Fatalf("servers = %+v", sv)
	}
	want := ipc.Settings{FullTunnel: true, KillSwitch: true, DNS: "9.9.9.9:53", Pin: sv[0].ID, DirectDomains: []string{"a.ir"}, DirectCIDRs: []string{}}
	if err := h.SetSettings(want); err != nil {
		t.Fatal(err)
	}
	got := h.Settings()
	if !got.FullTunnel || !got.KillSwitch || got.DNS != want.DNS || got.Pin != want.Pin || len(got.DirectDomains) != 1 {
		t.Fatalf("settings = %+v", got)
	}
	// a kill switch alone implies the tunnel
	if err := h.SetSettings(ipc.Settings{KillSwitch: true}); err != nil {
		t.Fatal(err)
	}
	if !h.Settings().FullTunnel {
		t.Fatal("kill switch without full tunnel")
	}
}

func TestUnblock(t *testing.T) {
	h, n := newHelper(t)
	if err := h.Unblock(); err != nil || *n != 1 {
		t.Fatalf("Unblock idle: err=%v calls=%d", err, *n)
	}
}

func TestRelayOptInAndDNSSettings(t *testing.T) {
	h, _ := newHelper(t)
	if _, err := h.ImportInvite(testInvite()); err != nil {
		t.Fatal(err)
	}
	if err := h.AddRelay("community-1"); err != nil {
		t.Fatal(err)
	}
	if err := h.AddRelay("community-1"); err != nil {
		t.Fatal(err)
	}
	if err := h.AddRelay(""); !errors.Is(err, &ipc.Error{Code: ipc.CodeBadInvite}) {
		t.Fatalf("empty relay = %v", err)
	}
	want := ipc.Settings{
		FullTunnel: true, DNSPreset: "quad9", DoH: true, UseRelays: true,
		Carriers: []string{"h3", "reverse", "cdnws", "dns"}, DirectDomains: []string{}, DirectCIDRs: []string{},
	}
	if err := h.SetSettings(want); err != nil {
		t.Fatal(err)
	}
	got := h.Settings()
	if got.DNS != "9.9.9.9:53" || got.DNSPreset != "quad9" || !got.DoH || !got.UseRelays ||
		len(got.Carriers) != 2 || got.Carriers[0] != "h3" || got.Carriers[1] != "cdnws" {
		t.Fatalf("settings = %+v", got)
	}
	if err := h.RemoveRelay("community-1"); err != nil {
		t.Fatal(err)
	}
}

func TestServersCarryCountry(t *testing.T) {
	h, _ := newHelper(t)
	uri := invite.Invite{
		TrustedKey: []string{"k"},
		Endpoints: []transport.Endpoint{{
			Kind: transport.KindH2, Name: "Berlin", Addr: "n:443",
			Params: map[string]string{"cc": "DE"},
		}},
	}.Encode()
	if _, err := h.ImportInvite(uri); err != nil {
		t.Fatal(err)
	}
	sv := h.Servers()
	if len(sv) != 1 || sv[0].Country != "DE" || sv[0].Name != "Berlin" {
		t.Fatalf("servers = %+v", sv)
	}
}

func TestLoadMigratesProxyOnlyToFullTunnel(t *testing.T) {
	h, _ := newHelper(t)
	uri := invite.Invite{
		TrustedKey: []string{"k"},
		Endpoints:  []transport.Endpoint{{Kind: transport.KindH2, Name: "n", Addr: "n:443"}},
	}.Encode()
	if _, err := h.ImportInvite(uri); err != nil {
		t.Fatal(err)
	}
	c, err := config.Load(h.path)
	if err != nil {
		t.Fatal(err)
	}
	off := false
	c.Schema = 0
	c.Tun.Enabled = &off
	c.Tun.DNSPreset = "system"
	if err := config.Save(h.path, c); err != nil {
		t.Fatal(err)
	}
	h.mu.Lock()
	got, err := h.load()
	h.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	if !got.Tun.TunnelOn() || got.Schema != config.SchemaCurrent {
		t.Fatalf("expected full tunnel after load migrate, got %+v schema=%d", got.Tun, got.Schema)
	}
}

func TestMergeEndpointsKindString(t *testing.T) {
	a := transport.Endpoint{Kind: transport.KindH2, Addr: "a:443", Name: "a"}
	b := transport.Endpoint{Kind: transport.KindH3, Addr: "b:443", Name: "b"}
	got := product.MergeEndpoints([]transport.Endpoint{a}, []transport.Endpoint{a, b})
	if len(got) != 2 {
		t.Fatalf("merge = %+v", got)
	}
}

func TestAccountUnauthorizedClearsInvite(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/account" {
			http.NotFound(w, r)
			return
		}
		http.Error(w, `{"error":"invalid account"}`, http.StatusUnauthorized)
	}))
	t.Cleanup(srv.Close)

	h, _ := newHelper(t)
	uri := invite.Invite{
		TrustedKey: []string{"k"},
		Account:    "dead-key",
		Control:    []discovery.ControlChannel{{URL: srv.URL}},
		Endpoints:  []transport.Endpoint{{Kind: transport.KindH2, Name: "n", Addr: "n:443"}},
	}.Encode()
	if _, err := h.ImportInvite(uri); err != nil {
		t.Fatal(err)
	}
	if !h.Status().Configured {
		t.Fatal("expected configured before account check")
	}
	_, err := h.Account()
	if !errors.Is(err, &ipc.Error{Code: ipc.CodeAuth}) {
		t.Fatalf("Account = %v, want auth", err)
	}
	if h.Status().Configured {
		t.Fatal("stale invite must be cleared after auth rejection")
	}
}
