// Package store records one usage event per command to an append-only NDJSON
// log and aggregates it on read. Append-only + O_APPEND gives lock-free,
// crash-safe writes that are safe across concurrent trimdown invocations
// (e.g. parallel agent subagents) — a better fit than rtk's SQLite-per-write.
package store

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// Mode classifies how a command was handled.
type Mode string

const (
	ModeFiltered    Mode = "filtered"    // a filter compacted the output
	ModePassthrough Mode = "passthrough" // ran raw, recorded for usage
	ModeParseFail   Mode = "parse_fail"  // filter errored → raw fallback
)

// Event is one recorded command.
type Event struct {
	TS      string  `json:"ts"` // RFC3339 UTC
	Tool    string  `json:"tool"`
	Sub     string  `json:"sub,omitempty"`
	Project string  `json:"project,omitempty"`
	In      int     `json:"in"`  // input tokens (raw output)
	Out     int     `json:"out"` // output tokens (filtered)
	Saved   int     `json:"saved"`
	Pct     float64 `json:"pct"`
	MS      int64   `json:"ms"`
	Mode    Mode    `json:"mode"`
}

// NewEvent computes saved/pct and stamps the time + project path.
func NewEvent(tool, sub string, inTokens, outTokens int, ms int64, mode Mode) Event {
	saved := inTokens - outTokens
	if saved < 0 {
		saved = 0
	}
	var pct float64
	if inTokens > 0 {
		pct = float64(saved) / float64(inTokens) * 100
	}
	return Event{
		TS:      time.Now().UTC().Format(time.RFC3339),
		Tool:    tool,
		Sub:     sub,
		Project: projectPath(),
		In:      inTokens,
		Out:     outTokens,
		Saved:   saved,
		Pct:     pct,
		MS:      ms,
		Mode:    mode,
	}
}

// Store persists and reads events.
type Store interface {
	Record(Event) error
	Read() ([]Event, error)
}

// EventLog is the default append-only NDJSON Store.
type EventLog struct{ path string }

// Open returns the default event log, creating its directory if needed.
func Open() (*EventLog, error) {
	p := logPath()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return nil, err
	}
	return &EventLog{path: p}, nil
}

// Record appends one event as a single NDJSON line. Under O_APPEND a write this
// small is atomic, so concurrent writers don't interleave or need a lock.
func (e *EventLog) Record(ev Event) error {
	f, err := os.OpenFile(e.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	b, err := json.Marshal(ev)
	if err != nil {
		return err
	}
	_, err = f.Write(append(b, '\n'))
	return err
}

// Read loads all events, skipping any malformed lines (forward-compatible).
func (e *EventLog) Read() ([]Event, error) {
	f, err := os.Open(e.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer func() { _ = f.Close() }()

	var out []Event
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var ev Event
		if json.Unmarshal(line, &ev) == nil {
			out = append(out, ev)
		}
	}
	return out, sc.Err()
}

// Reset deletes the event log, returning all savings totals to zero. A fresh
// log is created on the next recorded command.
func (e *EventLog) Reset() error {
	if err := os.Remove(e.path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func logPath() string {
	if d := os.Getenv("TRIMDOWN_DATA"); d != "" {
		return filepath.Join(d, "events.ndjson")
	}
	if d, err := os.UserConfigDir(); err == nil {
		return filepath.Join(d, "trimdown", "events.ndjson")
	}
	return filepath.Join(os.TempDir(), "trimdown", "events.ndjson")
}

func projectPath() string {
	if wd, err := os.Getwd(); err == nil {
		return wd
	}
	return ""
}
