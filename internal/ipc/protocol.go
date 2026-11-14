// SPDX-License-Identifier: AGPL-3.0-only

package ipc

import (
	"encoding/json"
	"errors"
)

// Methods. Every call is one request line and one response line (JSON, "\n"
// terminated); Subscribe is the exception: after its response the connection
// carries one Status line per change until the client closes it.
const (
	MethodStatus      = "Status"
	MethodConnect     = "Connect"
	MethodDisconnect  = "Disconnect"
	MethodImport      = "ImportInvite"
	MethodServers     = "Servers"
	MethodGetSettings = "GetSettings"
	MethodSetSettings = "SetSettings"
	MethodUnblock     = "Unblock"
	MethodSubscribe   = "Subscribe"
	MethodAccount     = "Account"
	MethodRelays      = "Relays"
	MethodAddRelay    = "AddRelay"
	MethodRemoveRelay = "RemoveRelay"
	MethodAds         = "Ads"
	MethodLogout      = "Logout"
	MethodBuildReport = "BuildReport"
	MethodProbe       = "ProbeServers"
)

// maxLine bounds one request or response; an invite is a few KB.
const maxLine = 1 << 20

// Codes a client can act on. Anything else is CodeInternal and shown as text.
const (
	CodeNotConfigured = "not_configured"
	CodeBadInvite     = "bad_invite"
	CodeBusy          = "busy"
	CodeVersion       = "version"
	// CodeAuth: control rejected the saved account key (deleted/revoked invite).
	CodeAuth     = "auth"
	CodeInternal = "internal"
)

// Error is a failed call as the client sees it.
type Error struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *Error) Error() string { return e.Message }

// Is lets errors.Is(err, &ipc.Error{Code: ...}) match on code alone.
func (e *Error) Is(target error) bool {
	t, ok := target.(*Error)
	return ok && t.Code == e.Code
}

// Coded wraps err with a code for the wire.
func Coded(code string, err error) error { return &Error{Code: code, Message: err.Error()} }

func asError(err error) *Error {
	var e *Error
	if errors.As(err, &e) {
		return e
	}
	return &Error{Code: CodeInternal, Message: err.Error()}
}

type request struct {
	V      int             `json:"v"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params,omitempty"`
}

type response struct {
	Result json.RawMessage `json:"result,omitempty"`
	Error  *Error          `json:"error,omitempty"`
}

// Backend is what the server calls: the helper's session and config.
type Backend interface {
	Status() Status
	Subscribe() (events <-chan Status, cancel func())
	// Connect starts the session with the saved settings.
	Connect() error
	// Disconnect stops it and waits until the OS is restored.
	Disconnect()
	ImportInvite(uri string) (ImportResult, error)
	Servers() []Server
	// ProbeServers dials each ladder rung once and returns servers with latency.
	ProbeServers() ([]Server, error)
	Settings() Settings
	SetSettings(Settings) error
	// Unblock removes kill-switch rules left behind by a crash.
	Unblock() error
	Account() (Account, error)
	Relays() ([]Relay, error)
	AddRelay(idOrURI string) error
	RemoveRelay(id string) error
	Ads(slot, relayID string) ([]Ad, error)
	// Logout clears the invite/account from this device and stops any session.
	Logout() error
	// BuildReport seals the recent scrubbed logs for the user to email.
	BuildReport(BuildReportParams) (ReportResult, error)
}
