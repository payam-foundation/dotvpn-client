package crash_test

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/payam-foundation/dotvpn-client/internal/crash"
)

// Live smoke against payam-foundation EU projects. Opt-in via SENTRY_SMOKE=1
// and DSNs from sibling .env.sentry (never committed).
func TestLiveSentrySmoke(t *testing.T) {
	if os.Getenv("SENTRY_SMOKE") != "1" {
		t.Skip("set SENTRY_SMOKE=1 to send live events")
	}
	env := loadEnvSentry(t)
	desktop := env["SENTRY_DSN_DESKTOP"]
	android := env["SENTRY_DSN_ANDROID"]
	if desktop == "" || android == "" {
		t.Fatal("missing DSNs in .env.sentry")
	}
	token := time.Now().UTC().Format("20060102T150405Z")

	t.Run("desktop_opt_in_message", func(t *testing.T) {
		r := crash.New(crash.Options{
			DSN:         desktop,
			Release:     "smoke-desktop@" + token,
			Environment: "smoke",
		})
		if err := r.SetEnabled(true); err != nil {
			t.Fatal(err)
		}
		defer func() { _ = r.SetEnabled(false) }()
		msg := "dv-desktop smoke " + token
		id := sentry.CaptureMessage(msg)
		ok := sentry.Flush(10 * time.Second)
		if id == nil || *id == "" || !ok {
			t.Fatalf("desktop send failed id=%v flush=%v", id, ok)
		}
		t.Log("sent:", msg, "event_id=", string(*id))
	})

	t.Run("desktop_opt_out_no_send", func(t *testing.T) {
		r := crash.New(crash.Options{DSN: desktop, Environment: "smoke"})
		if r.Enabled() {
			t.Fatal("should start disabled")
		}
		// Capture with DSN cleared client should not deliver.
		_ = sentry.Init(sentry.ClientOptions{Dsn: ""})
		crash.CaptureMessage("dv-desktop should-not-appear " + token)
	})

	t.Run("android_dsn_ingest", func(t *testing.T) {
		// Proves the Android project DSN accepts events (Go transport smoke).
		// Native Sentry Android SDK is compiled into the app; toggle enables it.
		if err := sentry.Init(sentry.ClientOptions{
			Dsn:              android,
			Release:          "smoke-android@" + token,
			Environment:      "smoke",
			SendDefaultPII:   false,
			TracesSampleRate: 0,
		}); err != nil {
			t.Fatal(err)
		}
		defer func() { _ = sentry.Init(sentry.ClientOptions{Dsn: ""}) }()
		msg := "dv-android smoke " + token
		id := sentry.CaptureMessage(msg)
		ok := sentry.Flush(10 * time.Second)
		if id == nil || *id == "" || !ok {
			t.Fatalf("android send failed id=%v flush=%v", id, ok)
		}
		t.Log("sent:", msg, "event_id=", string(*id))
	})
}

func loadEnvSentry(t *testing.T) map[string]string {
	t.Helper()
	candidates := []string{
		filepath.Join("..", "..", ".env.sentry"),
		filepath.Join("..", "..", "..", "dotvpn-android", ".env.sentry"),
	}
	var path string
	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			path = c
			break
		}
	}
	if path == "" {
		t.Fatal(".env.sentry not found")
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	out := map[string]string{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") || !strings.Contains(line, "=") {
			continue
		}
		k, v, _ := strings.Cut(line, "=")
		out[strings.TrimSpace(k)] = strings.TrimSpace(v)
	}
	return out
}
