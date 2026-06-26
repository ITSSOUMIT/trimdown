// Package js holds native filters for the JS/TS toolchain: tsc and eslint
// (diagnostics), jest/vitest/playwright (failures-only), and compaction for the
// package-manager-driven tools (npm/npx/pnpm/next/prisma/prettier/biome).
package js

import (
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/itssoumit/trimdown/internal/engine"
	"github.com/itssoumit/trimdown/internal/ir"
	"github.com/itssoumit/trimdown/internal/registry"
)

func init() {
	registry.Register(tsc{})
	registry.Register(eslint{})
	registry.Register(jsTest{tool: "jest"})
	registry.Register(jsTest{tool: "vitest"})
	registry.Register(jsTest{tool: "playwright"})
	registry.Register(prettier{})
	registry.Register(npm{})
	registry.Register(node{})
	for _, t := range []string{"npx", "pnpm", "next", "prisma", "biome"} {
		registry.Register(compactor{tool: t})
	}
}

// pkgExec runs a project-local JS tool. Lockfile presence selects the runner so
// the project's pinned binary is used; otherwise a global binary, else npx.
func pkgExec(tool string, args []string) *exec.Cmd {
	switch {
	case fileExists("pnpm-lock.yaml"):
		return engine.ResolvedCommand("pnpm", append([]string{"exec", tool}, args...)...)
	case fileExists("yarn.lock"):
		return engine.ResolvedCommand("yarn", append([]string{"exec", tool}, args...)...)
	}
	if _, err := exec.LookPath(tool); err == nil {
		return engine.ResolvedCommand(tool, args...)
	}
	return engine.ResolvedCommand("npx", append([]string{"--no-install", tool}, args...)...)
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
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

func splitLines(s string) []string {
	s = strings.TrimRight(s, "\n")
	if s == "" {
		return nil
	}
	lines := strings.Split(s, "\n")
	for i := range lines {
		lines[i] = strings.TrimSuffix(lines[i], "\r")
	}
	return lines
}

func truncateRunes(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max-1]) + "…"
}

// compactor strips ANSI/blank noise, truncates, and caps — used for the
// package-manager tools whose output is progress-heavy but unstructured.
type compactor struct{ tool string }

func (c compactor) Tool() string     { return c.tool }
func (compactor) Subcommand() string { return "" }

func (c compactor) Exec(o registry.Opts) engine.CaptureResult {
	// npm/npx/pnpm are runners themselves; next/prisma/biome go through pkgExec.
	if c.tool == "npm" || c.tool == "npx" || c.tool == "pnpm" {
		return engine.Capture(engine.ResolvedCommand(c.tool, o.Args...))
	}
	return engine.Capture(pkgExec(c.tool, o.Args))
}

func (c compactor) Parse(cr engine.CaptureResult, _ registry.Opts) (ir.Report, error) {
	if cr.ExitCode != 0 {
		return ir.Report{Filtered: false, Raw: rawOf(cr), ExitCode: cr.ExitCode}, nil
	}
	var kept []string
	for _, l := range splitLines(engine.StripANSI(cr.Stdout)) {
		if strings.TrimSpace(l) == "" {
			continue
		}
		kept = append(kept, truncateRunes(l, 200))
	}
	const limit = 60
	var notes []string
	if len(kept) > limit {
		notes = append(notes, "… +"+strconv.Itoa(len(kept)-limit)+" more lines")
		kept = kept[:limit]
	}
	return ir.Report{Tool: c.tool, Status: ir.StatusOK, Text: strings.Join(kept, "\n"), Notes: notes, Filtered: true, Raw: rawOf(cr)}, nil
}
