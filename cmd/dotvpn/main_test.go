// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"os"
	"testing"

	"github.com/payam-foundation/dotvpn-client/internal/ipc"
)

func TestConnectedLine(t *testing.T) {
	if got, want := connectedLine("h3", false), "connected via h3"; got != want {
		t.Fatalf("plain = %q, want %q", got, want)
	}
	if got, want := connectedLine("h3", true), "\x1b[38;2;198;244;50mconnected\x1b[0m via h3"; got != want {
		t.Fatalf("colour = %q, want %q", got, want)
	}
}

func TestUseColor(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "out")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if useColor(f, "") {
		t.Fatal("a regular file must not get colour")
	}
	tty, err := os.Open(os.DevNull) // a character device, like a terminal
	if err != nil {
		t.Fatal(err)
	}
	defer tty.Close()
	if !useColor(tty, "") {
		t.Fatal("a character device should get colour")
	}
	if useColor(tty, "1") {
		t.Fatal("NO_COLOR must disable colour")
	}
}

func TestStatusLine(t *testing.T) {
	st := ipc.Status{State: ipc.StateConnected, Carrier: "h3", Server: "fra", LatencyMS: 42, FullTunnel: true, KillSwitch: true}
	if got, want := statusLine(st), "connected  via h3  fra  42ms  full tunnel  kill switch"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	if got := statusLine(ipc.Status{State: ipc.StateOff}); got != "off" {
		t.Fatalf("got %q", got)
	}
}
