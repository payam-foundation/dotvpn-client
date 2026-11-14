// SPDX-License-Identifier: AGPL-3.0-only

package diag

import (
	"regexp"
	"strings"
)

var (
	reIPv4   = regexp.MustCompile(`\b(?:\d{1,3}\.){3}\d{1,3}\b`)
	reIPv6   = regexp.MustCompile(`\b(?:[0-9a-fA-F]{0,4}:){2,7}[0-9a-fA-F]{0,4}\b`)
	reBearer = regexp.MustCompile(`(?i)bearer\s+[a-z0-9._\-]{8,}`)
	reInvite = regexp.MustCompile(`(?i)dv://[^\s"']+`)
	reHost   = regexp.MustCompile(`\b[a-z0-9][a-z0-9.\-]+\.(com|net|org|io|dev)(:\d+)?\b`)
)

var banned = []string{
	"vpn", "tunnel", "kill_switch", "kill-switch", "carrier", "knock", "socks", "wireguard",
}

// scrubLine removes secrets and fingerprintable tokens from a short detail field.
func scrubLine(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	low := strings.ToLower(s)
	for _, b := range banned {
		if strings.Contains(low, b) {
			return "redacted"
		}
	}
	s = reInvite.ReplaceAllString(s, "[invite]")
	s = reBearer.ReplaceAllString(s, "[token]")
	s = reIPv4.ReplaceAllString(s, "[ip]")
	s = reIPv6.ReplaceAllString(s, "[ip]")
	s = reHost.ReplaceAllString(s, "[host]")
	if len(s) > 80 {
		s = s[:80]
	}
	return s
}
