// SPDX-License-Identifier: AGPL-3.0-only

package client

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"time"

	"github.com/payam-foundation/dotvpn-wire/transport"
)

// Info describes the live session.
type Info struct {
	Connected bool
	Endpoint  transport.Endpoint
	// Latency is how long the winning endpoint took to dial and pass the
	// data-plane probe.
	Latency time.Duration
	// Since is when the session was established.
	Since time.Time
}

// Health is one rung of the ladder as the last attempts left it.
type Health struct {
	// ID names the endpoint for EngineConfig.Pin. It is a hash, so it does not
	// carry the node address.
	ID       string
	Endpoint transport.Endpoint
	// Active marks the endpoint carrying the live session.
	Active bool
	// Latency is the last successful dial plus probe; zero if never tried or
	// the last attempt failed.
	Latency time.Duration
	// Err is the last failure, empty after a success.
	Err string
	// Cooldown is when a failed endpoint becomes preferred again; zero if it
	// is not cooling down.
	Cooldown time.Time
}

type epHealth struct {
	latency time.Duration
	err     string
	at      time.Time
	gen     uint64 // netGen the reading was taken under
}

const (
	// latencyTTL is how long a reading orders the ladder.
	latencyTTL = 10 * time.Minute
	// warmMax and warmConc bound the background probe.
	warmMax  = 4
	warmConc = 2
)

// fresh reports whether h is a success from this network generation, recent
// enough to rank by.
func (h epHealth) fresh(gen uint64, now time.Time) bool {
	return h.err == "" && h.latency > 0 && h.gen == gen && now.Sub(h.at) < latencyTTL
}

// Info returns the live session, or Connected=false.
func (e *Engine) Info() Info {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.sess == nil || !e.sess.Alive() {
		return Info{}
	}
	return Info{Connected: true, Endpoint: e.cur, Latency: e.health[key(e.cur)].latency, Since: e.since}
}

// Health lists the ladder in display order (including cool endpoints).
func (e *Engine) Health() []Health {
	order := e.order(e.expand(context.Background(), false), false)
	e.mu.Lock()
	defer e.mu.Unlock()
	live := e.sess != nil && e.sess.Alive()
	now := time.Now()
	out := make([]Health, 0, len(order))
	for _, ep := range order {
		k := key(ep)
		h := Health{ID: EndpointID(ep), Endpoint: ep, Active: live && k == key(e.cur), Latency: e.health[k].latency, Err: e.health[k].err}
		if until := e.cooldown[k]; now.Before(until) {
			h.Cooldown = until
		}
		out = append(out, h)
	}
	return out
}

// record stores a reading taken under network generation gen.
func (e *Engine) record(ep transport.Endpoint, d time.Duration, err error, gen uint64) {
	h := epHealth{latency: d, at: time.Now(), gen: gen}
	if err != nil {
		h = epHealth{err: err.Error(), at: h.at, gen: gen}
	}
	e.mu.Lock()
	e.health[key(ep)] = h
	e.mu.Unlock()
}

// ProbeAll dials each ladder rung once (non-recurring) and records latency.
// Sessions from the probe are closed; the live session is left alone.
func (e *Engine) ProbeAll(ctx context.Context) {
	eps := e.expand(ctx, false)
	gen := e.generation()
	e.recordProbe(eps, Probe(ctx, eps, e.cfg.Dialers, e.cfg.Token, e.cfg.ProbeTarget, 4), gen)
}

// Warm times the next-best rungs with handshake-only dials so a failover on
// this network picks the fastest one directly. It probes at most warmMax
// stale, non-cool rungs other than the live one, warmConc at a time, and does
// nothing when they are all fresh.
//
// ponytail: handshake time and the data-plane latency recorded by a real
// connect share one scale. Split them if the ranking proves skewed.
func (e *Engine) Warm(ctx context.Context) {
	gen := e.generation()
	now := time.Now()
	e.mu.Lock()
	live := ""
	if e.sess != nil && e.sess.Alive() {
		live = key(e.cur)
	}
	e.mu.Unlock()
	var eps []transport.Endpoint
	for _, ep := range e.ladder() {
		e.mu.Lock()
		skip := key(ep) == live || e.health[key(ep)].fresh(gen, now)
		e.mu.Unlock()
		if !skip && len(eps) < warmMax {
			eps = append(eps, ep)
		}
	}
	if len(eps) == 0 {
		return
	}
	ranked := Probe(ctx, eps, e.cfg.Dialers, e.cfg.Token, "", warmConc)
	if ctx.Err() != nil { // cut short: the misses are not failures
		return
	}
	e.recordProbe(eps, ranked, gen)
}

// recordProbe stores a probe round: ranked rungs get their latency, the rest
// are marked failed.
func (e *Engine) recordProbe(eps []transport.Endpoint, ranked []Ranked, gen uint64) {
	ok := map[string]bool{}
	for _, r := range ranked {
		e.record(r.Endpoint, r.Total(), nil, gen)
		ok[key(r.Endpoint)] = true
	}
	for _, ep := range eps {
		if !ok[key(ep)] {
			e.record(ep, 0, errProbeFailed, gen)
		}
	}
}

var errProbeFailed = errString("probe failed")

type errString string

func (e errString) Error() string { return string(e) }

// notify wakes Keep and reports the new Info to OnState.
func (e *Engine) notify() {
	select {
	case e.wake <- struct{}{}:
	default:
	}
	if e.cfg.OnState != nil {
		e.cfg.OnState(e.Info())
	}
}

// EndpointID is the stable, address-free name of an endpoint.
func EndpointID(ep transport.Endpoint) string {
	sum := sha256.Sum256([]byte(key(ep)))
	return hex.EncodeToString(sum[:6])
}

func pinned(eps []transport.Endpoint, id string) []transport.Endpoint {
	var out []transport.Endpoint
	for _, ep := range eps {
		if EndpointID(ep) == id {
			out = append(out, ep)
		}
	}
	return out
}
