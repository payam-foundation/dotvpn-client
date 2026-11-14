// SPDX-License-Identifier: AGPL-3.0-only

package report

import (
	"regexp"
	"strings"
)

var (
	reInvite = regexp.MustCompile(`(?i)dv://[^\s"']+`)
	reBearer = regexp.MustCompile(`(?i)bearer\s+[a-z0-9._\-]{8,}`)
	reJWT    = regexp.MustCompile(`\beyJ[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}\b`)
	// Account, join and node secrets: ak_, jt_, ns_, at_, lt_ (and legacy dvacc_/dvkey_).
	reSecret = regexp.MustCompile(`(?i)\b(?:ak|jt|ns|at|lt|dvacc|dvkey)_[0-9a-z]{8,}`)
	reIPv4   = regexp.MustCompile(`\b(?:\d{1,3}\.){3}\d{1,3}(?::\d{1,5})?\b`)
	// Candidate IPv6; scrubIPv6 keeps clock-like matches such as 17:45:00.
	reIPv6 = regexp.MustCompile(`(?i)(?:[0-9a-f]{0,4}:){2,7}[0-9a-f]{0,4}`)
	// name.tld[:port], any alphabetic TLD.
	reHost = regexp.MustCompile(`(?i)\b(?:[a-z0-9](?:[a-z0-9-]*[a-z0-9])?\.)+[a-z]{2,}(?::\d{1,5})?\b`)
	// Long opaque tokens: keys, hashes, base64 blobs.
	reBlob = regexp.MustCompile(`[A-Za-z0-9_+/=-]{32,}`)
)

func scrubIPv6(m string) string {
	if strings.Contains(m, "::") || strings.Count(m, ":") >= 5 {
		return "[ip]"
	}
	return m
}

// Scrub removes secrets, addresses and hostnames from one log line. It runs
// at record time, so nothing unscrubbed ever sits in the ring.
func Scrub(s string) string {
	s = reInvite.ReplaceAllString(s, "[invite]")
	s = reBearer.ReplaceAllString(s, "[token]")
	s = reJWT.ReplaceAllString(s, "[jwt]")
	s = reSecret.ReplaceAllString(s, "[key]")
	s = reIPv4.ReplaceAllString(s, "[ip]")
	s = reIPv6.ReplaceAllStringFunc(s, scrubIPv6)
	s = reHost.ReplaceAllString(s, "[host]")
	s = reBlob.ReplaceAllString(s, "[blob]")
	return s
}
