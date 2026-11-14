// SPDX-License-Identifier: AGPL-3.0-only

package app

import (
	"testing"

	"github.com/payam-foundation/dotvpn-wire/discovery"
	"github.com/payam-foundation/dotvpn-wire/transport"
	"github.com/payam-foundation/dotvpn-wire/transport/reverse"
)

func TestRelayTransportPolicy(t *testing.T) {
	direct := transport.Endpoint{Kind: transport.KindH3, Addr: "direct:443"}
	relay := transport.Endpoint{Kind: reverse.KindReverse, Addr: "relay:443"}
	dns := transport.Endpoint{Kind: reverse.KindDNS, Addr: "dns:53"}

	if got := eligibleEndpoints([]transport.Endpoint{direct, relay, dns}, false); len(got) != 1 || got[0].Kind != transport.KindH3 {
		t.Fatalf("relays disabled endpoints = %+v", got)
	}
	if got := eligibleEndpoints([]transport.Endpoint{direct, relay, dns}, true); len(got) != 3 {
		t.Fatalf("relays enabled endpoints = %+v", got)
	}
	if got := Dialers(transport.Options{}, false); got[reverse.KindReverse] != nil || got[reverse.KindDNS] != nil {
		t.Fatalf("relays disabled dialers = %+v", got)
	}
	if got := Dialers(transport.Options{}, true); got[reverse.KindReverse] == nil || got[reverse.KindDNS] == nil {
		t.Fatalf("relays enabled dialers = %+v", got)
	}

	eps := []transport.Endpoint{relay}
	channelHTTP := relayChannelHTTP(func() []transport.Endpoint { return eps }, transport.Options{}, func() string { return "token" })
	if got := channelHTTP(discovery.ControlChannel{}); got != nil {
		t.Fatal("channel without relay must use the default HTTP client")
	}
	if got := channelHTTP(discovery.ControlChannel{Relay: "missing"}); got != nil {
		t.Fatal("unknown relay must use the default HTTP client")
	}
	if got := channelHTTP(discovery.ControlChannel{Relay: relay.Addr}); got == nil {
		t.Fatal("matching relay must get a tunnel HTTP client")
	}
	// Bootstrap-discovered relays must be visible without config.Endpoints.
	eps = []transport.Endpoint{{Kind: reverse.KindReverse, Addr: "boot:443", Name: "boot"}}
	if got := channelHTTP(discovery.ControlChannel{Relay: "boot:443"}); got == nil {
		t.Fatal("bootstrap relay endpoints must drive ChannelHTTP")
	}
}
