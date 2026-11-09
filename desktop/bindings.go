package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"runtime"
	"strings"

	"github.com/payam-foundation/dotvpn-client/internal/ipc"
)

// Codes beyond ipc's own that only the app produces.
const (
	codeHelperDown = "helper_down"
	codeElevation  = "elevation"
	codeNoCLI      = "no_cli"
)

// helperError is what the frontend sees in `err.cause`: a code it can branch
// on and a message it can show.
type helperError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *helperError) Error() string { return e.Message }

// toErr maps helper and socket failures to helperError.
func toErr(err error) error {
	if err == nil {
		return nil
	}
	var he *helperError
	if errors.As(err, &he) {
		return he
	}
	var ie *ipc.Error
	if errors.As(err, &ie) {
		return &helperError{Code: ie.Code, Message: ie.Message}
	}
	if isHelperDown(err) {
		return &helperError{Code: codeHelperDown, Message: "cannot reach the helper"}
	}
	return &helperError{Code: ipc.CodeInternal, Message: err.Error()}
}

func isHelperDown(err error) bool {
	return errors.Is(err, os.ErrNotExist) || errors.Is(err, os.ErrPermission) ||
		strings.Contains(err.Error(), "connection refused") ||
		strings.Contains(err.Error(), "pipe") // Windows: no such pipe
}

// marshalError makes Wails send helperError as JSON, so `err.cause.code`
// reaches the frontend. Anything else gets Wails' default.
func marshalError(err error) []byte {
	var he *helperError
	if !errors.As(toErr(err), &he) {
		return nil
	}
	b, jerr := json.Marshal(he)
	if jerr != nil {
		return nil
	}
	return b
}

// AppService is the set of methods the frontend can call. Each is a thin
// pass-through to the helper; the app holds no connection logic of its own.
type AppService struct {
	d *desktop
}

// Info is static facts about this build.
type Info struct {
	OS      string `json:"os"`
	Version string `json:"version"`
	// TrayHidden is true on Linux desktops where no tray host was found, so
	// closing the window would lose the app. The window then stays.
	TrayHidden bool `json:"trayHidden"`
}

func (s *AppService) Info() Info {
	return Info{OS: runtime.GOOS, Version: version, TrayHidden: !s.d.trayOK()}
}

func (s *AppService) Status(ctx context.Context) (ipc.Status, error) {
	st, err := s.d.ipc.Status(ctx)
	return st, toErr(err)
}

func (s *AppService) Connect(ctx context.Context) error { return toErr(s.d.ipc.Connect(ctx)) }

func (s *AppService) Disconnect(ctx context.Context) error {
	return toErr(s.d.ipc.Disconnect(ctx))
}

func (s *AppService) ImportInvite(ctx context.Context, uri string) error {
	return toErr(s.d.ipc.ImportInvite(ctx, strings.TrimSpace(uri)))
}

func (s *AppService) Servers(ctx context.Context) ([]ipc.Server, error) {
	sv, err := s.d.ipc.Servers(ctx)
	if sv == nil {
		sv = []ipc.Server{}
	}
	return sv, toErr(err)
}

func (s *AppService) Settings(ctx context.Context) (ipc.Settings, error) {
	st, err := s.d.ipc.Settings(ctx)
	return st, toErr(err)
}

func (s *AppService) SetSettings(ctx context.Context, st ipc.Settings) error {
	return toErr(s.d.ipc.SetSettings(ctx, st))
}

func (s *AppService) Unblock(ctx context.Context) error { return toErr(s.d.ipc.Unblock(ctx)) }

func (s *AppService) Account(ctx context.Context) (ipc.Account, error) {
	a, err := s.d.ipc.Account(ctx)
	return a, toErr(err)
}

func (s *AppService) Relays(ctx context.Context) ([]ipc.Relay, error) {
	rs, err := s.d.ipc.Relays(ctx)
	if rs == nil {
		rs = []ipc.Relay{}
	}
	return rs, toErr(err)
}

func (s *AppService) AddRelay(ctx context.Context, idOrURI string) error {
	return toErr(s.d.ipc.AddRelay(ctx, strings.TrimSpace(idOrURI)))
}

func (s *AppService) RemoveRelay(ctx context.Context, id string) error {
	return toErr(s.d.ipc.RemoveRelay(ctx, strings.TrimSpace(id)))
}

func (s *AppService) Logout(ctx context.Context) error {
	return toErr(s.d.ipc.Logout(ctx))
}

// InstallHelper installs the helper service behind one OS admin prompt.
func (s *AppService) InstallHelper() error {
	owner, err := currentOwner()
	if err != nil {
		return toErr(err)
	}
	if err := runElevated("service", "install", "-owner", owner); err != nil {
		return toErr(err)
	}
	s.d.watchNow()
	return nil
}

// RemoveHelper uninstalls the helper service, which also drops any tunnel.
func (s *AppService) RemoveHelper() error {
	return toErr(runElevated("service", "uninstall"))
}

func (s *AppService) Prefs() Prefs {
	p := s.d.prefs.get()
	if on, err := s.d.app.Autostart.IsEnabled(); err == nil {
		p.StartAtLogin = on
	}
	return p
}

func (s *AppService) SetPrefs(p Prefs) error {
	if err := s.d.setPrefs(p); err != nil {
		return toErr(err)
	}
	return nil
}

// TakeInvite returns the outcome of a dotvpn:// link the app was opened with
// before the frontend was listening, once.
func (s *AppService) TakeInvite() *InviteResult { return s.d.takeInvite() }
