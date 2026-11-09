package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
)

// Prefs are the app's own preferences. They live next to the app, never in
// the helper: the helper owns only what the tunnel needs.
type Prefs struct {
	// Language is "en" or "fa". Empty until the frontend resolves the system
	// language on first start.
	Language string `json:"language"`
	// Theme is "system", "light" or "dark".
	Theme string `json:"theme"`
	// StartAtLogin is read from and written to the OS, not the file.
	StartAtLogin bool `json:"startAtLogin"`
	// StartMinimized hides the window when the OS launches the app at login.
	StartMinimized bool `json:"startMinimized"`
}

func defaultPrefs() Prefs { return Prefs{Theme: "system", StartMinimized: true} }

func (p Prefs) valid() error {
	switch p.Language {
	case "", "en", "fa":
	default:
		return errors.New("unknown language")
	}
	switch p.Theme {
	case "system", "light", "dark":
	default:
		return errors.New("unknown theme")
	}
	return nil
}

type prefStore struct {
	path string
	mu   sync.Mutex
	cur  Prefs
}

func openPrefs() *prefStore {
	dir, err := os.UserConfigDir()
	if err != nil {
		dir = os.TempDir()
	}
	s := &prefStore{path: filepath.Join(dir, "dotvpn", "desktop.json"), cur: defaultPrefs()}
	if b, err := os.ReadFile(s.path); err == nil {
		p := defaultPrefs()
		if json.Unmarshal(b, &p) == nil && p.valid() == nil {
			s.cur = p
		}
	}
	return s
}

func (s *prefStore) get() Prefs {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cur
}

func (s *prefStore) set(p Prefs) error {
	if err := p.valid(); err != nil {
		return err
	}
	b, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(s.path, b, 0o644); err != nil {
		return err
	}
	s.mu.Lock()
	s.cur = p
	s.mu.Unlock()
	return nil
}
