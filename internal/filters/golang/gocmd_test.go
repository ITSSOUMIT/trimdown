package golang

import (
	"strings"
	"testing"

	"github.com/itssoumit/trimdown/internal/engine"
	"github.com/itssoumit/trimdown/internal/ir"
	"github.com/itssoumit/trimdown/internal/registry"
)

func TestGoCmdParse(t *testing.T) {
	tests := []struct {
		name        string
		args        []string
		cap         engine.CaptureResult
		wantSub     string
		wantStatus  ir.Status
		wantFilter  bool
		wantSummary string   // substring match
		wantItems   []string // substrings expected somewhere in Items (key or val)
		wantNotes   []string // substrings expected somewhere in Notes
		wantDiag    int      // expected number of diagnostics (-1 to skip)
	}{
		{
			name: "mod tidy with downloads strips noise",
			args: []string{"mod", "tidy"},
			cap: engine.CaptureResult{
				Stderr: "go: downloading golang.org/x/text v0.3.7\n" +
					"go: finding module for package golang.org/x/text/encoding\n" +
					"go: extracting golang.org/x/text v0.3.7\n" +
					"go: added golang.org/x/text v0.3.7\n" +
					"go: removed github.com/old/dep v1.2.3\n",
				ExitCode: 0,
			},
			wantSub:     "mod",
			wantStatus:  ir.StatusOK,
			wantFilter:  true,
			wantSummary: "1 added, 1 removed",
			wantItems:   []string{"golang.org/x/text", "github.com/old/dep", "removed v1.2.3"},
			wantDiag:    -1,
		},
		{
			name: "mod tidy needs updates fails",
			args: []string{"mod", "tidy"},
			cap: engine.CaptureResult{
				Stderr:   "go: updates to go.mod needed; to update it:\n\tgo mod tidy\n",
				ExitCode: 1,
			},
			wantSub:     "mod",
			wantStatus:  ir.StatusFail,
			wantFilter:  true,
			wantSummary: "go mod tidy failed",
			wantNotes:   []string{"updates to go.mod needed"},
			wantDiag:    -1,
		},
		{
			name: "go get upgrade",
			args: []string{"get", "golang.org/x/text@latest"},
			cap: engine.CaptureResult{
				Stderr: "go: downloading golang.org/x/text v0.3.7\n" +
					"go: upgraded golang.org/x/text v0.3.0 => v0.3.7\n" +
					"go: added github.com/new/pkg v1.0.0\n" +
					"go: downgraded github.com/foo/bar v2.0.0 => v1.5.0\n",
				ExitCode: 0,
			},
			wantSub:     "get",
			wantStatus:  ir.StatusOK,
			wantFilter:  true,
			wantSummary: "1 added, 1 upgraded, 1 downgraded",
			wantItems:   []string{"upgraded v0.3.0 => v0.3.7", "added v1.0.0", "downgraded v2.0.0 => v1.5.0"},
			wantDiag:    -1,
		},
		{
			name: "go fmt lists reformatted files",
			args: []string{"fmt", "./..."},
			cap: engine.CaptureResult{
				Stdout:   "internal/a/a.go\ninternal/b/b.go\n",
				ExitCode: 0,
			},
			wantSub:     "fmt",
			wantStatus:  ir.StatusWarn,
			wantFilter:  true,
			wantSummary: "2 file(s) reformatted",
			wantItems:   []string{"internal/a/a.go", "internal/b/b.go"},
			wantDiag:    -1,
		},
		{
			name: "go fmt already formatted",
			args: []string{"fmt", "./..."},
			cap: engine.CaptureResult{
				Stdout:   "",
				ExitCode: 0,
			},
			wantSub:     "fmt",
			wantStatus:  ir.StatusOK,
			wantFilter:  true,
			wantSummary: "already formatted",
			wantDiag:    -1,
		},
		{
			name: "go env key=value lines",
			args: []string{"env"},
			cap: engine.CaptureResult{
				Stdout: "GOARCH='arm64'\n" +
					"GOOS='darwin'\n" +
					"GOPATH='/Users/x/go'\n",
				ExitCode: 0,
			},
			wantSub:     "env",
			wantStatus:  ir.StatusOK,
			wantFilter:  true,
			wantSummary: "3 var(s)",
			wantItems:   []string{"GOARCH", "arm64", "GOPATH", "/Users/x/go"},
			wantDiag:    -1,
		},
		{
			name: "go env specific keys bare values",
			args: []string{"env", "GOPATH", "GOROOT"},
			cap: engine.CaptureResult{
				Stdout:   "/Users/x/go\n/usr/local/go\n",
				ExitCode: 0,
			},
			wantSub:     "env",
			wantStatus:  ir.StatusOK,
			wantFilter:  true,
			wantSummary: "2 var(s)",
			wantItems:   []string{"GOPATH", "/Users/x/go", "GOROOT", "/usr/local/go"},
			wantDiag:    -1,
		},
		{
			name: "go run with compile error",
			args: []string{"run", "main.go"},
			cap: engine.CaptureResult{
				Stderr: "# command-line-arguments\n" +
					"./main.go:7:2: undefined: foo\n" +
					"./main.go:9:5: cannot use x (variable of type int) as string value\n",
				ExitCode: 1,
			},
			wantSub:     "run",
			wantStatus:  ir.StatusFail,
			wantFilter:  true,
			wantSummary: "2 error(s)",
			wantDiag:    2,
		},
		{
			name: "go run program output passthrough",
			args: []string{"run", "main.go"},
			cap: engine.CaptureResult{
				Stdout:   "hello world\nresult: 42\n",
				ExitCode: 0,
			},
			wantSub:    "run",
			wantFilter: false,
			wantDiag:   -1,
		},
		{
			name: "go doc passthrough",
			args: []string{"doc", "fmt.Println"},
			cap: engine.CaptureResult{
				Stdout:   "func Println(a ...any) (n int, err error)\n    Println formats using the default formats...\n",
				ExitCode: 0,
			},
			wantSub:    "",
			wantFilter: false,
			wantDiag:   -1,
		},
		{
			name: "go list caps items",
			args: []string{"list", "./..."},
			cap: engine.CaptureResult{
				Stdout:   strings.Repeat("example.com/pkg\n", 60),
				ExitCode: 0,
			},
			wantSub:     "list",
			wantStatus:  ir.StatusOK,
			wantFilter:  true,
			wantSummary: "60 package(s)",
			wantNotes:   []string{"+10 more"},
			wantDiag:    -1,
		},
		{
			name: "go version",
			args: []string{"version"},
			cap: engine.CaptureResult{
				Stdout:   "go version go1.22.0 darwin/arm64\n",
				ExitCode: 0,
			},
			wantSub:     "version",
			wantStatus:  ir.StatusOK,
			wantFilter:  true,
			wantSummary: "go version go1.22.0",
			wantDiag:    -1,
		},
		{
			name: "go clean ok",
			args: []string{"clean"},
			cap: engine.CaptureResult{
				Stdout:   "",
				ExitCode: 0,
			},
			wantSub:     "clean",
			wantStatus:  ir.StatusOK,
			wantFilter:  true,
			wantSummary: "ok go clean",
			wantDiag:    -1,
		},
		{
			name: "go install ok strips downloads",
			args: []string{"install", "example.com/cmd@latest"},
			cap: engine.CaptureResult{
				Stderr:   "go: downloading example.com/cmd v1.0.0\n",
				ExitCode: 0,
			},
			wantSub:     "install",
			wantStatus:  ir.StatusOK,
			wantFilter:  true,
			wantSummary: "ok go install",
			wantDiag:    -1,
		},
	}

	var f goCmd
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rep, err := f.Parse(tt.cap, registry.Opts{Tool: "go", Args: tt.args})
			if err != nil {
				t.Fatalf("Parse error: %v", err)
			}
			if rep.Filtered != tt.wantFilter {
				t.Errorf("Filtered = %v, want %v", rep.Filtered, tt.wantFilter)
			}
			if !tt.wantFilter {
				// Passthrough: should carry raw and exit code, nothing else.
				if rep.Raw == "" {
					t.Errorf("passthrough Raw is empty")
				}
				return
			}
			if rep.Subcommand != tt.wantSub {
				t.Errorf("Subcommand = %q, want %q", rep.Subcommand, tt.wantSub)
			}
			if rep.Status != tt.wantStatus {
				t.Errorf("Status = %v, want %v", rep.Status, tt.wantStatus)
			}
			if tt.wantSummary != "" && !strings.Contains(rep.Summary, tt.wantSummary) {
				t.Errorf("Summary = %q, want substring %q", rep.Summary, tt.wantSummary)
			}
			if tt.wantDiag >= 0 && len(rep.Diagnostics) != tt.wantDiag {
				t.Errorf("len(Diagnostics) = %d, want %d", len(rep.Diagnostics), tt.wantDiag)
			}
			for _, want := range tt.wantItems {
				if !itemsContain(rep.Items, want) {
					t.Errorf("Items missing substring %q; got %+v", want, rep.Items)
				}
			}
			for _, want := range tt.wantNotes {
				if !linesContain(rep.Notes, want) {
					t.Errorf("Notes missing substring %q; got %+v", want, rep.Notes)
				}
			}
		})
	}
}

func TestGoCmdRegistration(t *testing.T) {
	f, ok := registry.Lookup("go", []string{"mod", "tidy"})
	if !ok {
		t.Fatal("no filter for `go mod tidy`")
	}
	if f.Subcommand() != "" {
		t.Errorf("expected whole-tool dispatcher (Subcommand \"\"), got %q", f.Subcommand())
	}
	// Specific filters must still win.
	tf, ok := registry.Lookup("go", []string{"test", "./..."})
	if !ok || tf.Subcommand() != "test" {
		t.Errorf("expected `go test` to resolve to test filter, got %v ok=%v", tf, ok)
	}
}

func itemsContain(items []ir.Item, sub string) bool {
	for _, it := range items {
		if strings.Contains(it.Key, sub) || strings.Contains(it.Val, sub) {
			return true
		}
	}
	return false
}

func linesContain(lines []string, sub string) bool {
	for _, l := range lines {
		if strings.Contains(l, sub) {
			return true
		}
	}
	return false
}
