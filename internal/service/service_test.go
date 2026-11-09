package service

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRender(t *testing.T) {
	s := Spec{Exe: "/usr/local/bin/dotvpn", Config: "/etc/dotvpn/client.yaml", Owner: "501"}
	p := plist(s)
	for _, w := range []string{"<string>org.dotvpn.helper</string>", "<string>/usr/local/bin/dotvpn</string>", "<string>-owner</string>", "<string>501</string>", "<key>KeepAlive</key><true/>"} {
		if !strings.Contains(p, w) {
			t.Errorf("plist missing %q:\n%s", w, p)
		}
	}
	u := unit(s)
	if !strings.Contains(u, "ExecStart=/usr/local/bin/dotvpn daemon -config /etc/dotvpn/client.yaml -owner 501\n") {
		t.Errorf("unit ExecStart wrong:\n%s", u)
	}
	if !strings.Contains(unit(Spec{Exe: "/opt/a b/dotvpn", Config: "/c"}), `ExecStart="/opt/a b/dotvpn" daemon`) {
		t.Error("exe with a space must be quoted")
	}
}

func TestPlace(t *testing.T) {
	d := t.TempDir()
	src, dst := filepath.Join(d, "src"), filepath.Join(d, "sub", "dotvpn")
	if err := os.WriteFile(src, []byte("v1"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, body := range []string{"v1", "v2"} { // second pass replaces an existing copy
		if err := os.WriteFile(src, []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := place(src, dst); err != nil {
			t.Fatal(err)
		}
		if got, _ := os.ReadFile(dst); string(got) != body {
			t.Fatalf("dst = %q, want %q", got, body)
		}
	}
	if err := place(dst, dst); err != nil { // already in place
		t.Fatal(err)
	}
	if _, err := os.Stat(dst + ".new"); err == nil {
		t.Error("temp file left behind")
	}
}

func TestValidate(t *testing.T) {
	if (Spec{Exe: "/x", Config: "/c"}).validate() != nil {
		t.Error("plain spec rejected")
	}
	for _, s := range []Spec{{}, {Exe: "/x", Config: "/c\n"}, {Exe: "/x</string>", Config: "/c"}, {Exe: "/x", Config: "/c", Owner: "1&2"}} {
		if s.validate() == nil {
			t.Errorf("%+v accepted", s)
		}
	}
}
