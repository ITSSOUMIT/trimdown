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
	if s.Commands != 3 || s.Filtered != 2 || s.Passthrough != 1 {
		t.Fatalf("commands=%d filtered=%d passthrough=%d, want 3/2/1", s.Commands, s.Filtered, s.Passthrough)
	}
	if s.TotalSaved != 360+300 {
		t.Fatalf("total saved = %d, want 660", s.TotalSaved)
	}
	if s.Pct != 66 { // aggregate ratio 660/1000, not a mean of per-event pcts
		t.Fatalf("pct = %v, want 66", s.Pct)
	}
	if s.Coverage < 66.6 || s.Coverage > 66.7 { // 2/3 filtered
		t.Fatalf("coverage = %v, want ~66.7", s.Coverage)
	}
	if len(s.Savers) == 0 || s.Savers[0].Command != "git status" {
		t.Fatalf("expected 'git status' as top saver, got %+v", s.Savers)
	}
}

func TestAggregateClassifiesModes(t *testing.T) {
	events := []Event{
		NewEvent("git", "diff", 600, 100, 0, ModeFiltered),         // saver
		NewEvent("cargo", "build", 5000, 5000, 0, ModePassthrough), // opportunity (measured raw)
		NewEvent("go", "test", 800, 800, 0, ModeParseFail),         // failure
	}
	s := Aggregate(events, AggregateOpts{})
	if s.OppTokens != 5000 || len(s.Opportunities) != 1 || s.Opportunities[0].Command != "cargo build" {
		t.Fatalf("opportunity wrong: opp=%d %+v", s.OppTokens, s.Opportunities)
	}
	if s.FailTokens != 800 || len(s.Failures) != 1 || s.Failures[0].Command != "go test" {
		t.Fatalf("failure wrong: fail=%d %+v", s.FailTokens, s.Failures)
	}
	// Passthrough/fail tokens must NOT inflate the effectiveness denominator.
	if s.TotalIn != 600 {
		t.Fatalf("TotalIn = %d, want 600 (filtered only)", s.TotalIn)
	}
}

func TestAggregateBuckets(t *testing.T) {
	events := []Event{
		{TS: "2026-03-01T10:00:00Z", Tool: "git", In: 100, Out: 10, Saved: 90, MS: 50, Mode: ModeFiltered},
		{TS: "2026-03-01T11:00:00Z", Tool: "go", In: 200, Out: 20, Saved: 180, MS: 100, Mode: ModeFiltered},
		{TS: "2026-03-02T09:00:00Z", Tool: "git", In: 100, Out: 50, Saved: 50, MS: 30, Mode: ModeFiltered},
	}
	daily := AggregateBuckets(events, AggregateOpts{}, Daily)
	if len(daily) != 2 {
		t.Fatalf("daily buckets = %d, want 2", len(daily))
	}
	if daily[0].Label != "2026-03-01" || daily[0].Cmds != 2 || daily[0].Saved != 270 {
		t.Fatalf("first daily bucket wrong: %+v", daily[0])
	}
	monthly := AggregateBuckets(events, AggregateOpts{}, Monthly)
	if len(monthly) != 1 || monthly[0].Cmds != 3 || monthly[0].Label != "2026-03" {
		t.Fatalf("monthly wrong: %+v", monthly)
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
