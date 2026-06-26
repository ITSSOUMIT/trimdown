package run

import (
	"errors"
	"testing"

	"github.com/itssoumit/trimdown/internal/engine"
	"github.com/itssoumit/trimdown/internal/ir"
	"github.com/itssoumit/trimdown/internal/registry"
	"github.com/itssoumit/trimdown/internal/store"
)

type stubFilter struct {
	parse func(engine.CaptureResult, registry.Opts) (ir.Report, error)
}

func (stubFilter) Tool() string                            { return "stub" }
func (stubFilter) Subcommand() string                      { return "" }
func (stubFilter) Exec(registry.Opts) engine.CaptureResult { return engine.CaptureResult{} }
func (s stubFilter) Parse(c engine.CaptureResult, o registry.Opts) (ir.Report, error) {
	return s.parse(c, o)
}

func TestSafeParse_Filtered(t *testing.T) {
	f := stubFilter{parse: func(engine.CaptureResult, registry.Opts) (ir.Report, error) {
		return ir.Report{Summary: "ok", Filtered: true}, nil
	}}
	_, mode := safeParse(f, engine.CaptureResult{Stdout: "lots of output"}, registry.Opts{})
	if mode != store.ModeFiltered {
		t.Fatalf("mode = %q, want filtered", mode)
	}
}

func TestSafeParse_ErrorFallsBackToRaw(t *testing.T) {
	f := stubFilter{parse: func(engine.CaptureResult, registry.Opts) (ir.Report, error) {
		return ir.Report{}, errors.New("boom")
	}}
	rep, mode := safeParse(f, engine.CaptureResult{Stdout: "raw out", ExitCode: 2}, registry.Opts{})
	if mode != store.ModeParseFail {
		t.Fatalf("mode = %q, want parse_fail", mode)
	}
	if rep.Raw != "raw out" || rep.Filtered {
		t.Fatalf("expected raw fallback report, got %+v", rep)
	}
}

func TestSafeParse_PanicIsRecovered(t *testing.T) {
	f := stubFilter{parse: func(engine.CaptureResult, registry.Opts) (ir.Report, error) {
		panic("kaboom")
	}}
	rep, mode := safeParse(f, engine.CaptureResult{Stdout: "raw"}, registry.Opts{})
	if mode != store.ModeParseFail {
		t.Fatalf("mode = %q, want parse_fail after panic", mode)
	}
	if rep.Raw != "raw" {
		t.Fatalf("expected raw output preserved, got %q", rep.Raw)
	}
}

func TestRawOf(t *testing.T) {
	if got := rawOf(engine.CaptureResult{Stdout: "a", Stderr: "b"}); got != "ab" {
		t.Fatalf("rawOf both = %q, want ab", got)
	}
	if got := rawOf(engine.CaptureResult{Stderr: "only-err"}); got != "only-err" {
		t.Fatalf("rawOf stderr-only = %q", got)
	}
}
