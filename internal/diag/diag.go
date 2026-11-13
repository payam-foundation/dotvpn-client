// Package diag is the L0/L1 diagnostics seam: a quiet in-memory event ring
// and scrubbed submit path to control (or a browser GitHub prefill).
package diag

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"runtime"
	"strings"
	"sync"
	"time"
)

const ringCap = 64

// Event codes are stable, non-fingerprint identifiers (no host:port / carrier names).
type Code string

const (
	CodeReady        Code = "ready"
	CodeStart        Code = "start"
	CodeStop         Code = "stop"
	CodeConnectOK    Code = "connect_ok"
	CodeConnectFail  Code = "connect_fail"
	CodeRungFail     Code = "rung_fail"
	CodeHelperRestart Code = "helper_restart"
	CodeFail         Code = "fail"
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

// Result is what Submit returns.
type Result struct {
	IssueURL string `json:"issue_url"`
	Via      string `json:"via"` // "control" | "browser"
}

// Collector records coded events.
type Collector interface {
	Note(Event)
	Snapshot() Bundle
}

// Submitter sends a bundle (control first, browser fallback).
type Submitter interface {
	Submit(ctx context.Context, b Bundle) (Result, error)
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

// ClientSubmit posts to control; on failure returns a browser prefill URL.
type ClientSubmit struct {
	ControlURL string // e.g. https://ctrl.example/api
	AccountKey string
	HTTP       *http.Client
	// OpenURL opens a browser; nil means return Via=browser without opening.
	OpenURL func(string) error
}

func (c *ClientSubmit) Submit(ctx context.Context, b Bundle) (Result, error) {
	payload := map[string]any{
		"platform":    b.Platform,
		"app_version": b.AppVersion,
		"bundle":      b,
	}
	body, _ := json.Marshal(payload)
	base := strings.TrimRight(c.ControlURL, "/")
	if base != "" && c.AccountKey != "" {
		cli := c.HTTP
		if cli == nil {
			cli = &http.Client{Timeout: 15 * time.Second}
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/v1/support/report", bytes.NewReader(body))
		if err == nil {
			req.Header.Set("Authorization", "Bearer "+c.AccountKey)
			req.Header.Set("Content-Type", "application/json")
			resp, err := cli.Do(req)
			if err == nil {
				defer resp.Body.Close()
				raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
				if resp.StatusCode >= 200 && resp.StatusCode < 300 {
					var out struct {
						IssueURL string `json:"issue_url"`
					}
					if json.Unmarshal(raw, &out) == nil && out.IssueURL != "" {
						return Result{IssueURL: out.IssueURL, Via: "control"}, nil
					}
				}
			}
		}
	}
	u := BrowserIssueURL(b)
	if c.OpenURL != nil {
		_ = c.OpenURL(u)
	}
	return Result{IssueURL: u, Via: "browser"}, nil
}

// BrowserIssueURL builds a GitHub new-issue link. Issue forms ignore body=,
// so we only set a short title; the user pastes the bundle from the app if needed.
func BrowserIssueURL(b Bundle) string {
	repo := "dotvpn-client"
	if b.Platform == "android" {
		repo = "dotvpn-android"
	}
	title := fmt.Sprintf("diagnostics: %s %s", b.Platform, strings.TrimSpace(b.AppVersion))
	if strings.TrimSpace(title) == "diagnostics:" {
		title = "diagnostics: report"
	}
	q := url.Values{}
	q.Set("title", truncate(title, 120))
	q.Set("labels", "diagnostics")
	return fmt.Sprintf("https://github.com/payam-foundation/%s/issues/new?%s", repo, q.Encode())
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
