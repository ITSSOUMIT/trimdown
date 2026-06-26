// Package run is the engine pipeline that ties a Filter to capture, fail-safe
// parsing, rendering, and usage recording:
//
//	Exec → failsafe(Parse) → render → print → record → propagate exit code
package run

import (
	"fmt"
	"os"
	"time"

	"github.com/itssoumit/trimdown/internal/engine"
	"github.com/itssoumit/trimdown/internal/ir"
	"github.com/itssoumit/trimdown/internal/registry"
	"github.com/itssoumit/trimdown/internal/render"
	"github.com/itssoumit/trimdown/internal/store"
	"github.com/itssoumit/trimdown/internal/tokenizer"
)

// Execute runs one filtered command end to end and returns its exit code.
func Execute(f registry.Filter, o registry.Opts) int {
	start := time.Now()
	cr := f.Exec(o)

	rep, mode := safeParse(f, cr, o)
	rep.Tool = f.Tool()
	rep.ExitCode = cr.ExitCode // always preserve the underlying tool's code

	out := render.Render(rep, render.Opts{JSON: o.JSON, Raw: o.Raw, Quiet: o.Quiet})
	fmt.Println(out)

	record(f, o, cr, out, mode, time.Since(start))
	return cr.ExitCode
}

// safeParse wraps the filter's Parse so a panic or error never breaks the
// command — it falls back to raw output and flags the run as a parse failure.
func safeParse(f registry.Filter, cr engine.CaptureResult, o registry.Opts) (rep ir.Report, mode store.Mode) {
	mode = store.ModeFiltered
	defer func() {
		if r := recover(); r != nil {
			rep = ir.RawReport(f.Tool(), rawOf(cr), cr.ExitCode)
			mode = store.ModeParseFail
		}
	}()

	rep, err := f.Parse(cr, o)
	if err != nil {
		return ir.RawReport(f.Tool(), rawOf(cr), cr.ExitCode), store.ModeParseFail
	}
	if !rep.Filtered {
		mode = store.ModePassthrough
	}
	return rep, mode
}

func record(f registry.Filter, o registry.Opts, cr engine.CaptureResult, out string, mode store.Mode, dur time.Duration) {
	tok := tokenizer.Default()
	in := tok.Count(rawOf(cr))
	outTok := tok.Count(out)
	ev := store.NewEvent(f.Tool(), firstNonFlag(o.Args), in, outTok, dur.Milliseconds(), mode)
	recordEvent(ev)
}

// rawOf is the unfiltered output the agent would otherwise have seen.
func rawOf(c engine.CaptureResult) string {
	if c.Stderr == "" {
		return c.Stdout
	}
	if c.Stdout == "" {
		return c.Stderr
	}
	return c.Stdout + c.Stderr
}

// recordEvent is best-effort: tracking must never break or slow a command.
func recordEvent(ev store.Event) {
	s, err := store.Open()
	if err != nil {
		return
	}
	if err := s.Record(ev); err != nil && os.Getenv("TRIMDOWN_DEBUG") != "" {
		fmt.Fprintln(os.Stderr, "trimdown: record:", err)
	}
}

func firstNonFlag(args []string) string {
	for _, a := range args {
		if a == "" || a[0] == '-' {
			continue
		}
		return a
	}
	return ""
}

// RecordPassthrough logs a passthrough command (0% savings) without filtering.
func RecordPassthrough(tool string, args []string, dur time.Duration) {
	recordEvent(store.NewEvent(tool, firstNonFlag(args), 0, 0, dur.Milliseconds(), store.ModePassthrough))
}
