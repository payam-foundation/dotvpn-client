package main

import (
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"runtime"
	"strings"
)

// The GUI never runs as root. The one thing that needs it, installing or
// removing the helper service, is the embedded CLI's `service` command run through the
// OS's own elevation prompt: osascript on macOS, pkexec on Linux, UAC on
// Windows.

const (
	cliBinaryName    = "dotvpn"
	cliBinaryEnvName = "DOTVPN_BIN"
)

// helperBinary writes the embedded dotvpn binary to a private temp dir and
// returns its path and a cleanup func. DOTVPN_BIN overrides it for
// development. The temp dir is 0700, so only this user can touch the file
// between extraction and the admin prompt.
func helperBinary() (string, func(), error) {
	if v := os.Getenv(cliBinaryEnvName); v != "" {
		return v, func() {}, nil
	}
	name := cliBinaryName
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	data, err := helperFS.ReadFile("helper/" + cliBinaryName)
	if err != nil {
		return "", nil, fmt.Errorf("this build has no embedded helper; run go generate in desktop/")
	}
	dir, err := os.MkdirTemp("", "dotvpn-setup-")
	if err != nil {
		return "", nil, err
	}
	cleanup := func() { _ = os.RemoveAll(dir) }
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, data, 0o700); err != nil {
		cleanup()
		return "", nil, err
	}
	return p, cleanup, nil
}

// elevatedCommand returns the command that runs exe with args as an
// administrator on goos. Pure, so each OS's quoting is testable anywhere.
func elevatedCommand(goos, exe string, args []string) (string, []string) {
	switch goos {
	case "darwin":
		script := `do shell script ` + appleQuote(shellJoin(append([]string{exe}, args...))) + ` with administrator privileges`
		return "osascript", []string{"-e", script}
	case "windows":
		quoted := make([]string, len(args))
		for i, a := range args {
			quoted[i] = psQuote(a)
		}
		ps := `Start-Process -Verb RunAs -Wait -WindowStyle Hidden -FilePath ` + psQuote(exe) +
			` -ArgumentList ` + strings.Join(quoted, ",")
		return "powershell", []string{"-NoProfile", "-NonInteractive", "-Command", ps}
	}
	return "pkexec", append([]string{exe}, args...)
}

// shellJoin quotes each word for /bin/sh.
func shellJoin(words []string) string {
	q := make([]string, len(words))
	for i, w := range words {
		q[i] = `'` + strings.ReplaceAll(w, `'`, `'\''`) + `'`
	}
	return strings.Join(q, " ")
}

// appleQuote is s as an AppleScript string literal.
func appleQuote(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	return `"` + s + `"`
}

// psQuote is s as a PowerShell single-quoted string.
func psQuote(s string) string { return `'` + strings.ReplaceAll(s, `'`, `''`) + `'` }

// runElevated runs the embedded `dotvpn <args>` as administrator.
func runElevated(args ...string) error {
	exe, cleanup, err := helperBinary()
	if err != nil {
		return &helperError{Code: codeNoCLI, Message: err.Error()}
	}
	defer cleanup()
	name, a := elevatedCommand(runtime.GOOS, exe, args)
	out, err := exec.Command(name, a...).CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			msg = err.Error()
		}
		return &helperError{Code: codeElevation, Message: msg}
	}
	return nil
}

// currentOwner is the id the helper should let in: the uid, or the SID on
// Windows.
func currentOwner() (string, error) {
	u, err := user.Current()
	if err != nil {
		return "", err
	}
	return u.Uid, nil
}

// canNotify reports whether the OS notification service can start. On macOS
// it needs a bundle identifier, which a bare binary (go run, a dev build)
// lacks, and a service that fails to start would end the whole app.
func canNotify() bool {
	if runtime.GOOS != "darwin" {
		return true
	}
	self, err := os.Executable()
	return err == nil && strings.Contains(self, ".app/Contents/MacOS/")
}
