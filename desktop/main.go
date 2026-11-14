// SPDX-License-Identifier: AGPL-3.0-only

// Command dotvpn-desktop is the Dot VPN window and tray icon. It runs as the
// logged-in user and holds no VPN logic: it talks to the privileged helper
// (dotvpn daemon) over internal/ipc, the same way the dotvpn CLI does.
package main

import (
	"embed"
	"log"
	"os"
	"slices"
)

//go:generate go run ./tools/buildhelper

//go:embed all:frontend/dist
var assets embed.FS

// helperFS holds the dotvpn binary (helper/dotvpn, made by go generate). The
// app installs it as the privileged service, so no separate install is needed.
//
//go:embed all:helper
var helperFS embed.FS

func main() {
	args := os.Args[1:]
	background := slices.Contains(args, backgroundFlag)
	if err := run(background, args); err != nil {
		log.Fatal(err)
	}
}
