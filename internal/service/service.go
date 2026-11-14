// SPDX-License-Identifier: AGPL-3.0-only

// Package service installs the helper as an OS service: a LaunchDaemon on
// macOS, a systemd unit on Linux, a Windows service. Install is the one step
// that needs administrator rights; the GUI runs it once, elevated.
package service

import (
	"fmt"
	"strings"
)

// Name is the service identifier on every OS.
const Name = "dotvpn"

// Label is the launchd label.
const Label = "org.dotvpn.helper"

// Status is whether the helper is installed and running.
type Status struct {
	Installed bool `json:"installed"`
	Running   bool `json:"running"`
}

// Spec is what the service runs.
type Spec struct {
	// Exe is the absolute path of the dotvpn binary.
	Exe string
	// Config is the helper's config file.
	Config string
	// Owner is the user id (SID on Windows) allowed to talk to the helper.
	Owner string
}

func (s Spec) args() []string {
	a := []string{"daemon", "-config", s.Config}
	if s.Owner != "" {
		a = append(a, "-owner", s.Owner)
	}
	return a
}

func (s Spec) validate() error {
	if s.Exe == "" || s.Config == "" {
		return fmt.Errorf("service: exe and config are required")
	}
	for _, v := range append([]string{s.Exe}, s.args()...) {
		if strings.ContainsAny(v, "\n\r\"<>&") {
			return fmt.Errorf("service: %q has characters that cannot be written into a service definition", v)
		}
	}
	return nil
}

// plist renders the LaunchDaemon. KeepAlive restarts the helper after a crash,
// and its startup Unblock then clears any kill-switch rules left behind.
func plist(s Spec) string {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key><string>` + Label + `</string>
	<key>ProgramArguments</key>
	<array>
		<string>` + s.Exe + `</string>
`)
	for _, a := range s.args() {
		b.WriteString("\t\t<string>" + a + "</string>\n")
	}
	b.WriteString(`	</array>
	<key>RunAtLoad</key><true/>
	<key>KeepAlive</key><true/>
	<key>StandardErrorPath</key><string>/var/log/dotvpn.log</string>
</dict>
</plist>
`)
	return b.String()
}

// unit renders the systemd unit.
func unit(s Spec) string {
	exec := quoteSystemd(s.Exe)
	for _, a := range s.args() {
		exec += " " + quoteSystemd(a)
	}
	return `[Unit]
Description=Dot VPN helper
After=network-online.target
Wants=network-online.target

[Service]
ExecStart=` + exec + `
Restart=on-failure
RestartSec=2
RuntimeDirectory=dotvpn

[Install]
WantedBy=multi-user.target
`
}

// quoteSystemd quotes an argument that contains whitespace.
func quoteSystemd(a string) string {
	if strings.ContainsAny(a, " \t") {
		return `"` + a + `"`
	}
	return a
}
