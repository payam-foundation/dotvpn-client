package mobile

import (
	"net/netip"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"golang.zx2c4.com/wireguard/tun/netstack"

	"github.com/payam-foundation/dotvpn-client/internal/config"
	"github.com/payam-foundation/dotvpn-wire/invite"
	"github.com/payam-foundation/dotvpn-wire/transport"
)

type fakeProtector struct{ n atomic.Int32 }

func (f *fakeProtector) Protect(int) bool { f.n.Add(1); return true }

func TestImportInvite(t *testing.T) {
	uri := invite.Invite{
		Endpoints:  []transport.Endpoint{{Kind: transport.KindH2, Addr: "127.0.0.1:1", Host: "x.example"}},
		TrustedKey: []string{"k"}, Token: "tok",
	}.Encode()
	y, err := ImportInvite(uri)
	if err != nil {
		t.Fatal(err)
	}
	c, err := config.Parse([]byte(y))
	if err != nil {
		t.Fatal(err)
	}
	if !c.Tun.Enabled || c.Token != "tok" || len(c.Endpoints) != 1 {
		t.Fatalf("config = %+v", c)
	}
	if l, err := Endpoints(y); err != nil || strings.TrimSpace(l) == "" {
		t.Errorf("Endpoints = %q, %v", l, err)
	}
	for _, bad := range []string{"", "dotvpn://!!", "https://x"} {
		if _, err := ImportInvite(bad); err == nil {
			t.Errorf("ImportInvite(%q) accepted", bad)
		}
	}
}

// Start dials the carrier through the Protector, reports progress in Status,
// refuses a second Start and tears down on Stop.
func TestStartStop(t *testing.T) {
	y, err := ImportInvite(invite.Invite{
		Endpoints:  []transport.Endpoint{{Kind: transport.KindH2, Addr: "127.0.0.1:1", Host: "x.example"}},
		TrustedKey: []string{"k"}, Token: "tok",
	}.Encode())
	if err != nil {
		t.Fatal(err)
	}
	c, err := config.Parse([]byte(y))
	if err != nil {
		t.Fatal(err)
	}
	c.StateFile, c.ProbeTarget = "off", "off"
	b, _ := config.Marshal(c)
	y = string(b)
	dev, _, err := netstack.CreateNetTUN([]netip.Addr{netip.MustParseAddr("198.18.0.1")}, nil, 1380)
	if err != nil {
		t.Fatal(err)
	}
	p := &fakeProtector{}
	if err := start(y, t.TempDir(), dev, p); err != nil {
		t.Fatal(err)
	}
	if s := Status(); s != "connecting" {
		t.Errorf("status = %q", s)
	}
	if err := start(y, t.TempDir(), dev, p); err == nil || !strings.Contains(err.Error(), "already") {
		t.Errorf("second start: %v", err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for p.n.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if p.n.Load() == 0 {
		t.Error("engine socket never went through Protect")
	}
	Stop()
	Stop() // idempotent
	if s := Status(); s != "off" {
		t.Errorf("status after stop = %q", s)
	}
}

func TestStartRejects(t *testing.T) {
	if err := Start("tun: [", t.TempDir(), 3, &fakeProtector{}); err == nil {
		t.Error("device outside Android accepted")
	}
	dev, _, _ := netstack.CreateNetTUN([]netip.Addr{netip.MustParseAddr("198.18.0.1")}, nil, 1380)
	if err := start("tun: [", t.TempDir(), dev, &fakeProtector{}); err == nil {
		t.Error("bad config accepted")
	}
	if err := start("", t.TempDir(), dev, nil); err == nil {
		t.Error("nil protector accepted")
	}
}
