package declarative

import (
	"embed"
	"fmt"
	"io/fs"
	"os"
	"strings"

	"github.com/itssoumit/trimdown/internal/engine"
	"github.com/itssoumit/trimdown/internal/ir"
	"github.com/itssoumit/trimdown/internal/registry"
	"gopkg.in/yaml.v3"
)

//go:embed specs/*.yaml
var specFS embed.FS

func init() {
	specs, errs := LoadEmbedded()
	for _, err := range errs {
		// A malformed embedded spec is a build-time bug caught by tests; in a
		// shipped binary we skip it rather than crash every command.
		if os.Getenv("TRIMDOWN_DEBUG") != "" {
			fmt.Fprintln(os.Stderr, "trimdown: declarative spec:", err)
		}
	}
	for _, s := range specs {
		registry.Register(&Filter{spec: s})
	}
}

// LoadEmbedded parses and compiles every embedded spec, returning the valid
// ones plus any errors encountered (so tests can assert there are none).
func LoadEmbedded() ([]*Spec, []error) {
	return loadFS(specFS, "specs")
}

func loadFS(fsys fs.FS, dir string) ([]*Spec, []error) {
	entries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		return nil, []error{err}
	}
	var specs []*Spec
	var errs []error
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		data, err := fs.ReadFile(fsys, dir+"/"+e.Name())
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", e.Name(), err))
			continue
		}
		var s Spec
		if err := yaml.Unmarshal(data, &s); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", e.Name(), err))
			continue
		}
		if err := s.Compile(); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", e.Name(), err))
			continue
		}
		specs = append(specs, &s)
	}
	return specs, errs
}

// Filter adapts a Spec to the registry.Filter interface.
type Filter struct{ spec *Spec }

func (f *Filter) Tool() string       { return f.spec.Tool }
func (f *Filter) Subcommand() string { return f.spec.Subcommand }

func (f *Filter) Exec(o registry.Opts) engine.CaptureResult {
	return engine.Capture(engine.ResolvedCommand(f.spec.Tool, o.Args...))
}

func (f *Filter) Parse(c engine.CaptureResult, _ registry.Opts) (ir.Report, error) {
	src := c.Stdout
	if f.spec.FilterStderr {
		src = joinNonEmpty(c.Stdout, c.Stderr)
	}
	out := f.spec.Apply(src)

	// When we filter only stdout but the command failed, keep stderr visible so
	// the agent still sees the actual error.
	if c.ExitCode != 0 && !f.spec.FilterStderr {
		if errTxt := strings.TrimRight(c.Stderr, "\n"); strings.TrimSpace(errTxt) != "" {
			if out != "" {
				out += "\n"
			}
			out += errTxt
		}
	}

	return ir.Report{
		Tool:       f.spec.Tool,
		Subcommand: f.spec.Subcommand,
		Filtered:   true,
		Text:       out,
		Raw:        joinNonEmpty(c.Stdout, c.Stderr),
	}, nil
}

func joinNonEmpty(a, b string) string {
	switch {
	case a == "":
		return b
	case b == "":
		return a
	default:
		return a + b
	}
}
