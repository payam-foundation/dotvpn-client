//go:build linux && !android

package tun

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNewMark(t *testing.T) {
	seen := map[int]bool{}
	for range 50 {
		m := newMark()
		if m <= 0xffff || m > 0x7fffffff {
			t.Fatalf("mark %#x out of range", m)
		}
		seen[m] = true
	}
	if len(seen) < 45 {
		t.Fatalf("marks are not random: %d distinct of 50", len(seen))
	}
	if fwMark == 0x6476 {
		t.Fatal("mark is the old fixed value")
	}
}

func TestSameNS(t *testing.T) {
	d := t.TempDir()
	link := func(name, target string) string {
		p := filepath.Join(d, name)
		if err := os.Symlink(target, p); err != nil {
			t.Fatal(err)
		}
		return p
	}
	a, b, c := link("a", "net:[1]"), link("b", "net:[1]"), link("c", "net:[2]")
	if !sameNS(a, b) {
		t.Error("same namespace reported different")
	}
	if sameNS(a, c) {
		t.Error("different namespaces reported the same")
	}
	if !sameNS(a, filepath.Join(d, "missing")) {
		t.Error("unreadable link must count as same (keep old behaviour)")
	}
}
