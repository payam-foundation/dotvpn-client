package config

import (
	"os"
	"path/filepath"
	"testing"
)

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
