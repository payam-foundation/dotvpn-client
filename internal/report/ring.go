// SPDX-License-Identifier: AGPL-3.0-only

package report

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

const (
	// RingMaxAge is how long log lines are kept.
	RingMaxAge = 60 * time.Minute
	// RingMaxBytes caps the ring's memory.
	RingMaxBytes = 512 << 10
	maxLine      = 2 << 10
)

type entry struct {
	at   time.Time
	line string
}

// Ring keeps scrubbed log lines in RAM only. It is an io.Writer for a
// slog.TextHandler; see Handler.
type Ring struct {
	mu      sync.Mutex
	entries []entry
	size    int
	started time.Time
	maxAge  time.Duration
	maxSize int
	now     func() time.Time
}

// NewRing returns an empty ring with the default caps.
func NewRing() *Ring {
	return &Ring{started: time.Now(), maxAge: RingMaxAge, maxSize: RingMaxBytes, now: time.Now}
}

// Started is when the ring began collecting (process start).
func (r *Ring) Started() time.Time { return r.started }

// Write stores one scrubbed record. slog.TextHandler issues one Write per record.
func (r *Ring) Write(p []byte) (int, error) {
	n := len(p)
	line := Scrub(strings.TrimRight(string(p), "\n"))
	if len(line) > maxLine {
		line = line[:maxLine]
		for !utf8.ValidString(line) {
			line = line[:len(line)-1]
		}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	at := r.now()
	r.entries = append(r.entries, entry{at, line})
	r.size += len(line)
	r.evict(at)
	return n, nil
}

func (r *Ring) evict(now time.Time) {
	cut := 0
	for cut < len(r.entries) && (now.Sub(r.entries[cut].at) > r.maxAge || r.size > r.maxSize) {
		r.size -= len(r.entries[cut].line)
		cut++
	}
	if cut > 0 {
		r.entries = append(r.entries[:0], r.entries[cut:]...)
	}
}

// Snapshot returns the lines recorded within window, oldest first.
func (r *Ring) Snapshot(window time.Duration) []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := r.now()
	r.evict(now)
	var out []string
	for _, e := range r.entries {
		if now.Sub(e.at) <= window {
			out = append(out, e.line)
		}
	}
	return out
}

// LogRing is a slog.Handler that records Info and above into a Ring.
type LogRing struct {
	slog.Handler
	ring *Ring
}

// Handler returns a handler feeding r. Fan it out with slog.NewMultiHandler.
func (r *Ring) Handler() *LogRing {
	return &LogRing{
		Handler: slog.NewTextHandler(r, &slog.HandlerOptions{Level: slog.LevelInfo}),
		ring:    r,
	}
}

// Enabled implements slog.Handler.
func (l *LogRing) Enabled(ctx context.Context, lv slog.Level) bool {
	return l.Handler.Enabled(ctx, lv)
}

// WithAttrs implements slog.Handler.
func (l *LogRing) WithAttrs(a []slog.Attr) slog.Handler {
	return &LogRing{Handler: l.Handler.WithAttrs(a), ring: l.ring}
}

// WithGroup implements slog.Handler.
func (l *LogRing) WithGroup(name string) slog.Handler {
	return &LogRing{Handler: l.Handler.WithGroup(name), ring: l.ring}
}

// Logs is the process-wide ring. The helper (desktop) or the mobile engine
// owns it; both run one session per process.
var Logs = NewRing()

// Tee returns a logger that writes to h as before and records Info and above,
// scrubbed, into Logs.
func Tee(h slog.Handler) *slog.Logger {
	return slog.New(slog.NewMultiHandler(h, Logs.Handler()))
}
