package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/payam-foundation/dotvpn-client/internal/ipc"
)

func TestElevatedCommand(t *testing.T) {
	exe := "/Applications/Dot VPN.app/dotvpn"
	args := []string{"service", "install", "-owner", "501"}

	name, a := elevatedCommand("linux", exe, args)
	if name != "pkexec" || !reflect.DeepEqual(a, append([]string{exe}, args...)) {
		t.Fatalf("linux: %s %q", name, a)
	}

	name, a = elevatedCommand("darwin", exe, args)
	want := `do shell script "'/Applications/Dot VPN.app/dotvpn' 'service' 'install' '-owner' '501'" with administrator privileges`
	if name != "osascript" || a[1] != want {
		t.Fatalf("darwin: %s %q", name, a)
	}

	name, a = elevatedCommand("windows", `C:\Program Files\Dot VPN\dotvpn.exe`, args)
	if name != "powershell" || !strings.Contains(a[3], `-FilePath 'C:\Program Files\Dot VPN\dotvpn.exe' -ArgumentList 'service','install','-owner','501'`) {
		t.Fatalf("windows: %s %q", name, a)
	}
}

func TestElevatedCommandQuotesHostileInput(t *testing.T) {
	_, a := elevatedCommand("darwin", "/tmp/a'b\"c", []string{"x"})
	if !strings.Contains(a[1], `'/tmp/a'\\''b\"c'`) {
		t.Fatalf("not escaped for shell and AppleScript: %s", a[1])
	}
	_, a = elevatedCommand("windows", "x", []string{"it's"})
	if !strings.Contains(a[3], `'it''s'`) {
		t.Fatalf("not escaped for PowerShell: %s", a[3])
	}
}

func TestToErr(t *testing.T) {
	cases := []struct {
		in   error
		code string
	}{
		{&ipc.Error{Code: ipc.CodeBadInvite, Message: "bad"}, ipc.CodeBadInvite},
		{os.ErrNotExist, codeHelperDown},
		{errors.New("dial unix /x: connect: connection refused"), codeHelperDown},
		{errors.New("boom"), ipc.CodeInternal},
		{&helperError{Code: codeElevation, Message: "no"}, codeElevation},
	}
	for _, c := range cases {
		he, ok := toErr(c.in).(*helperError)
		if !ok || he.Code != c.code {
			t.Errorf("toErr(%v) = %#v, want code %s", c.in, toErr(c.in), c.code)
		}
	}
	if toErr(nil) != nil {
		t.Error("nil must stay nil")
	}
}

func TestMarshalError(t *testing.T) {
	var got helperError
	if err := json.Unmarshal(marshalError(&ipc.Error{Code: ipc.CodeBusy, Message: "busy"}), &got); err != nil {
		t.Fatal(err)
	}
	if got.Code != ipc.CodeBusy || got.Message != "busy" {
		t.Fatalf("got %+v", got)
	}
}

func TestIconKind(t *testing.T) {
	cases := []struct {
		up   bool
		st   ipc.State
		want string
	}{
		{false, ipc.StateConnected, "off"},
		{true, ipc.StateOff, "off"},
		{true, ipc.StateConnecting, "busy"},
		{true, ipc.StateSwitching, "busy"},
		{true, ipc.StateConnected, "on"},
		{true, ipc.StateFailed, "fail"},
	}
	for _, c := range cases {
		if got := iconKind(c.up, ipc.Status{State: c.st}); got != c.want {
			t.Errorf("up=%v %s: %s, want %s", c.up, c.st, got, c.want)
		}
	}
	for _, k := range []string{"off", "busy", "on", "fail"} {
		for _, p := range []string{"icons/tray-mono-", "icons/tray-color-"} {
			if _, err := iconFS.ReadFile(p + k + ".png"); err != nil {
				t.Errorf("missing icon %s%s: %v", p, k, err)
			}
		}
	}
}

func TestStateLine(t *testing.T) {
	st := ipc.Status{State: ipc.StateConnected, Carrier: "h3", LatencyMS: 42}
	if got := stateLine("en", st); got != "Connected · h3 · 42 ms" {
		t.Errorf("en: %q", got)
	}
	if got := stateLine("fa", st); got != "متصل · h3 · ۴۲ میلی‌ثانیه" {
		t.Errorf("fa: %q", got)
	}
	if got := stateLine("en", ipc.Status{State: ipc.StateConnecting, Carrier: "h3"}); got != "Connecting" {
		t.Errorf("connecting must not show a carrier yet: %q", got)
	}
}

func TestCopyKeysMatch(t *testing.T) {
	for k := range copyFor["en"] {
		if copyFor["fa"][k] == "" {
			t.Errorf("fa missing %q", k)
		}
	}
}

func TestInviteFrom(t *testing.T) {
	if got := inviteFrom([]string{"--background", "dotvpn://abc"}); got != "dotvpn://abc" {
		t.Errorf("got %q", got)
	}
	if inviteFrom([]string{"--background", "https://x"}) != "" {
		t.Error("only dotvpn:// links count")
	}
}

func TestPrefsRoundTrip(t *testing.T) {
	s := &prefStore{path: filepath.Join(t.TempDir(), "d", "desktop.json"), cur: defaultPrefs()}
	if err := s.set(Prefs{Language: "fa", Theme: "dark"}); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(s.path)
	if err != nil {
		t.Fatal(err)
	}
	var got Prefs
	if err := json.Unmarshal(b, &got); err != nil || got.Language != "fa" || got.Theme != "dark" {
		t.Fatalf("%v %+v", err, got)
	}
	if err := s.set(Prefs{Language: "xx", Theme: "dark"}); err == nil {
		t.Error("unknown language accepted")
	}
	if err := s.set(Prefs{Theme: "neon"}); err == nil {
		t.Error("unknown theme accepted")
	}
	if s.get().Language != "fa" {
		t.Error("a rejected set must not change prefs")
	}
}
