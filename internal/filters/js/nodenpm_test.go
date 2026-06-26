package js

import (
	"strings"
	"testing"

	"github.com/itssoumit/trimdown/internal/engine"
	"github.com/itssoumit/trimdown/internal/ir"
	"github.com/itssoumit/trimdown/internal/registry"
)

func TestNpmParse(t *testing.T) {
	tests := []struct {
		name        string
		args        []string
		cr          engine.CaptureResult
		wantFilter  bool
		wantStatus  ir.Status
		wantSummary string   // substring match if non-empty
		wantText    []string // substrings expected in Text/Items
		notText     []string // substrings that must NOT appear
	}{
		{
			name: "install with vulns and deprecation",
			args: []string{"install"},
			cr: engine.CaptureResult{Stdout: `npm warn deprecated har-validator@5.1.5: this library is no longer supported

added 120 packages, removed 4 packages, changed 2 packages, and audited 1400 packages in 12s

5 vulnerabilities (2 low, 1 moderate, 1 high, 1 critical)

run ` + "`npm audit fix`" + ` to fix them, or ` + "`npm audit`" + ` for details.`},
			wantFilter:  true,
			wantStatus:  ir.StatusWarn,
			wantSummary: "+120 -4",
			wantText:    []string{"5 vulnerabilities", "2 low", "1 critical", "deprecated har-validator"},
		},
		{
			name: "install with ERR drops log path",
			args: []string{"install"},
			cr: engine.CaptureResult{
				Stderr: `npm error code E404
npm error 404 Not Found - GET https://registry.npmjs.org/nonexistent-pkg-xyz
npm error A complete log of this run can be found in: /Users/x/.npm/_logs/2024.log`,
				ExitCode: 1,
			},
			wantFilter:  true,
			wantStatus:  ir.StatusFail,
			wantSummary: "install failed",
			wantText:    []string{"E404", "404 Not Found"},
			notText:     []string{"complete log"},
		},
		{
			name:        "outdated exits 1 but is success",
			args:        []string{"outdated"},
			cr:          engine.CaptureResult{Stdout: "Package  Current  Wanted  Latest  Location\nlodash   4.17.20  4.17.21 4.17.21 node_modules/lodash\nreact    17.0.1   17.0.2  18.2.0  node_modules/react\n", ExitCode: 1},
			wantFilter:  true,
			wantStatus:  ir.StatusWarn,
			wantSummary: "2 outdated",
			wantText:    []string{"lodash", "4.17.20→4.17.21", "react", "17.0.1→18.2.0"},
		},
		{
			name:       "run script framing strip",
			args:       []string{"run", "build"},
			cr:         engine.CaptureResult{Stdout: "> myapp@1.0.0 build\n> tsc -p .\n\nCompiled successfully.\nDone in 1.2s\n"},
			wantFilter: true,
			wantStatus: ir.StatusOK,
			wantText:   []string{"Compiled successfully.", "Done in 1.2s"},
			notText:    []string{"myapp@1.0.0", "tsc -p"},
		},
		{
			name:        "run script with npm ERR",
			args:        []string{"run", "build"},
			cr:          engine.CaptureResult{Stdout: "> myapp@1.0.0 build\n> tsc -p .\n\nsrc/index.ts(1,1): error TS1005\n", Stderr: "npm ERR! code ELIFECYCLE\nnpm ERR! Exit status 2\nnpm ERR! A complete log of this run can be found in: /tmp/log", ExitCode: 2},
			wantFilter:  true,
			wantStatus:  ir.StatusFail,
			wantSummary: "script failed",
			wantText:    []string{"ELIFECYCLE", "error TS1005"},
			notText:     []string{"myapp@1.0.0", "complete log"},
		},
		{
			name:        "audit summary",
			args:        []string{"audit"},
			cr:          engine.CaptureResult{Stdout: "# npm audit report\n\n3 vulnerabilities (1 low, 2 high)\n", ExitCode: 1},
			wantFilter:  true,
			wantStatus:  ir.StatusFail,
			wantSummary: "3 vulnerabilities (1 low, 2 high)",
		},
		{
			name:        "npm --version",
			args:        []string{"--version"},
			cr:          engine.CaptureResult{Stdout: "10.2.4\n"},
			wantFilter:  true,
			wantStatus:  ir.StatusOK,
			wantSummary: "10.2.4",
		},
		{
			name:       "unknown subcommand passthrough",
			args:       []string{"frobnicate"},
			cr:         engine.CaptureResult{Stdout: "weird output\n"},
			wantFilter: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rep, err := npm{}.Parse(tc.cr, registry.Opts{Tool: "npm", Args: tc.args})
			if err != nil {
				t.Fatalf("Parse error: %v", err)
			}
			if rep.Filtered != tc.wantFilter {
				t.Fatalf("Filtered = %v, want %v", rep.Filtered, tc.wantFilter)
			}
			if !tc.wantFilter {
				return
			}
			if rep.Status != tc.wantStatus {
				t.Errorf("Status = %v, want %v", rep.Status, tc.wantStatus)
			}
			if tc.wantSummary != "" && !strings.Contains(rep.Summary, tc.wantSummary) {
				t.Errorf("Summary = %q, want substring %q", rep.Summary, tc.wantSummary)
			}
			hay := reportHaystack(rep)
			for _, want := range tc.wantText {
				if !strings.Contains(hay, want) {
					t.Errorf("output missing %q\ngot: %s", want, hay)
				}
			}
			for _, no := range tc.notText {
				if strings.Contains(hay, no) {
					t.Errorf("output should not contain %q\ngot: %s", no, hay)
				}
			}
		})
	}
}

func TestNodeParse(t *testing.T) {
	tests := []struct {
		name        string
		args        []string
		cr          engine.CaptureResult
		wantFilter  bool
		wantStatus  ir.Status
		wantSummary string
		wantText    []string
		notText     []string
	}{
		{
			name:        "node --version",
			args:        []string{"--version"},
			cr:          engine.CaptureResult{Stdout: "v20.11.0\n"},
			wantFilter:  true,
			wantStatus:  ir.StatusOK,
			wantSummary: "v20.11.0",
		},
		{
			name:        "node --check ok",
			args:        []string{"--check", "ok.js"},
			cr:          engine.CaptureResult{ExitCode: 0},
			wantFilter:  true,
			wantStatus:  ir.StatusOK,
			wantSummary: "syntax ok",
		},
		{
			name:        "node --test with failure",
			args:        []string{"--test"},
			cr:          engine.CaptureResult{Stdout: "TAP version 13\nok 1 - adds numbers\nnot ok 2 - subtracts numbers\n  ---\n  error: expected 1 to equal 2\n  ...\n1..2\n# tests 2\n# pass 1\n# fail 1\n", ExitCode: 1},
			wantFilter:  true,
			wantStatus:  ir.StatusFail,
			wantSummary: "2 tests, 1 failed",
			wantText:    []string{"subtracts numbers"},
			notText:     []string{"adds numbers"},
		},
		{
			name: "node script with stack trace compacted",
			args: []string{"script.js"},
			cr: engine.CaptureResult{
				Stdout: "starting up\n",
				Stderr: `/app/script.js:5
  doThing();
  ^

TypeError: doThing is not a function
    at run (/app/script.js:5:3)
    at /app/script.js:9:1
    at Object.<anonymous> (/app/node_modules/lib/index.js:1:1)
    at Module._compile (node:internal/modules/cjs/loader:1234:14)`,
				ExitCode: 1,
			},
			wantFilter:  true,
			wantStatus:  ir.StatusFail,
			wantSummary: "TypeError",
			wantText:    []string{"doThing is not a function", "at run (/app/script.js:5:3)"},
			notText:     []string{"node_modules", "node:internal"},
		},
		{
			name:       "node script clean passthrough",
			args:       []string{"script.js"},
			cr:         engine.CaptureResult{Stdout: "hello world\nall good\n", ExitCode: 0},
			wantFilter: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rep, err := node{}.Parse(tc.cr, registry.Opts{Tool: "node", Args: tc.args})
			if err != nil {
				t.Fatalf("Parse error: %v", err)
			}
			if rep.Filtered != tc.wantFilter {
				t.Fatalf("Filtered = %v, want %v", rep.Filtered, tc.wantFilter)
			}
			if !tc.wantFilter {
				return
			}
			if rep.Status != tc.wantStatus {
				t.Errorf("Status = %v, want %v", rep.Status, tc.wantStatus)
			}
			if tc.wantSummary != "" && !strings.Contains(rep.Summary, tc.wantSummary) {
				t.Errorf("Summary = %q, want substring %q", rep.Summary, tc.wantSummary)
			}
			hay := reportHaystack(rep)
			for _, want := range tc.wantText {
				if !strings.Contains(hay, want) {
					t.Errorf("output missing %q\ngot: %s", want, hay)
				}
			}
			for _, no := range tc.notText {
				if strings.Contains(hay, no) {
					t.Errorf("output should not contain %q\ngot: %s", no, hay)
				}
			}
		})
	}
}

// reportHaystack flattens the human-visible fields of a report for substring
// assertions.
func reportHaystack(r ir.Report) string {
	var b strings.Builder
	b.WriteString(r.Summary)
	b.WriteByte('\n')
	b.WriteString(r.Text)
	b.WriteByte('\n')
	for _, it := range r.Items {
		b.WriteString(it.Key)
		b.WriteByte(' ')
		b.WriteString(it.Val)
		b.WriteByte('\n')
	}
	for _, n := range r.Notes {
		b.WriteString(n)
		b.WriteByte('\n')
	}
	for _, tr := range r.Tests {
		b.WriteString(tr.Name)
		b.WriteByte('\n')
	}
	for _, d := range r.Diagnostics {
		b.WriteString(d.Rule)
		b.WriteByte(' ')
		b.WriteString(d.Message)
		b.WriteByte('\n')
		for _, c := range d.Context {
			b.WriteString(c)
			b.WriteByte('\n')
		}
	}
	return b.String()
}
