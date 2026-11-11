package diag

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRingAndScrub(t *testing.T) {
	r := NewRing("desktop", "1.2.3")
	r.Note(Event{Code: CodeConnectFail, Detail: "timeout at 1.2.3.4", LatencyMS: 250})
	r.Note(Event{Code: CodeConnectOK, EndpointID: "abc", LatencyMS: 40})
	b := r.Snapshot()
	if len(b.Events) != 2 {
		t.Fatalf("events=%d", len(b.Events))
	}
	if b.Events[0].Detail == "timeout at 1.2.3.4" || strings.Contains(b.Events[0].Detail, "1.2.3.4") {
		t.Fatalf("detail not scrubbed: %q", b.Events[0].Detail)
	}
	if b.Events[0].LatencyMS != 200 {
		t.Fatalf("latency bucket=%d", b.Events[0].LatencyMS)
	}
	if b.FailCounts["connect_fail:redacted"]+b.FailCounts["connect_fail:"+b.Events[0].Detail] == 0 && len(b.FailCounts) == 0 {
		t.Fatalf("fail counts empty: %+v", b.FailCounts)
	}
}

func TestSubmitControl(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/support/report" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"issue_url": "https://github.com/x/y/issues/1"})
	}))
	t.Cleanup(srv.Close)
	c := &ClientSubmit{ControlURL: srv.URL, AccountKey: "k", HTTP: srv.Client()}
	res, err := c.Submit(context.Background(), Bundle{Platform: "cli", AppVersion: "1"})
	if err != nil || res.Via != "control" || res.IssueURL == "" {
		t.Fatalf("res=%+v err=%v", res, err)
	}
}

func TestSubmitBrowserFallback(t *testing.T) {
	opened := ""
	c := &ClientSubmit{
		ControlURL: "http://127.0.0.1:1",
		AccountKey: "k",
		OpenURL:    func(u string) error { opened = u; return nil },
	}
	res, err := c.Submit(context.Background(), Bundle{Platform: "android", AppVersion: "1"})
	if err != nil || res.Via != "browser" || opened == "" || !strings.Contains(opened, "dotvpn-android") {
		t.Fatalf("res=%+v opened=%q err=%v", res, opened, err)
	}
	if strings.Contains(opened, "template=") || strings.Contains(opened, "body=") {
		t.Fatalf("issue forms ignore body; want title-only URL: %q", opened)
	}
	if !strings.Contains(opened, "title=") {
		t.Fatalf("missing title: %q", opened)
	}
}

func TestScrubBanned(t *testing.T) {
	if scrubLine("full tunnel up") != "redacted" {
		t.Fatal("expected redacted")
	}
}
