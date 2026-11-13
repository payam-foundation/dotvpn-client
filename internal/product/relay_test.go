package product

import (
	"testing"

	"github.com/payam-foundation/dotvpn-client/internal/config"
	"github.com/payam-foundation/dotvpn-wire/transport"
)

func TestMergeRelayInviteBareAndShort(t *testing.T) {
	c := &config.Config{}
	id, err := MergeRelayInvite(c, "community-1")
	if err != nil || id != "community-1" {
		t.Fatalf("bare: id=%q err=%v", id, err)
	}
	id, err = MergeRelayInvite(c, "dvr://abc123")
	if err != nil || id != "abc123" {
		t.Fatalf("short: id=%q err=%v", id, err)
	}
	if _, err := MergeRelayInvite(c, ""); err == nil {
		t.Fatal("empty accepted")
	}
}

func TestMergeEndpointsDedupes(t *testing.T) {
	a := transport.Endpoint{Kind: transport.KindH2, Addr: "1.2.3.4:443", Name: "n"}
	b := transport.Endpoint{Kind: transport.KindH3, Addr: "1.2.3.4:443", Name: "n"}
	got := MergeEndpoints([]transport.Endpoint{a}, []transport.Endpoint{a, b})
	if len(got) != 2 {
		t.Fatalf("got %d", len(got))
	}
}
