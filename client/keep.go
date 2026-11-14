package client

import (
	"context"
	"math/rand/v2"
	"time"
)

// Hooks observe Keep. Every field is optional; they run on Keep's goroutine
// and must not block.
type Hooks struct {
	// OnConnecting runs before each connect attempt.
	OnConnecting func()
	// OnFail runs when an attempt fails; Keep retries after retryIn, or
	// sooner if the network changes.
	OnFail func(err error, retryIn time.Duration)
	// OnUp runs once a session is established.
	OnUp func(Info)
	// OnDrop runs when an established session is lost, before the reconnect.
	OnDrop func()
}

const (
	minBackoff = time.Second
	maxBackoff = 30 * time.Second
)

// Keep holds the engine connected until ctx ends: it connects, retries with a
// 1s..30s backoff on failure, and reconnects as soon as the session drops or
// NetworkChanged reports a roam.
func (e *Engine) Keep(ctx context.Context, h Hooks) {
	call := func(f func()) {
		if f != nil {
			f()
		}
	}
	backoff := minBackoff
	for ctx.Err() == nil {
		select { // a wake raised before this attempt is already served by it
		case <-e.wake:
		default:
		}
		call(h.OnConnecting)
		gen := e.generation()
		if err := e.Connect(ctx); err != nil {
			if ctx.Err() != nil {
				return
			}
			if e.generation() != gen { // the roam aborted this attempt; not a failure
				backoff = minBackoff
				continue
			}
			if h.OnFail != nil {
				h.OnFail(err, backoff)
			}
			t := time.NewTimer(backoff)
			select {
			case <-ctx.Done():
				t.Stop()
				return
			case <-e.wake: // a roam makes the old backoff meaningless
				t.Stop()
				backoff = minBackoff
			case <-t.C:
				backoff = min(backoff*2, maxBackoff)
			}
			continue
		}
		backoff = minBackoff
		if h.OnUp != nil {
			h.OnUp(e.Info())
		}
		// ponytail: a session that dies silently raises no wake, so Alive is
		// also polled every 2s. Add an Alive channel to transport.Session if
		// that latency matters.
		wctx, stopWarm := context.WithCancel(ctx)
		go e.warmLater(wctx)
		tick := time.NewTicker(2 * time.Second)
		for e.Info().Connected {
			select {
			case <-ctx.Done():
				tick.Stop()
				stopWarm()
				return
			case <-e.wake:
			case <-tick.C:
			}
		}
		tick.Stop()
		stopWarm()
		call(h.OnDrop)
	}
}

// warmLater runs Warm once, 2-5s after a connect so it does not compete with
// the tunnel coming up. ctx ends it with the session.
func (e *Engine) warmLater(ctx context.Context) {
	t := time.NewTimer(2*time.Second + rand.N(3*time.Second))
	defer t.Stop()
	select {
	case <-ctx.Done():
	case <-t.C:
		e.Warm(ctx)
	}
}
