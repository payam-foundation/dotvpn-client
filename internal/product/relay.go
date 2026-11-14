// SPDX-License-Identifier: AGPL-3.0-only

// Package product holds client product helpers shared by the desktop helper
// and the mobile AAR so invite/relay merge behavior cannot drift.
package product

import (
	"fmt"
	"strings"

	"github.com/payam-foundation/dotvpn-client/internal/config"
	"github.com/payam-foundation/dotvpn-wire/invite"
	"github.com/payam-foundation/dotvpn-wire/transport"
)

// RelayID returns the bare relay id from a dvr:// URI or bare id, or "" if invalid.
func RelayID(s string) string {
	id, err := invite.ParseRelayID(s)
	if err != nil {
		return ""
	}
	return id
}

// MergeRelayInvite applies a dvr:// invite into c (endpoints + id) or accepts a bare id.
func MergeRelayInvite(c *config.Config, idOrURI string) (string, error) {
	s := strings.TrimSpace(idOrURI)
	if s == "" {
		return "", fmt.Errorf("empty")
	}
	if strings.Contains(s, invite.RelayPrefix) || strings.HasPrefix(s, invite.RelayPrefix) {
		ri, err := invite.DecodeRelayInvite(s)
		if err != nil {
			// Unsigned short form dvr://<id> (ParseRelayID); reject opaque garbage there.
			id, perr := invite.ParseRelayID(s)
			if perr != nil || id == "" {
				return "", err
			}
			return id, nil
		}
		if len(c.TrustedKey) > 0 && ri.Sig != "" {
			if err := ri.Verify(c.TrustedKey); err != nil {
				return "", err
			}
		}
		if len(ri.Endpoints) > 0 {
			c.Endpoints = MergeEndpoints(c.Endpoints, ri.Endpoints)
		}
		return ri.ID, nil
	}
	id := RelayID(s)
	if id == "" {
		return "", fmt.Errorf("invalid relay id")
	}
	return id, nil
}

// MergeEndpoints appends src endpoints not already present in dst.
func MergeEndpoints(dst, src []transport.Endpoint) []transport.Endpoint {
	seen := map[string]bool{}
	key := func(ep transport.Endpoint) string {
		return string(ep.Kind) + "|" + ep.Addr + "|" + ep.Name + "|" + ep.Params["node"] + "|" + ep.Params["relay_id"]
	}
	out := append([]transport.Endpoint(nil), dst...)
	for _, ep := range dst {
		seen[key(ep)] = true
	}
	for _, ep := range src {
		k := key(ep)
		if seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, ep)
	}
	return out
}
