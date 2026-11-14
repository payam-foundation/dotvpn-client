// SPDX-License-Identifier: AGPL-3.0-only

package ipc

import (
	"os"
	"runtime"
)

// DefaultAddr is where the helper listens: a unix socket in a root-owned
// directory, or a named pipe on Windows. DOTVPN_ADDR overrides it (tests, lab).
func DefaultAddr() string {
	if v := os.Getenv("DOTVPN_ADDR"); v != "" {
		return v
	}
	switch runtime.GOOS {
	case "windows":
		return `\\.\pipe\dotvpn`
	case "darwin":
		return "/var/run/dotvpn/dotvpn.sock"
	}
	return "/run/dotvpn/dotvpn.sock"
}
