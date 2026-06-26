// Package golang holds native filters for the Go toolchain: `go test` (NDJSON
// streaming), `go build`/`go vet` (error extraction), and golangci-lint.
package golang

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/itssoumit/trimdown/internal/engine"
	"github.com/itssoumit/trimdown/internal/ir"
	"github.com/itssoumit/trimdown/internal/registry"
)

func init() {
	registry.Register(goTest{})
	registry.Register(goBuild{})
	registry.Register(goVet{})
	registry.Register(golangci{})
}

const maxFailureLines = 5

type goTest struct{}

func (goTest) Tool() string       { return "go" }
func (goTest) Subcommand() string { return "test" }

func (goTest) Exec(o registry.Opts) engine.CaptureResult {
	rest := o.Args
	if len(rest) > 0 && rest[0] == "test" {
		rest = rest[1:]
	}
	args := []string{"test"}
	if !contains(rest, "-json") {
		args = append(args, "-json")
	}
	args = append(args, rest...)
	return engine.Capture(engine.ResolvedCommand("go", args...))
}

type goEvent struct {
	Action     string `json:"Action"`
	Package    string `json:"Package"`
	Test       string `json:"Test"`
	Output     string `json:"Output"`
	ImportPath string `json:"ImportPath"`
}

func (goTest) Parse(c engine.CaptureResult, _ registry.Opts) (ir.Report, error) {
	var pass, fail, skip int
	pkgs := map[string]bool{}
	outputs := map[string][]string{}
	var failures []ir.TestResult

	var buildErrors []string // build-output lines (compile failures, setup failures)
	buildFailed := false

	engine.ScanLines(c.Stdout, func(line string) {
		line = strings.TrimSpace(line)
		if line == "" || line[0] != '{' {
			return
		}
		var ev goEvent
		if json.Unmarshal([]byte(line), &ev) != nil {
			return
		}
		if ev.Package != "" {
			pkgs[ev.Package] = true
		}
		key := ev.Package + "\t" + ev.Test
		switch ev.Action {
		case "build-output":
			if t := strings.TrimRight(ev.Output, "\n"); strings.TrimSpace(t) != "" {
				buildErrors = append(buildErrors, t)
			}
		case "build-fail":
			buildFailed = true
		case "output":
			if ev.Test != "" {
				outputs[key] = append(outputs[key], strings.TrimRight(ev.Output, "\n"))
			} else if isSetupFailure(ev.Output) {
				buildErrors = append(buildErrors, strings.TrimRight(ev.Output, "\n"))
			}
		case "pass":
			if ev.Test != "" {
				pass++
			}
		case "skip":
			if ev.Test != "" {
				skip++
			}
		case "fail":
			if ev.Test != "" {
				fail++
				failures = append(failures, ir.TestResult{
					Name:    ev.Test,
					Package: shortPkg(ev.Package),
					Status:  ir.StatusFail,
					Detail:  selectFailureLines(outputs[key]),
				})
			} else if ev.FailedBuildOrSetup() {
				buildFailed = true
			}
		}
	})

	// Build/compile/setup failure with no test results — surface the error.
	if pass+fail+skip == 0 && (buildFailed || len(buildErrors) > 0) {
		return ir.Report{
			Tool: "go", Subcommand: "test",
			Summary:  "build failed",
			Status:   ir.StatusFail,
			Notes:    capLines(buildErrors, maxFailureLines),
			Filtered: true, Raw: rawOf(c),
		}, nil
	}

	// Not NDJSON at all (unexpected) — fall back to raw.
	if pass+fail+skip == 0 && len(pkgs) == 0 {
		return ir.Report{Filtered: false, Raw: rawOf(c), ExitCode: c.ExitCode}, nil
	}

	status := ir.StatusOK
	summary := fmt.Sprintf("%d passed", pass)
	if fail > 0 {
		status = ir.StatusFail
		summary = fmt.Sprintf("%d passed, %d failed", pass, fail)
	}
	if skip > 0 {
		summary += fmt.Sprintf(", %d skipped", skip)
	}
	summary += fmt.Sprintf(" in %d package(s)", len(pkgs))

	return ir.Report{
		Tool:       "go",
		Subcommand: "test",
		Summary:    summary,
		Status:     status,
		Tests:      failures,
		Filtered:   true,
		Raw:        rawOf(c),
	}, nil
}

// FailedBuildOrSetup reports a package-level failure that isn't a specific test
// (build failure, setup failure, timeout).
func (e goEvent) FailedBuildOrSetup() bool { return e.Test == "" }

func isSetupFailure(out string) bool {
	return strings.Contains(out, "[setup failed]") || strings.Contains(out, "[build failed]")
}

func capLines(lines []string, n int) []string {
	if len(lines) <= n {
		return lines
	}
	return append(lines[:n:n], fmt.Sprintf("… +%d more", len(lines)-n))
}

// selectFailureLines keeps up to maxFailureLines informative lines from a failed
// test's output (locations and failure indicators), skipping go test framing.
func selectFailureLines(lines []string) []string {
	var out []string
	for _, l := range lines {
		t := strings.TrimSpace(l)
		if t == "" || strings.HasPrefix(t, "=== RUN") || strings.HasPrefix(t, "=== PAUSE") ||
			strings.HasPrefix(t, "=== CONT") || strings.HasPrefix(t, "--- FAIL") ||
			strings.HasPrefix(t, "--- PASS") || strings.HasPrefix(t, "--- SKIP") {
			continue
		}
		out = append(out, strings.TrimRight(l, " "))
		if len(out) >= maxFailureLines {
			break
		}
	}
	return out
}

func shortPkg(pkg string) string {
	if i := strings.LastIndex(pkg, "/"); i >= 0 {
		return pkg[i+1:]
	}
	return pkg
}

func contains(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

func rawOf(c engine.CaptureResult) string {
	switch {
	case c.Stderr == "":
		return c.Stdout
	case c.Stdout == "":
		return c.Stderr
	default:
		return c.Stdout + c.Stderr
	}
}
