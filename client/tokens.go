package client

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/payam-foundation/dotvpn-wire/discovery"
)

// Refresher is the part of discovery.Client the manager needs.
type Refresher interface {
	Refresh(ctx context.Context) (discovery.Result, error)
}

// TokenManager keeps a fresh bearer token and endpoint list. Token returns
// the current token; Run refreshes before expiry and on demand (after a node
// rejects the token, or when the local clock is wrong).
type TokenManager struct {
	Source Refresher
	// OnEndpoints receives every new endpoint list (Engine.SetEndpoints).
	OnEndpoints func(discovery.Result)
	Log         *slog.Logger

	mu      sync.Mutex
	token   string
	expires time.Time
	kick    chan struct{}
	once    sync.Once
}

func (m *TokenManager) init() { m.once.Do(func() { m.kick = make(chan struct{}, 1) }) }

// Token implements transport.TokenSource.
func (m *TokenManager) Token() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.token
}

// Refresh requests an immediate refresh without blocking.
func (m *TokenManager) Refresh() {
	m.init()
	select {
	case m.kick <- struct{}{}:
	default:
	}
}

// Start performs the first refresh synchronously, so the caller either has a
// token (or at least offline endpoints) or a clear error, then keeps
// refreshing in the background until ctx ends.
func (m *TokenManager) Start(ctx context.Context) error {
	m.init()
	if m.Log == nil {
		m.Log = slog.Default()
	}
	err := m.refresh(ctx)
	if m.Token() == "" {
		if err == nil {
			err = errors.New("client: control plane returned no token")
		}
		return err
	}
	go m.loop(ctx)
	return nil
}

func (m *TokenManager) refresh(ctx context.Context) error {
	r, err := m.Source.Refresh(ctx)
	if r.Token != "" {
		m.mu.Lock()
		m.token, m.expires = r.Token, r.Expires.Add(-r.Skew)
		m.mu.Unlock()
	}
	if len(r.Endpoints) > 0 && m.OnEndpoints != nil {
		m.OnEndpoints(r)
	}
	return err
}

func (m *TokenManager) loop(ctx context.Context) {
	for {
		m.mu.Lock()
		wait := time.Until(m.expires) * 2 / 3
		m.mu.Unlock()
		// Never spin: a failed refresh (expires unchanged) retries in 20s.
		wait = min(max(wait, 20*time.Second), 10*time.Minute)
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		case <-m.kick:
		}
		if err := m.refresh(ctx); err != nil {
			m.Log.Warn("token refresh failed", "err", err)
		}
	}
}
