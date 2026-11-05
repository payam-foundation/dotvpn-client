package main

import (
	"os"
	"testing"

	"github.com/payam-foundation/dotvpn-client/internal/config"
)

// Off-host binds are only allowed with proxy authentication.
func TestCheckListen(t *testing.T) {
	for _, tc := range []struct {
		name, listen, auth string
		ok                 bool
	}{
		{"loopback no auth", "127.0.0.1:1080", "", true},
		{"loopback v6", "[::1]:1080", "", true},
		{"loopback with auth", "127.0.0.1:1080", "u:p", true},
		{"any no auth", "0.0.0.0:1080", "", false},
		{"empty host no auth", ":1080", "", false},
		{"lan no auth", "192.168.1.5:1080", "", false},
		{"lan with auth", "192.168.1.5:1080", "u:p", true},
		{"any with auth", "0.0.0.0:1080", "u:p", true},
		{"unix refused", "unix:/tmp/x", "u:p", false},
		{"malformed auth", "0.0.0.0:1080", "nopass", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := checkListen(&config.Config{Listen: tc.listen, ListenAuth: tc.auth})
			if (err == nil) != tc.ok {
				t.Fatalf("err = %v, want ok=%v", err, tc.ok)
			}
		})
	}
}

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
