// Package ipc is the contract between the privileged helper (dotvpn daemon)
// and its unprivileged clients, the desktop app and the CLI. This file holds
// the wire types; the protocol, server and client are beside it.
//
// The helper owns the config. Nothing here carries a token, account key or
// node address, so a client that is compromised or logs everything leaks none.
package ipc

import "time"

// Version is the protocol version; client and helper must match.
// Bump when the helper binary must be reinstalled for a protocol or
// behavior change the desktop cannot paper over.
const Version = 12

// State is where the connection is in its life.
type State string

const (
	// StateOff: nothing running.
	StateOff State = "off"
	// StateConnecting: first connect, or retry after a failure.
	StateConnecting State = "connecting"
	// StateSwitching: the carrier dropped while connected; reconnecting. With
	// the kill switch on, traffic stays blocked meanwhile.
	StateSwitching State = "switching"
	// StateConnected: tunnel carries traffic.
	StateConnected State = "connected"
	// StateFailed: the last attempt failed. The helper keeps retrying unless
	// the failure was fatal (see Status.Running).
	StateFailed State = "failed"
)

// Status is a point-in-time view of the connection.
type Status struct {
	State State `json:"state"`
	// Running is true while a session exists (connecting, switching,
	// connected, or failed and retrying).
	Running bool `json:"running"`
	// Error is the last failure, in the helper's words.
	Error string `json:"error,omitempty"`
	// Carrier is the active transport ("h3", "cdnws", ...), Server its name.
	Carrier string `json:"carrier,omitempty"`
	Server  string `json:"server,omitempty"`
	Country string `json:"country,omitempty"`
	// ViaRelay is set when the active path is reverse/dns: the relay display name.
	ViaRelay     string `json:"viaRelay,omitempty"`
	ViaRelayTier string `json:"viaRelayTier,omitempty"`
	// ViaRelayID is the control-plane relay id for ad/slot lookups (Params["relay_id"]).
	ViaRelayID string    `json:"viaRelayId,omitempty"`
	LatencyMS  int       `json:"latencyMs,omitempty"`
	Since        time.Time `json:"since,omitzero"`
	// ExitIP is the probed public address through the tunnel, when known.
	ExitIP string `json:"exitIp,omitempty"`
	// PublicIP is the device's current public address (direct), when known.
	PublicIP string `json:"publicIp,omitempty"`
	// UpBytes and DownBytes count payload through the tunnel since Since of
	// this session.
	UpBytes    uint64 `json:"upBytes"`
	DownBytes  uint64 `json:"downBytes"`
	FullTunnel bool   `json:"fullTunnel"`
	KillSwitch bool   `json:"killSwitch"`
	// Configured is false until an invite has been imported.
	Configured bool      `json:"configured"`
	At         time.Time `json:"at"`
}

// Server is one rung of the fallback ladder (one carrier endpoint).
type Server struct {
	// ID is what Settings.Pin takes.
	ID        string `json:"id"`
	Name      string `json:"name"`
	Country   string `json:"country,omitempty"`
	Carrier   string `json:"carrier"`
	Active    bool   `json:"active"`
	LatencyMS int    `json:"latencyMs,omitempty"`
	// Error is the last failure of this endpoint, empty after a success.
	Error string `json:"error,omitempty"`
	// CooldownMS is how long until a failed endpoint is preferred again.
	CooldownMS int `json:"cooldownMs,omitempty"`
}

// Account is the safe view of the subscription (no keys).
type Account struct {
	Name           string `json:"name"`
	Package        string `json:"package,omitempty"`
	QuotaLimit     int64  `json:"quotaLimit"`
	QuotaUsed      int64  `json:"quotaUsed"`
	QuotaRemaining int64  `json:"quotaRemaining"`
	QuotaReset     string `json:"quotaReset,omitempty"`
	Expires        int64  `json:"expires,omitempty"`
	MaxDevices     int    `json:"maxDevices"`
	// DevicesUsed is how many seats are in use (at least this install).
	DevicesUsed int  `json:"devicesUsed,omitempty"`
	Mbps        int  `json:"mbps,omitempty"`
	Disabled    bool `json:"disabled"`
}

// Ad is a client ad slot payload from control.
type Ad struct {
	ID       string `json:"id"`
	Issuer   string `json:"issuer"`
	RelayID  string `json:"relayId,omitempty"`
	Format   string `json:"format"`
	ImageURL string `json:"imageUrl,omitempty"`
	IconURL  string `json:"iconUrl,omitempty"`
	Title    string `json:"title,omitempty"`
	Desc     string `json:"desc,omitempty"`
	Link     string `json:"link,omitempty"`
}

// Relay is a safe curated/community relay row.
type Relay struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Country string `json:"country,omitempty"`
	Tier    string `json:"tier"`
	Health  string `json:"health"`
	Invite  string `json:"invite,omitempty"`
	// Public is true when the relay is in the open catalog (system or donated).
	Public bool `json:"public"`
	// OptedIn is true when the user pasted this id (community / private).
	OptedIn bool `json:"optedIn"`
}

// ImportResult is the outcome of ImportInvite.
type ImportResult struct {
	// NeedsRelay is true when the invite saved but control was unreachable,
	// so the UI should offer a bootstrap relay.
	NeedsRelay bool `json:"needsRelay"`
}

// Settings are the helper-owned options. The app's own preferences (language,
// theme, start at login) never leave the app.
type Settings struct {
	// FullTunnel routes all traffic; off serves the local proxy only.
	FullTunnel bool `json:"fullTunnel"`
	KillSwitch bool `json:"killSwitch"`
	// DNS is the resolver the client itself uses, "ip:port".
	DNS string `json:"dns"`
	// DNSPreset names a known resolver (system, cloudflare, google, quad9, custom).
	DNSPreset string `json:"dnsPreset,omitempty"`
	// DoH uses DNS-over-HTTPS for bypass/ECH lookups when the preset supports it.
	DoH bool `json:"doh"`
	// SystemDNS is the host resolver Dot VPN sees when the preset is system
	// (first nameserver from resolv.conf / platform equivalent). Read-only.
	SystemDNS string `json:"systemDns,omitempty"`
	// Pin is a Server.ID; empty means automatic.
	Pin string `json:"pin,omitempty"`
	// UseRelays enables reverse and DNS relay transports.
	UseRelays bool `json:"useRelays"`
	// Carriers lists enabled kinds; empty means Auto (all).
	Carriers      []string `json:"carriers,omitempty"`
	DirectDomains []string `json:"directDomains"`
	DirectCIDRs   []string `json:"directCidrs"`
	// CrashReporting is opt-in crash reporting (off by default).
	CrashReporting bool `json:"crashReporting"`
}

// ReportResult is the outcome of Send diagnostics.
type ReportResult struct {
	IssueURL string `json:"issueUrl"`
	Via      string `json:"via"` // control | browser
}
