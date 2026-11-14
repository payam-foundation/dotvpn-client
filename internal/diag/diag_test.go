// SPDX-License-Identifier: AGPL-3.0-only

package diag

import (
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

func TestScrubBanned(t *testing.T) {
	if scrubLine("full tunnel up") != "redacted" {
		t.Fatal("expected redacted")
	}
}
