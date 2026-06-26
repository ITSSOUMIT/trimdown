package store

import (
	"path/filepath"
	"testing"
)

func TestEventLogRoundTrip(t *testing.T) {
	t.Setenv("TRIMDOWN_DATA", t.TempDir())

	s, err := Open()
	if err != nil {
		t.Fatal(err)
	}
	for _, ev := range []Event{
		NewEvent("git", "status", 400, 40, 12, ModeFiltered),
		NewEvent("pytest", "", 800, 80, 30, ModeFiltered),
		NewEvent("echo", "hi", 0, 0, 1, ModePassthrough),
	} {
		if err := s.Record(ev); err != nil {
			t.Fatal(err)
		}
	}

	events, err := s.Read()
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 3 {
		t.Fatalf("read %d events, want 3", len(events))
	}
}

func TestNewEventSavings(t *testing.T) {
	ev := NewEvent("git", "diff", 1000, 100, 5, ModeFiltered)
	if ev.Saved != 900 {
		t.Fatalf("saved = %d, want 900", ev.Saved)
	}
	if ev.Pct != 90 {
		t.Fatalf("pct = %v, want 90", ev.Pct)
	}
	// no negative savings when output somehow exceeds input
	ev = NewEvent("x", "", 10, 50, 0, ModeFiltered)
	if ev.Saved != 0 {
		t.Fatalf("saved = %d, want 0 (clamped)", ev.Saved)
	}
}

func TestAggregate(t *testing.T) {
	events := []Event{
		NewEvent("git", "status", 400, 40, 0, ModeFiltered), // 90%
		NewEvent("git", "diff", 600, 300, 0, ModeFiltered),  // 50%
		NewEvent("echo", "", 0, 0, 0, ModePassthrough),      // not in avg
	}
	s := Aggregate(events, AggregateOpts{})
	if s.Commands != 3 || s.Filtered != 2 {
		t.Fatalf("commands=%d filtered=%d, want 3/2", s.Commands, s.Filtered)
	}
	if s.TotalSaved != 360+300 {
		t.Fatalf("total saved = %d, want 660", s.TotalSaved)
	}
	if s.AvgPct != 70 { // (90+50)/2, passthrough excluded
		t.Fatalf("avg pct = %v, want 70", s.AvgPct)
	}
	if len(s.TopTools) == 0 || s.TopTools[0].Tool != "git" {
		t.Fatalf("expected git as top tool, got %+v", s.TopTools)
	}
}

func TestHumanize(t *testing.T) {
	cases := map[int]string{0: "0", 999: "999", 1500: "1.5K", 2_000_000: "2M"}
	for in, want := range cases {
		if got := Humanize(in); got != want {
			t.Fatalf("Humanize(%d) = %q, want %q", in, got, want)
		}
	}
}

func TestLogPathHonorsEnv(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("TRIMDOWN_DATA", dir)
	if got := logPath(); got != filepath.Join(dir, "events.ndjson") {
		t.Fatalf("logPath = %q", got)
	}
}
