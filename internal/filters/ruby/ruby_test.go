package ruby

import (
	"strings"
	"testing"

	"github.com/itssoumit/trimdown/internal/engine"
	"github.com/itssoumit/trimdown/internal/ir"
	"github.com/itssoumit/trimdown/internal/registry"
)

func TestRspecJSON(t *testing.T) {
	js := `Running via Spring preloader in process 123
{"examples":[
  {"full_description":"User is valid","status":"passed","file_path":"./spec/user_spec.rb","line_number":5},
  {"full_description":"User saves","status":"failed","file_path":"./spec/user_spec.rb","line_number":10,
   "exception":{"class":"RSpec::Expectations::ExpectationNotMetError","message":"expected true\ngot false"}}
 ],
 "summary":{"example_count":2,"failure_count":1,"pending_count":0}}`
	rep, _ := rspec{}.Parse(engine.CaptureResult{Stdout: js}, registry.Opts{})
	if rep.Summary != "1 passed, 1 failed" || rep.Status != ir.StatusFail {
		t.Fatalf("summary=%q status=%v", rep.Summary, rep.Status)
	}
	if len(rep.Tests) != 1 || rep.Tests[0].Name != "User saves" {
		t.Fatalf("tests=%+v", rep.Tests)
	}
	d := strings.Join(rep.Tests[0].Detail, " | ")
	if !strings.Contains(d, "ExpectationNotMetError: expected true") {
		t.Fatalf("detail=%q (class should be shortened, message first line)", d)
	}
}

func TestRubocopJSON(t *testing.T) {
	js := `{"files":[
   {"path":"app/models/user.rb","offenses":[
     {"cop_name":"Layout/TrailingWhitespace","severity":"convention","message":"Trailing whitespace.","location":{"start_line":10,"start_column":5}}
   ]}],
   "summary":{"offense_count":1,"target_file_count":2}}`
	rep, _ := rubocop{}.Parse(engine.CaptureResult{Stdout: js}, registry.Opts{})
	if rep.Summary != "1 offenses (2 files)" {
		t.Fatalf("summary=%q", rep.Summary)
	}
	if len(rep.Diagnostics) != 1 || rep.Diagnostics[0].Rule != "Layout/TrailingWhitespace" {
		t.Fatalf("diags=%+v", rep.Diagnostics)
	}
}

func TestRubocopAutocorrectPassesThrough(t *testing.T) {
	rep, _ := rubocop{}.Parse(engine.CaptureResult{Stdout: "stuff"}, registry.Opts{Args: []string{"-A"}})
	if rep.Filtered {
		t.Fatalf("autocorrect should pass through raw")
	}
}

func TestRubocopClean(t *testing.T) {
	js := `{"files":[],"summary":{"offense_count":0,"target_file_count":3}}`
	rep, _ := rubocop{}.Parse(engine.CaptureResult{Stdout: js}, registry.Opts{})
	if rep.Status != ir.StatusOK || !strings.Contains(rep.Summary, "ok") {
		t.Fatalf("clean: %+v", rep)
	}
}

func TestParseMinitest(t *testing.T) {
	out := `# Running:
..F
Finished in 0.5s

  1) Failure:
TestUser#test_name [test/user_test.rb:15]:
Expected: "a"
  Actual: "b"

3 runs, 4 assertions, 1 failures, 0 errors, 0 skips`
	rep := parseMinitest(out, 1, out)
	if rep.Summary != "3 runs, 1 failures" || rep.Status != ir.StatusFail {
		t.Fatalf("summary=%q status=%v", rep.Summary, rep.Status)
	}
	if len(rep.Tests) != 1 {
		t.Fatalf("tests=%d", len(rep.Tests))
	}
}

func TestRailsTestRegisteredAndParses(t *testing.T) {
	// `rails test` must be its own entry point (not only `rake`), routing to
	// the minitest parser. Guards against the gap we shipped in v0.1.0.
	f := railsTest{}
	if f.Tool() != "rails" || f.Subcommand() != "test" {
		t.Fatalf("tool=%q sub=%q, want rails/test", f.Tool(), f.Subcommand())
	}
	out := "Finished in 0.1s\n5 runs, 7 assertions, 0 failures, 0 errors, 0 skips"
	rep, _ := f.Parse(engine.CaptureResult{Stdout: out, ExitCode: 0}, registry.Opts{Args: []string{"test"}})
	if !rep.Filtered || rep.Summary != "5 runs, 0 failures" {
		t.Fatalf("rails test not parsed: %+v", rep)
	}
}

func TestParseMinitestAllPass(t *testing.T) {
	out := "Finished in 0.1s\n8 runs, 12 assertions, 0 failures, 0 errors, 0 skips"
	rep := parseMinitest(out, 0, out)
	if rep.Status != ir.StatusOK || rep.Summary != "8 runs, 0 failures" {
		t.Fatalf("summary=%q status=%v", rep.Summary, rep.Status)
	}
}

func TestExtractJSON(t *testing.T) {
	if got := extractJSON("noise\n{\"a\":1}\ntrailer"); got != `{"a":1}` {
		t.Fatalf("got %q", got)
	}
	if got := extractJSON("no json here"); got != "" {
		t.Fatalf("expected empty, got %q", got)
	}
}
