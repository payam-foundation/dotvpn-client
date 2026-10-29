package client

import (
	"encoding/json"
	"errors"
	"os"
	"sync"
	"time"

	"github.com/payam-foundation/dotvpn-wire/atomicfile"
)

// State is what survives a restart: which endpoint worked last. It holds no
// secrets, only a routing hint, so a stolen file reveals a node address the
// config already names.
type State struct {
	// Key identifies the endpoint (Kind, Addr, Host, Path); Addr is the exact
	// dialled address, which for a clean-IP endpoint is the chosen IP.
	Key  string    `json:"key"`
	Addr string    `json:"addr"`
	At   time.Time `json:"at"`
}

// stateMaxAge bounds how long a remembered endpoint is trusted.
const stateMaxAge = 7 * 24 * time.Hour

// StateStore persists State. Load returns the zero State when nothing usable
// is stored; it never fails the caller, a lost hint only costs one ladder race.
type StateStore interface {
	Load() State
	Save(State) error
}

// MemStore keeps State in memory (tests, and platforms without a data dir).
type MemStore struct {
	mu sync.Mutex
	s  State
}

// Load returns the stored State.
func (m *MemStore) Load() State { m.mu.Lock(); defer m.mu.Unlock(); return m.s }

// Save replaces the stored State.
func (m *MemStore) Save(s State) error { m.mu.Lock(); m.s = s; m.mu.Unlock(); return nil }

// FileStore keeps State in a JSON file, written atomically with mode 0600.
type FileStore struct{ Path string }

// Load reads the file; a missing, corrupt or stale file yields the zero State.
func (f FileStore) Load() State {
	b, err := os.ReadFile(f.Path)
	if err != nil {
		return State{}
	}
	var s State
	if json.Unmarshal(b, &s) != nil || time.Since(s.At) > stateMaxAge {
		return State{}
	}
	return s
}

// Save writes the file atomically.
func (f FileStore) Save(s State) error {
	if f.Path == "" {
		return errors.New("client: state path empty")
	}
	b, err := json.Marshal(s)
	if err != nil {
		return err
	}
	return atomicfile.Write(f.Path, b, 0o700)
}
