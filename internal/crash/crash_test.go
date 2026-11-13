package crash

import (
	"testing"

	"github.com/getsentry/sentry-go"
)

func TestReporterNoDSN(t *testing.T) {
	t.Setenv("SENTRY_DSN", "")
	t.Setenv("SENTRY_DSN_DESKTOP", "")
	prev := DSN
	DSN = ""
	t.Cleanup(func() { DSN = prev })
	r := New(Options{DSN: ""})
	if err := r.SetEnabled(true); err == nil {
		t.Fatal("expected error without dsn")
	}
	if r.Enabled() {
		t.Fatal("should stay disabled")
	}
}

func TestBeforeSendScrubs(t *testing.T) {
	ev := &sentry.Event{Message: "fail Bearer secrettoken123 at 1.2.3.4"}
	out := beforeSend(ev, nil)
	if out == nil {
		t.Fatal("nil")
	}
	if contains(out.Message, "secrettoken") || contains(out.Message, "1.2.3.4") {
		t.Fatalf("not scrubbed: %q", out.Message)
	}
}

func contains(s, sub string) bool {
	return len(sub) > 0 && (len(s) >= len(sub)) && (stringIndex(s, sub) >= 0)
}

func stringIndex(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
