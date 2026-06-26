package python

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"

	"github.com/itssoumit/trimdown/internal/engine"
	"github.com/itssoumit/trimdown/internal/ir"
	"github.com/itssoumit/trimdown/internal/registry"
)

const maxPipItems = 60

type pip struct{}

func (pip) Tool() string       { return "pip" }
func (pip) Subcommand() string { return "" }

// pipBase returns ["pip"] or ["uv","pip"] depending on what's available.
func pipBase() (string, []string) {
	if _, err := exec.LookPath("pip"); err == nil {
		return "pip", nil
	}
	if _, err := exec.LookPath("uv"); err == nil {
		return "uv", []string{"pip"}
	}
	if _, err := exec.LookPath("pip3"); err == nil {
		return "pip3", nil
	}
	return "pip", nil
}

func (pip) Exec(o registry.Opts) engine.CaptureResult {
	sub := firstArg(o.Args)
	bin, prefix := pipBase()
	args := append(append([]string{}, prefix...), o.Args...)
	if (sub == "list" || sub == "outdated") && !hasAnyPrefix(o.Args, "--format") {
		args = append(args, "--format=json")
	}
	return engine.Capture(engine.ResolvedCommand(bin, args...))
}

type pipPkg struct {
	Name          string `json:"name"`
	Version       string `json:"version"`
	LatestVersion string `json:"latest_version"`
}

func (pip) Parse(c engine.CaptureResult, o registry.Opts) (ir.Report, error) {
	sub := firstArg(o.Args)
	if sub != "list" && sub != "outdated" {
		// install/show/etc. — pass through.
		return ir.Report{Filtered: false, Raw: rawOf(c), ExitCode: c.ExitCode}, nil
	}
	var pkgs []pipPkg
	if err := json.Unmarshal([]byte(strings.TrimSpace(c.Stdout)), &pkgs); err != nil {
		return ir.Report{Filtered: false, Raw: rawOf(c), ExitCode: c.ExitCode}, nil
	}

	if sub == "outdated" {
		if len(pkgs) == 0 {
			return ir.Report{Tool: "pip", Summary: "all up to date", Status: ir.StatusOK, Filtered: true, Raw: rawOf(c)}, nil
		}
		items := make([]ir.Item, 0, len(pkgs))
		for _, p := range pkgs {
			items = append(items, ir.Item{Key: p.Name, Val: p.Version + " → " + p.LatestVersion})
		}
		return ir.Report{
			Tool: "pip", Summary: fmt.Sprintf("%d outdated", len(pkgs)),
			Status: ir.StatusWarn, Items: capItems(items), Filtered: true, Raw: rawOf(c),
		}, nil
	}

	// list
	items := make([]ir.Item, 0, len(pkgs))
	for _, p := range pkgs {
		items = append(items, ir.Item{Key: p.Name, Val: p.Version})
	}
	return ir.Report{
		Tool: "pip", Summary: fmt.Sprintf("%d packages", len(pkgs)),
		Status: ir.StatusOK, Items: capItems(items), Filtered: true, Raw: rawOf(c),
	}, nil
}

func capItems(items []ir.Item) []ir.Item {
	if len(items) <= maxPipItems {
		return items
	}
	out := append([]ir.Item{}, items[:maxPipItems]...)
	return append(out, ir.Item{Key: fmt.Sprintf("… +%d more", len(items)-maxPipItems)})
}
