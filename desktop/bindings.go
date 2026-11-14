// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"runtime"
	"strings"
	"time"

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

func (s *AppService) Connect(ctx context.Context) error {
	slog.Info("connect")
	err := toErr(s.d.ipc.Connect(ctx))
	if err != nil {
		slog.Warn("connect failed", "err", err)
	}
	return err
}

func (s *AppService) Disconnect(ctx context.Context) error {
	slog.Info("disconnect")
	return toErr(s.d.ipc.Disconnect(ctx))
}

func (s *AppService) ImportInvite(ctx context.Context, uri string) (ipc.ImportResult, error) {
	r, err := s.d.ipc.ImportInvite(ctx, strings.TrimSpace(uri))
	return r, toErr(err)
}

func (s *AppService) Servers(ctx context.Context) ([]ipc.Server, error) {
	sv, err := s.d.ipc.Servers(ctx)
	if sv == nil {
		sv = []ipc.Server{}
	}
	return sv, toErr(err)
}

func (s *AppService) ProbeServers(ctx context.Context) ([]ipc.Server, error) {
	sv, err := s.d.ipc.ProbeServers(ctx)
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

func (s *AppService) Ads(ctx context.Context, slot, relayID string) ([]ipc.Ad, error) {
	ads, err := s.d.ipc.Ads(ctx, slot, relayID)
	if ads == nil {
		ads = []ipc.Ad{}
	}
	return ads, toErr(err)
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

// InstallHelper installs the helper service behind one OS admin prompt,
// then waits until the helper answers IPC with a matching protocol version.
func (s *AppService) InstallHelper() error {
	owner, err := currentOwner()
	if err != nil {
		return toErr(err)
	}
	if err := runElevated("service", "install", "-owner", owner); err != nil {
		return toErr(err)
	}
	s.d.watchNow()
	deadline := time.Now().Add(25 * time.Second)
	var last error
	for time.Now().Before(deadline) {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		_, err := s.d.ipc.Status(ctx)
		cancel()
		if err == nil {
			s.d.watchNow()
			return nil
		}
		last = err
		var ie *ipc.Error
		if errors.As(err, &ie) && ie.Code == ipc.CodeVersion {
			last = err
		}
		time.Sleep(400 * time.Millisecond)
		s.d.watchNow()
	}
	if last != nil {
		return toErr(last)
	}
	return &helperError{Code: codeHelperDown, Message: "helper did not become ready after install"}
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

// TakeInvite returns the outcome of a dv:// link the app was opened with
// before the frontend was listening, once.
func (s *AppService) TakeInvite() *InviteResult { return s.d.takeInvite() }
