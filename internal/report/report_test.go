// SPDX-License-Identifier: AGPL-3.0-only

package report

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/payam-foundation/dotvpn-client/internal/diag"
	"github.com/payam-foundation/log-decypher/armor"
	"github.com/payam-foundation/log-decypher/envelope"
	"github.com/payam-foundation/log-decypher/seal"
)

// akKey is split so secret scanners do not flag the fixture.
const tail = "_0123456789abcdef"

const akKey = "ak" + tail

func TestScrub(t *testing.T) {
	leaks := []string{
		"203.0.113.7", "203.0.113.7:443", "2001:db8::1", "fe80::1ff:fe23:4567:890a",
		"relay.example.com", "relay.example.com:8443", "sub.dotvpn.org",
		"dv://" + akKey, akKey, "jt" + tail, "ns" + tail,
		"ey" + "JhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkw.SflKxwRJSMeKKF2QT4fwpMeJf36",
		"Bearer abcdef0123456789", "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA",
	}
	for _, l := range leaks {
		got := Scrub("dial failed to " + l + " retry")
		if strings.Contains(got, l) {
			t.Errorf("leak %q in %q", l, got)
		}
	}
	for _, keep := range []string{"time 17:45:00 ok", "latency=12.5ms", "state=connected attempt=3"} {
		if got := Scrub(keep); got != keep {
			t.Errorf("Scrub(%q) = %q", keep, got)
		}
	}
}

func TestRingScrubsAtRecordTime(t *testing.T) {
	r := NewRing()
	l := slog.New(r.Handler()).With("peer", "198.51.100.9:1234")
	l.Info("connect", "host", "a.example.org", "key", akKey)
	l.Debug("noise")
	got := r.Snapshot(time.Hour)
	if len(got) != 1 {
		t.Fatalf("lines = %v", got)
	}
	for _, bad := range []string{"198.51.100.9", "example.org", "ak_0123"} {
		if strings.Contains(got[0], bad) {
			t.Errorf("leak %q in %q", bad, got[0])
		}
	}
}

func TestRingEviction(t *testing.T) {
	r := NewRing()
	clock := time.Now()
	r.now = func() time.Time { return clock }
	r.Write([]byte("old\n"))
	clock = clock.Add(61 * time.Minute)
	r.Write([]byte("new\n"))
	if got := r.Snapshot(time.Hour); len(got) != 1 || got[0] != "new" {
		t.Fatalf("age eviction: %v", got)
	}
	clock = clock.Add(time.Second)
	if got := r.Snapshot(time.Second / 2); len(got) != 0 {
		t.Fatalf("window: %v", got)
	}

	r = NewRing()
	r.maxSize = 100
	for i := 0; i < 50; i++ {
		r.Write([]byte(strings.Repeat("x", 20) + "\n"))
	}
	if r.size > 100 || len(r.Snapshot(time.Hour)) != 5 {
		t.Fatalf("byte cap: size=%d", r.size)
	}
}

func TestRingConcurrent(t *testing.T) {
	r := NewRing()
	l := slog.New(r.Handler())
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				l.Info("tick", "j", j)
				r.Snapshot(time.Minute)
			}
		}()
	}
	wg.Wait()
	if !l.Handler().Enabled(context.Background(), slog.LevelInfo) {
		t.Fatal("info disabled")
	}
}

func TestPublicKeyParses(t *testing.T) {
	if _, err := seal.ParsePublic(publicKeys[0]); err != nil {
		t.Fatal(err)
	}
}

func TestBuild(t *testing.T) {
	priv, err := seal.Generate()
	if err != nil {
		t.Fatal(err)
	}
	r := NewRing()
	l := slog.New(r.Handler())
	l.Info("dial", "to", "203.0.113.50:443", "acct", akKey, "dom", "secret.example.net")
	l.Info("session up", "n", 1)

	res, err := Build(Options{
		Window: 30 * time.Minute,
		Note:   "it fails on wifi",
		Meta:   map[string]string{"model": "Pixel"},
		Logs:   r,
		Events: diag.Bundle{Platform: "desktop", AppVersion: "1.2.3", Events: []diag.Event{{Code: diag.CodeStart}}},
		Key:    priv.Public().String(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.ID) != idLen || FileName(res.ID) != "r-"+res.ID+".dat" {
		t.Fatalf("id %q", res.ID)
	}
	blob, err := armor.Decode(res.Armor)
	if err != nil || string(blob) != string(res.Blob) {
		t.Fatalf("armor round trip: %v", err)
	}
	plain, err := seal.Open(priv, project, res.Blob)
	if err != nil {
		t.Fatal(err)
	}
	env, err := envelope.Unmarshal(plain)
	if err != nil {
		t.Fatal(err)
	}
	txt := string(plain)
	for _, bad := range []string{"203.0.113.50", akKey, "secret.example.net"} {
		if strings.Contains(txt, bad) {
			t.Errorf("leak %q", bad)
		}
	}
	if !strings.Contains(txt, "session up") || env.Meta["window_min"] != "30" || env.Meta["model"] != "Pixel" ||
		env.Meta["app_version"] != "1.2.3" || env.Meta["ring_started"] == "" {
		t.Fatalf("missing content: %s", txt)
	}
	if !strings.Contains(res.Preview, "it fails on wifi") || strings.Contains(res.Preview, "203.0.113.50") {
		t.Fatalf("preview: %s", res.Preview)
	}
	if _, err := seal.Open(priv, "other", res.Blob); err == nil {
		t.Fatal("opened with wrong project")
	}
}

func TestBuildPreviewOnly(t *testing.T) {
	res, err := Build(Options{PreviewOnly: true})
	if err != nil || res.Blob != nil || res.Preview == "" {
		t.Fatalf("%v %+v", err, res)
	}
}

func TestBuildNoteCap(t *testing.T) {
	res, err := Build(Options{PreviewOnly: true, Note: strings.Repeat("é", 2000)})
	if err != nil || strings.Count(res.Preview, "é") > maxNote/2 {
		t.Fatalf("note not capped: %v", err)
	}
}
