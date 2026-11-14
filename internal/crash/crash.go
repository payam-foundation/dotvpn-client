// SPDX-License-Identifier: AGPL-3.0-only

// Package crash is the L2 opt-in crash reporter seam (Sentry).
// Callers only use Reporter; DSN/Init/BeforeSend stay inside.
package crash

import (
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/getsentry/sentry-go"
)

// DSN is set at link time via -ldflags "-X …/crash.DSN=…" for release builds.
var DSN string

// Reporter enables or disables crash reporting.
type Reporter interface {
	SetEnabled(bool) error
	Enabled() bool
}

// Options configure a Sentry-backed reporter.
type Options struct {
	DSN         string // empty uses package DSN or SENTRY_DSN / SENTRY_DSN_DESKTOP
	Release     string
	Environment string // default prod
	// Persist stores the opt-in flag; nil keeps it in memory only.
	Persist func(enabled bool) error
	Load    func() bool
}

type sentryReporter struct {
	mu      sync.Mutex
	enabled bool
	opts    Options
	inited  bool
}

// New returns a Reporter. It does not Init Sentry until SetEnabled(true).
func New(opts Options) Reporter {
	if opts.DSN == "" {
		opts.DSN = DSN
	}
	if opts.DSN == "" {
		opts.DSN = os.Getenv("SENTRY_DSN_DESKTOP")
	}
	if opts.DSN == "" {
		opts.DSN = os.Getenv("SENTRY_DSN")
	}
	if opts.Environment == "" {
		opts.Environment = "prod"
	}
	r := &sentryReporter{opts: opts}
	if opts.Load != nil {
		r.enabled = opts.Load()
		if r.enabled {
			_ = r.enableLocked()
		}
	}
	return r
}

func (r *sentryReporter) Enabled() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.enabled
}

func (r *sentryReporter) SetEnabled(on bool) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if on == r.enabled && (!on || r.inited) {
		return nil
	}
	if !on {
		r.enabled = false
		if r.inited {
			sentry.Flush(2 * time.Second)
			_ = sentry.Init(sentry.ClientOptions{Dsn: ""})
			r.inited = false
		}
		if r.opts.Persist != nil {
			return r.opts.Persist(false)
		}
		return nil
	}
	if err := r.enableLocked(); err != nil {
		return err
	}
	if r.opts.Persist != nil {
		return r.opts.Persist(true)
	}
	return nil
}

func (r *sentryReporter) enableLocked() error {
	if r.opts.DSN == "" {
		return errNoDSN
	}
	err := sentry.Init(sentry.ClientOptions{
		Dsn:              r.opts.DSN,
		Release:          r.opts.Release,
		Environment:      r.opts.Environment,
		SendDefaultPII:   false,
		TracesSampleRate: 0,
		BeforeSend:       beforeSend,
	})
	if err != nil {
		return err
	}
	r.inited = true
	r.enabled = true
	return nil
}

// Recover captures a panic into Sentry when reporting is enabled.
// Callers with an active Reporter should `defer crash.Recover()`.
func Recover() {
	sentry.Recover()
	sentry.Flush(2 * time.Second)
}

// CaptureMessage sends a non-fatal event when a client is initialized
// (opt-in enabled). No-op otherwise. Used for smoke checks and rare marks.
func CaptureMessage(msg string) {
	sentry.CaptureMessage(msg)
	sentry.Flush(2 * time.Second)
}

var errNoDSN = errString("crash: dsn not configured")

type errString string

func (e errString) Error() string { return string(e) }

var (
	reIPv4   = regexp.MustCompile(`\b(?:\d{1,3}\.){3}\d{1,3}\b`)
	reBearer = regexp.MustCompile(`(?i)bearer\s+[a-z0-9._\-]{8,}`)
	reInvite = regexp.MustCompile(`(?i)dv://[^\s"']+`)
)

func beforeSend(event *sentry.Event, _ *sentry.EventHint) *sentry.Event {
	if event == nil {
		return nil
	}
	scrub := func(s string) string {
		s = reInvite.ReplaceAllString(s, "[invite]")
		s = reBearer.ReplaceAllString(s, "[token]")
		s = reIPv4.ReplaceAllString(s, "[ip]")
		return s
	}
	event.Message = scrub(event.Message)
	for i := range event.Exception {
		event.Exception[i].Value = scrub(event.Exception[i].Value)
	}
	if strings.Contains(strings.ToLower(event.Message), "bearer") || strings.Contains(event.Message, "dv://") {
		return nil
	}
	for _, ex := range event.Exception {
		if strings.Contains(strings.ToLower(ex.Value), "bearer") || strings.Contains(ex.Value, "dv://") {
			return nil
		}
	}
	event.User = sentry.User{} // no account id
	event.Request = nil
	event.ServerName = "" // no hostname fingerprint
	return event
}
