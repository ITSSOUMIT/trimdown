// Package ir defines the typed intermediate representation that every filter's
// Parse produces. Parsers emit a Report; reducers transform it; renderers turn
// it into text / ultra-compact / JSON. This Parse→IR→Render split is the core
// architectural improvement over rtk's string→string filtering.
package ir

// Status is the overall outcome of a command.
type Status int

const (
	StatusOK Status = iota
	StatusWarn
	StatusFail
)

func (s Status) String() string {
	switch s {
	case StatusOK:
		return "ok"
	case StatusWarn:
		return "warn"
	case StatusFail:
		return "fail"
	default:
		return "unknown"
	}
}

// Severity classifies a diagnostic.
type Severity int

const (
	SevError Severity = iota
	SevWarning
	SevNote
	SevInfo
)

// Diagnostic is a single linter/compiler finding.
type Diagnostic struct {
	File     string   `json:"file,omitempty"`
	Line     int      `json:"line,omitempty"`
	Col      int      `json:"col,omitempty"`
	Severity Severity `json:"severity"`
	Rule     string   `json:"rule,omitempty"` // e.g. "F401", "errcheck"
	Message  string   `json:"message"`
	Context  []string `json:"context,omitempty"`
}

// TestResult is a single test outcome (for test-runner filters).
type TestResult struct {
	Name    string   `json:"name"`
	Package string   `json:"package,omitempty"`
	Status  Status   `json:"status"`
	Detail  []string `json:"detail,omitempty"` // failure lines, capped
	Elapsed float64  `json:"elapsed,omitempty"`
}

// Item is a generic key/value list entry (packages, branches, pods, ...).
type Item struct {
	Key string `json:"key"`
	Val string `json:"val,omitempty"`
}

// Report is the typed result of parsing a tool's output.
type Report struct {
	Tool        string       `json:"tool"`
	Subcommand  string       `json:"subcommand,omitempty"`
	Summary     string       `json:"summary"`
	Status      Status       `json:"status"`
	Diagnostics []Diagnostic `json:"diagnostics,omitempty"`
	Tests       []TestResult `json:"tests,omitempty"`
	Items       []Item       `json:"items,omitempty"`
	Notes       []string     `json:"notes,omitempty"` // hints, e.g. "[full diff: --no-compact]"

	// Text is pre-formatted output for filters that produce compacted text
	// rather than structured diagnostics/tests (e.g. the declarative engine,
	// or simple line-strip native filters). When set, renderers emit it
	// directly instead of formatting the structured fields.
	Text string `json:"text,omitempty"`

	// Filtered reports whether we actually compacted. When false, renderers
	// emit Raw unchanged (the fail-safe / passthrough path).
	Filtered bool   `json:"-"`
	Raw      string `json:"-"`
	ExitCode int    `json:"-"`
}

// RawReport builds a passthrough Report that renders the raw output unchanged.
func RawReport(tool, raw string, exitCode int) Report {
	return Report{Tool: tool, Filtered: false, Raw: raw, ExitCode: exitCode}
}
