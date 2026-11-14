// SPDX-License-Identifier: AGPL-3.0-only

package report

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"runtime"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/payam-foundation/dotvpn-client/internal/diag"
	"github.com/payam-foundation/log-decypher/armor"
	"github.com/payam-foundation/log-decypher/envelope"
	"github.com/payam-foundation/log-decypher/seal"
)

const (
	maxNote   = 1 << 10
	maxMeta   = 200
	idLen     = 8
	idAlpha   = "23456789ABCDEFGHJKMNPQRSTUVWXYZ" // no 0/O/1/I/L
	idLimit   = 256 - 256%len(idAlpha)
	fileStart = "r-"
)

// Options configures Build.
type Options struct {
	Window time.Duration     // log window; default 60 min
	Note   string            // user text, max 1 KB, sealed unscrubbed
	Meta   map[string]string // extra platform metadata (SDK, model, network)
	Logs   *Ring             // nil = no log section
	Events diag.Bundle       // coded events from diag.Ring
	// PreviewOnly skips sealing; Result carries only ID and Preview.
	PreviewOnly bool
	// Key overrides the active public key (tests).
	Key string
}

// Result is a built report.
type Result struct {
	ID      string
	Blob    []byte // sealed, opaque
	Armor   string // armored Blob for pasting into chats
	Preview string // plaintext the user can inspect before sending
}

// FileName is the neutral attachment name.
func FileName(id string) string { return fileStart + id + ".dat" }

func newID() (string, error) {
	out := make([]byte, 0, idLen)
	var b [16]byte
	for len(out) < idLen {
		if _, err := rand.Read(b[:]); err != nil {
			return "", err
		}
		for _, c := range b {
			if int(c) < idLimit && len(out) < idLen {
				out = append(out, idAlpha[int(c)%len(idAlpha)])
			}
		}
	}
	return string(out), nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	s = s[:n]
	for !utf8.ValidString(s) {
		s = s[:len(s)-1]
	}
	return s
}

// Build assembles the envelope and seals it. No crypto lives here.
func Build(o Options) (Result, error) {
	if o.Window <= 0 {
		o.Window = RingMaxAge
	}
	id, err := newID()
	if err != nil {
		return Result{}, err
	}
	now := time.Now().UTC()
	_, off := time.Now().Zone()
	meta := map[string]string{
		"app_version": o.Events.AppVersion,
		"platform":    o.Events.Platform,
		"state":       o.Events.State,
		"os":          runtime.GOOS,
		"arch":        runtime.GOARCH,
		"window_min":  fmt.Sprint(int(o.Window / time.Minute)),
		"tz_offset_s": fmt.Sprint(off),
	}
	if o.Logs != nil {
		meta["ring_started"] = o.Logs.Started().UTC().Format(time.RFC3339)
	}
	for k, v := range o.Meta {
		meta[truncate(k, 64)] = truncate(v, maxMeta)
	}
	for k, v := range meta {
		if v == "" {
			delete(meta, k)
		}
	}
	ev, err := json.Marshal(o.Events)
	if err != nil {
		return Result{}, err
	}
	env := &envelope.Envelope{
		Project: project, Kind: "report", ID: id, Created: now, Meta: meta,
		Sections: []envelope.Section{{Name: "events", MIME: "application/json", Data: string(ev)}},
	}
	if o.Logs != nil {
		env.Sections = append(env.Sections, envelope.Section{
			Name: "log", MIME: "text/plain", Data: strings.Join(o.Logs.Snapshot(o.Window), "\n"),
		})
	}
	if note := strings.TrimSpace(o.Note); note != "" {
		env.Sections = append(env.Sections, envelope.Section{
			Name: "note", MIME: "text/plain", Data: truncate(note, maxNote),
		})
	}
	res := Result{ID: id, Preview: preview(env)}
	if o.PreviewOnly {
		return res, nil
	}
	plain, err := envelope.Marshal(env)
	if err != nil {
		return Result{}, err
	}
	key := o.Key
	if key == "" {
		key = publicKeys[0]
	}
	pub, err := seal.ParsePublic(key)
	if err != nil {
		return Result{}, err
	}
	res.Blob, err = seal.Seal(pub, project, plain)
	if err != nil {
		return Result{}, err
	}
	res.Armor = armor.Encode(res.Blob)
	return res, nil
}

func preview(e *envelope.Envelope) string {
	var b strings.Builder
	fmt.Fprintf(&b, "id: %s\ncreated: %s\n", e.ID, e.Created.Format(time.RFC3339))
	keys := make([]string, 0, len(e.Meta))
	for k := range e.Meta {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Fprintf(&b, "%s: %s\n", k, e.Meta[k])
	}
	for _, s := range e.Sections {
		fmt.Fprintf(&b, "\n== %s ==\n%s\n", s.Name, s.Data)
	}
	return b.String()
}
