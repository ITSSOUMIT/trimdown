package golang

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/itssoumit/trimdown/internal/engine"
	"github.com/itssoumit/trimdown/internal/ir"
	"github.com/itssoumit/trimdown/internal/registry"
)

// goErrRE matches a Go compiler/vet diagnostic: "path.go:line[:col]: message".
var goErrRE = regexp.MustCompile(`^(.+\.go):(\d+):(?:(\d+):)? (.+)$`)

const maxGoErrors = 25

type goBuild struct{}

func (goBuild) Tool() string       { return "go" }
func (goBuild) Subcommand() string { return "build" }
func (goBuild) Exec(o registry.Opts) engine.CaptureResult {
	return engine.Capture(engine.ResolvedCommand("go", o.Args...))
}
func (goBuild) Parse(c engine.CaptureResult, _ registry.Opts) (ir.Report, error) {
	return parseGoErrors("build", c)
}

type goVet struct{}

func (goVet) Tool() string       { return "go" }
func (goVet) Subcommand() string { return "vet" }
func (goVet) Exec(o registry.Opts) engine.CaptureResult {
	return engine.Capture(engine.ResolvedCommand("go", o.Args...))
}
func (goVet) Parse(c engine.CaptureResult, _ registry.Opts) (ir.Report, error) {
	return parseGoErrors("vet", c)
}

// parseGoErrors extracts file:line diagnostics from go build/vet (which write
// to stderr). The render layer groups them by file.
func parseGoErrors(sub string, c engine.CaptureResult) (ir.Report, error) {
	src := c.Stderr
	if src == "" {
		src = c.Stdout
	}
	var diags []ir.Diagnostic
	overflow := 0
	engine.ScanLines(src, func(line string) {
		if strings.HasPrefix(line, "#") || strings.HasPrefix(line, "go: downloading") {
			return
		}
		m := goErrRE.FindStringSubmatch(strings.TrimSpace(line))
		if m == nil {
			return
		}
		if len(diags) >= maxGoErrors {
			overflow++
			return
		}
		ln, _ := strconv.Atoi(m[2])
		col, _ := strconv.Atoi(m[3])
		diags = append(diags, ir.Diagnostic{
			File: m[1], Line: ln, Col: col, Severity: ir.SevError, Message: m[4],
		})
	})

	if len(diags) == 0 {
		if c.ExitCode == 0 {
			return ir.Report{Tool: "go", Subcommand: sub, Summary: "ok", Status: ir.StatusOK, Filtered: true, Raw: rawOf(c)}, nil
		}
		// Failed but no recognizable diagnostics — show raw.
		return ir.Report{Filtered: false, Raw: rawOf(c), ExitCode: c.ExitCode}, nil
	}

	notes := []string(nil)
	if overflow > 0 {
		notes = []string{fmt.Sprintf("… +%d more errors", overflow)}
	}
	return ir.Report{
		Tool:        "go",
		Subcommand:  sub,
		Summary:     fmt.Sprintf("%d error(s)", len(diags)+overflow),
		Status:      ir.StatusFail,
		Diagnostics: diags,
		Notes:       notes,
		Filtered:    true,
		Raw:         rawOf(c),
	}, nil
}
