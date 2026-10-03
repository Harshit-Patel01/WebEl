package services

import (
	"testing"
	"time"
)

func TestRingBufferWrapsAndDedupes(t *testing.T) {
	r := newRingBuffer(3)

	add := func(id string) bool {
		return r.add(bufferedEvent{ev: NormalizedEvent{ID: id}})
	}

	if add("a") != true || add("b") != true || add("c") != true {
		t.Fatal("first three adds should all be accepted")
	}
	if add("c") != false {
		t.Fatal("re-adding a held ID must be rejected")
	}

	// Wrapping past capacity evicts the oldest entry.
	if add("d") != true {
		t.Fatal("add after wrap should be accepted")
	}

	got := ids(r.snapshot())
	if len(got) != 3 {
		t.Fatalf("ring should hold capacity 3, got %d (%v)", len(got), got)
	}
	for _, gone := range []string{"a"} {
		for _, id := range got {
			if id == gone {
				t.Fatalf("evicted ID %q still present: %v", gone, got)
			}
		}
	}
	// Order must be oldest first, i.e. the rotation point is respected.
	if got[0] != "b" || got[1] != "c" || got[2] != "d" {
		t.Fatalf("want oldest-first b,c,d after wrap; got %v", got)
	}

	// An evicted ID becomes storable again, and the seen-set must not grow
	// without bound as entries cycle.
	for i := 0; i < 50; i++ {
		add("x" + time.Duration(i).String())
	}
	if len(r.seen) > 3 {
		t.Fatalf("seen set leaked past capacity: %d entries", len(r.seen))
	}
}

func ids(b []bufferedEvent) []string {
	out := make([]string, len(b))
	for i, e := range b {
		out[i] = e.ev.ID
	}
	return out
}

func TestGetEventsFilteredTimeRange(t *testing.T) {
	r := newRingBuffer(10)
	now := time.Now()

	for _, tc := range []struct {
		id string
		at time.Time
	}{
		{"old", now.Add(-30 * time.Minute)},
		{"mid", now.Add(-5 * time.Minute)},
		{"new", now.Add(-1 * time.Minute)},
	} {
		r.add(bufferedEvent{
			ev: NormalizedEvent{ID: tc.id, TS: tc.at.Format(time.RFC3339)},
			at: tc.at,
		})
	}

	svc := &EventService{ring: r}
	cutoff := now.Add(-10 * time.Minute)

	resp := svc.GetEventsFiltered(100, cutoff, time.Time{})
	if len(resp.Events) != 2 {
		t.Fatalf("since filter should keep 2 events, got %d", len(resp.Events))
	}
	for _, e := range resp.Events {
		if e.ID == "old" {
			t.Fatal("event before cutoff leaked through the since filter")
		}
	}

	// Newest first.
	if resp.Events[0].ID != "new" {
		t.Fatalf("want newest first, got %q then %q", resp.Events[0].ID, resp.Events[1].ID)
	}

	// An unbounded query returns everything.
	if all := svc.GetEventsFiltered(100, time.Time{}, time.Time{}); len(all.Events) != 3 {
		t.Fatalf("unbounded query should return 3, got %d", len(all.Events))
	}

	// Stats reflect the filtered set, not the whole ring.
	if resp.Stats.Total != 2 {
		t.Fatalf("stats should match filtered count 2, got %d", resp.Stats.Total)
	}
}

func TestIsNoiseFiltersDashboardChatter(t *testing.T) {
	// These must be suppressed: they are our own polling and tooling.
	for _, msg := range []string{
		`GET /api/v1/logs/events - HTTP 200 - 12 bytes`,
		`GET /api/v1/system - HTTP 200 - 4 bytes`,
		"job started",
		"journal_logs",
		"/usr/bin/journalctl",
		"pam_unix(sudo:session): session opened",
		"opendeploy.service: Deactivated successfully.",
		"GET /_next/static/chunk.js - HTTP 200 - 900 bytes",
	} {
		if !isNoise("NGINX", msg) {
			t.Errorf("expected noise to be filtered: %q", msg)
		}
	}

	// These must survive: they are real host activity.
	for _, msg := range []string{
		"GET /api/v1/deploys - HTTP 500 - 20 bytes",
		"failed to bind port 8080: address already in use",
		"nginx: [emerg] bind() to 0.0.0.0:80 failed",
		`POST /api/v1/deploy - HTTP 201 - 88 bytes`,
	} {
		if isNoise("NGINX", msg) {
			t.Errorf("real event was wrongly filtered: %q", msg)
		}
	}
}
