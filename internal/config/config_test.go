package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTunnelOnAndMigrate(t *testing.T) {
	off := false
	if (TunConfig{Enabled: &off}).TunnelOn() {
		t.Fatal("enabled:false must be off")
	}
	on := true
	if !(TunConfig{Enabled: &on}).TunnelOn() {
		t.Fatal("enabled:true must be on")
	}
	if !(TunConfig{}).TunnelOn() {
		t.Fatal("omitted enabled must default on")
	}

	c := &Config{Tun: TunConfig{Enabled: &off, DNSPreset: "system", DNS: "system"}}
	if !c.Migrate() {
		t.Fatal("schema 0 must migrate")
	}
	if c.Schema != SchemaCurrent || !c.Tun.TunnelOn() {
		t.Fatalf("after migrate: schema=%d tun=%+v", c.Schema, c.Tun)
	}
	if c.Tun.DNSPreset != "cloudflare" || c.Tun.DNS != "1.1.1.1:53" {
		t.Fatalf("system DNS must migrate to cloudflare, got %+v", c.Tun)
	}
	if c.DeviceID == "" || !strings.HasPrefix(c.DeviceID, "dv_") {
		t.Fatalf("device_id must be minted, got %q", c.DeviceID)
	}
	if c.Migrate() {
		t.Fatal("second migrate must be a no-op")
	}
	c.Tun.SetEnabled(false)
	if c.Migrate() || c.Tun.TunnelOn() {
		t.Fatal("explicit proxy-only after schema must stick")
	}
}

func TestRelaysCarriersDNSRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "c.yaml")
	c := &Config{
		Relays: []string{"r1", "r2"}, Carriers: []string{"h3"},
		Tun: TunConfig{DNSPreset: "google", DoH: true, DNS: "8.8.8.8:53"},
	}
	c.Defaults()
	if err := Save(path, c); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Relays) != 2 || got.Relays[0] != "r1" || len(got.Carriers) != 1 ||
		got.Tun.DNSPreset != "google" || !got.Tun.DoH {
		t.Fatalf("got %+v", got)
	}
}

func TestProxyAuth(t *testing.T) {
	file := filepath.Join(t.TempDir(), "auth")
	if err := os.WriteFile(file, []byte("bob:p:w\nignored\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name       string
		c          Config
		user, pass string
		ok, fail   bool
	}{
		{name: "unset"},
		{name: "inline", c: Config{ListenAuth: "a:b"}, user: "a", pass: "b", ok: true},
		{name: "password keeps colons", c: Config{ListenAuthFile: file}, user: "bob", pass: "p:w", ok: true},
		{name: "no password", c: Config{ListenAuth: "a"}, fail: true},
		{name: "empty user", c: Config{ListenAuth: ":b"}, fail: true},
		{name: "missing file", c: Config{ListenAuthFile: file + ".none"}, fail: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			u, p, ok, err := tc.c.ProxyAuth()
			if (err != nil) != tc.fail || ok != tc.ok || u != tc.user || p != tc.pass {
				t.Errorf("got (%q,%q,%v,%v)", u, p, ok, err)
			}
		})
	}
}

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
			err := (&Config{Listen: tc.listen, ListenAuth: tc.auth}).CheckListen()
			if (err == nil) != tc.ok {
				t.Fatalf("err = %v, want ok=%v", err, tc.ok)
			}
		})
	}
}
