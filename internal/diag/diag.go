// SPDX-License-Identifier: AGPL-3.0-only

// Package diag is the L0/L1 diagnostics seam: a quiet in-memory event ring
// whose snapshot goes into the sealed report (internal/report).
package diag

import (
	"runtime"
	"sync"
	"time"
)

const ringCap = 64

// Event codes are stable, non-fingerprint identifiers (no host:port / carrier names).
type Code string

const (
	CodeReady         Code = "ready"
	CodeStart         Code = "start"
	CodeStop          Code = "stop"
	CodeConnectOK     Code = "connect_ok"
	CodeConnectFail   Code = "connect_fail"
	CodeRungFail      Code = "rung_fail"
	CodeHelperRestart Code = "helper_restart"
	CodeFail          Code = "fail"
)

// Event is one ring entry.
type Event struct {
	TS         int64  `json:"ts"`
	Code       Code   `json:"code"`
	EndpointID string `json:"endpoint_id,omitempty"` // hashed id only
	LatencyMS  int    `json:"latency_ms,omitempty"`  // bucketed
	Detail     string `json:"detail,omitempty"`      // short fail code, never addresses
}

// Bundle is the scrubbed snapshot for L1.
type Bundle struct {
	Platform   string         `json:"platform"`
	AppVersion string         `json:"app_version"`
	OS         string         `json:"os"`
	Arch       string         `json:"arch"`
	State      string         `json:"state,omitempty"`
	Events     []Event        `json:"events"`
	FailCounts map[string]int `json:"fail_counts,omitempty"`
}

// Collector records coded events.
type Collector interface {
	Note(Event)
	Snapshot() Bundle
}

// Ring is an in-memory Collector.
type Ring struct {
	mu         sync.Mutex
	events     []Event
	platform   string
	appVersion string
	state      string
	fails      map[string]int
}

// NewRing builds a Collector. platform is desktop|android|cli.
func NewRing(platform, appVersion string) *Ring {
	return &Ring{
		platform:   platform,
		appVersion: appVersion,
		fails:      map[string]int{},
	}
}

// SetState updates the coarse connect state string (e.g. "connected").
func (r *Ring) SetState(state string) {
	r.mu.Lock()
	r.state = state
	r.mu.Unlock()
}

// SetVersion updates the app version embedded in snapshots.
func (r *Ring) SetVersion(v string) {
	r.mu.Lock()
	r.appVersion = v
	r.mu.Unlock()
}

// Note appends an event, dropping the oldest when full.
func (r *Ring) Note(e Event) {
	if e.TS == 0 {
		e.TS = time.Now().Unix()
	}
	e.Detail = scrubLine(e.Detail)
	e.EndpointID = scrubLine(e.EndpointID)
	e.LatencyMS = bucketLatency(e.LatencyMS)
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.events) >= ringCap {
		copy(r.events, r.events[1:])
		r.events[len(r.events)-1] = e
		r.events = r.events[:ringCap]
	} else {
		r.events = append(r.events, e)
	}
	if e.Code == CodeConnectFail || e.Code == CodeRungFail || e.Code == CodeFail {
		key := string(e.Code)
		if e.Detail != "" {
			key += ":" + e.Detail
		}
		r.fails[key]++
	}
}

// Snapshot returns a scrubbed copy.
func (r *Ring) Snapshot() Bundle {
	r.mu.Lock()
	defer r.mu.Unlock()
	ev := make([]Event, len(r.events))
	copy(ev, r.events)
	fails := map[string]int{}
	for k, v := range r.fails {
		fails[k] = v
	}
	return Bundle{
		Platform:   r.platform,
		AppVersion: r.appVersion,
		OS:         runtime.GOOS,
		Arch:       runtime.GOARCH,
		State:      r.state,
		Events:     ev,
		FailCounts: fails,
	}
}

func bucketLatency(ms int) int {
	switch {
	case ms <= 0:
		return 0
	case ms < 100:
		return 50
	case ms < 300:
		return 200
	case ms < 1000:
		return 500
	case ms < 3000:
		return 2000
	default:
		return 5000
	}
}
