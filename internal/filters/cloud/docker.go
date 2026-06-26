package cloud

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/itssoumit/trimdown/internal/engine"
	"github.com/itssoumit/trimdown/internal/ir"
	"github.com/itssoumit/trimdown/internal/registry"
)

// dockerBuildStepRE matches BuildKit per-layer progress lines like
// "#12 0.345 ..." and bare "#12 [stage 2/5] ..." we want to thin out.
var (
	dockerBuildStepRE = regexp.MustCompile(`^#\d+\s`)
	dockerBuildErrRE  = regexp.MustCompile(`(?i)\berror\b|\bfailed\b|cannot|denied`)
	dockerPullLayerRE = regexp.MustCompile(`^[0-9a-f]{12}:\s|Pulling fs layer|Waiting|Verifying Checksum|Download complete|Pull complete|Extracting|Downloading|Pushed|Pushing|Mounted from|Layer already exists|Preparing`)
)

// docker is the whole-tool dispatcher that routes on the first non-flag arg.
type docker struct{}

func (docker) Tool() string       { return "docker" }
func (docker) Subcommand() string { return "" }

func (docker) Exec(o registry.Opts) engine.CaptureResult {
	return engine.Capture(engine.ResolvedCommand("docker", o.Args...))
}

func (docker) Parse(c engine.CaptureResult, o registry.Opts) (ir.Report, error) {
	sub := firstNonFlagArg(o.Args)
	switch sub {
	case "ps":
		return dockerPS(c)
	case "images":
		return dockerImages(c)
	case "logs":
		return dockerLogs(c)
	case "build":
		return dockerBuild(c, dockerBuildTag(o.Args))
	case "pull":
		return dockerPullPush(c, "pulled", dockerImageArg(o.Args, "pull"))
	case "push":
		return dockerPullPush(c, "pushed", dockerImageArg(o.Args, "push"))
	case "inspect":
		return dockerInspect(c)
	case "version", "info":
		return dockerKeyLines(c, sub)
	case "compose":
		return dockerCompose(c, o.Args)
	case "exec", "run":
		return dockerExecRun(c)
	default:
		return ir.RawReport("docker", rawOf(c), c.ExitCode), nil
	}
}

func firstNonFlagArg(args []string) string {
	for _, a := range args {
		if a == "" || a[0] == '-' {
			continue
		}
		return a
	}
	return ""
}

// dockerPS parses the `docker ps` / `ps -a` table into name/image/status items.
func dockerPS(c engine.CaptureResult) (ir.Report, error) {
	if c.ExitCode != 0 {
		return dockerErrorOrRaw(c)
	}
	lines := splitLines(engine.StripANSI(c.Stdout))
	if len(lines) == 0 {
		return ir.Report{Tool: "docker", Status: ir.StatusOK, Summary: "no containers", Filtered: true, Raw: rawOf(c)}, nil
	}
	header := lines[0]
	idx := headerIndex(header, "IMAGE", "STATUS", "NAMES")
	var items []ir.Item
	for _, l := range lines[1:] {
		if strings.TrimSpace(l) == "" {
			continue
		}
		image := columnAt(l, idx, "IMAGE")
		status := columnAt(l, idx, "STATUS")
		name := columnAt(l, idx, "NAMES")
		items = append(items, ir.Item{Key: name, Val: strings.TrimSpace(image + "  " + status)})
	}
	items, notes := capItems(items)
	return ir.Report{
		Tool: "docker", Status: ir.StatusOK,
		Summary: fmt.Sprintf("%d container(s)", len(items)),
		Items:   items, Notes: notes, Filtered: true, Raw: rawOf(c),
	}, nil
}

// dockerImages parses `docker images` into repo:tag → size.
func dockerImages(c engine.CaptureResult) (ir.Report, error) {
	if c.ExitCode != 0 {
		return dockerErrorOrRaw(c)
	}
	lines := splitLines(engine.StripANSI(c.Stdout))
	if len(lines) == 0 {
		return ir.Report{Tool: "docker", Status: ir.StatusOK, Summary: "no images", Filtered: true, Raw: rawOf(c)}, nil
	}
	idx := headerIndex(lines[0], "REPOSITORY", "TAG", "SIZE")
	var items []ir.Item
	for _, l := range lines[1:] {
		if strings.TrimSpace(l) == "" {
			continue
		}
		repo := columnAt(l, idx, "REPOSITORY")
		tag := columnAt(l, idx, "TAG")
		size := columnAt(l, idx, "SIZE")
		key := repo
		if tag != "" && tag != "<none>" {
			key = repo + ":" + tag
		}
		items = append(items, ir.Item{Key: key, Val: size})
	}
	items, notes := capItems(items)
	return ir.Report{
		Tool: "docker", Status: ir.StatusOK,
		Summary: fmt.Sprintf("%d image(s)", len(items)),
		Items:   items, Notes: notes, Filtered: true, Raw: rawOf(c),
	}, nil
}

func dockerLogs(c engine.CaptureResult) (ir.Report, error) {
	lines := splitLines(engine.StripANSI(rawOf(c)))
	lines = dedupLines(lines)
	for i := range lines {
		lines[i] = truncateRunes(lines[i], 200)
	}
	lines, notes := capLines(lines, maxCloudLines)
	st := ir.StatusOK
	if c.ExitCode != 0 {
		st = ir.StatusFail
	}
	return ir.Report{Tool: "docker", Status: st, Text: strings.Join(lines, "\n"), Notes: notes, Filtered: true, Raw: rawOf(c)}, nil
}

// dockerBuild strips per-layer progress noise, keeping step transitions, the
// final image id, and any errors.
func dockerBuild(c engine.CaptureResult, tag string) (ir.Report, error) {
	lines := splitLines(engine.StripANSI(rawOf(c)))
	var kept []string
	var hadErr bool
	var imageID string
	for _, l := range lines {
		t := strings.TrimSpace(l)
		if t == "" {
			continue
		}
		if dockerBuildErrRE.MatchString(t) {
			hadErr = true
			kept = append(kept, truncateRunes(t, 200))
			continue
		}
		// Keep step transitions: legacy "Step 3/8 :" and BuildKit stage headers.
		if strings.HasPrefix(t, "Step ") || strings.Contains(t, "writing image") || strings.Contains(t, "naming to") {
			kept = append(kept, truncateRunes(t, 200))
		}
		if m := dockerImageIDRE.FindStringSubmatch(t); m != nil {
			imageID = m[1]
		}
		// Drop BuildKit per-layer "#NN ..." progress and pull progress.
		if dockerBuildStepRE.MatchString(t) || dockerPullLayerRE.MatchString(t) {
			continue
		}
	}
	if imageID != "" {
		kept = append(kept, "image "+imageID)
	}
	kept, notes := capLines(kept, maxCloudLines)
	st := ir.StatusOK
	var summary string
	if c.ExitCode != 0 || hadErr {
		st = ir.StatusFail
		summary = "build failed"
		if tag != "" {
			summary = "build failed " + strings.TrimSpace(tag)
		}
	} else {
		summary = strings.TrimSpace("build " + tag)
	}
	return ir.Report{Tool: "docker", Status: st, Summary: summary, Text: strings.Join(kept, "\n"), Notes: notes, Filtered: true, Raw: rawOf(c)}, nil
}

var dockerImageIDRE = regexp.MustCompile(`(?:Successfully built|writing image)\s+(sha256:[0-9a-f]+|[0-9a-f]{12})`)

func dockerPullPush(c engine.CaptureResult, verb, image string) (ir.Report, error) {
	if c.ExitCode != 0 {
		return dockerErrorOrRaw(c)
	}
	// Keep only the digest/status summary line(s); strip layer progress.
	var status string
	for _, l := range splitLines(engine.StripANSI(rawOf(c))) {
		t := strings.TrimSpace(l)
		if t == "" || dockerPullLayerRE.MatchString(t) {
			continue
		}
		if strings.HasPrefix(t, "Digest:") || strings.HasPrefix(t, "Status:") {
			status = t
		}
	}
	summary := verb + " " + image
	if status != "" {
		return ir.Report{Tool: "docker", Status: ir.StatusOK, Summary: summary, Text: status, Filtered: true, Raw: rawOf(c)}, nil
	}
	return ir.Report{Tool: "docker", Status: ir.StatusOK, Summary: summary, Filtered: true, Raw: rawOf(c)}, nil
}

// dockerInspect compacts inspect JSON into a few structural key fields.
func dockerInspect(c engine.CaptureResult) (ir.Report, error) {
	if c.ExitCode != 0 {
		return dockerErrorOrRaw(c)
	}
	var arr []map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(c.Stdout)), &arr); err != nil || len(arr) == 0 {
		// Fall back to compact JSON.
		return genericJSONCompact("docker", c)
	}
	var lines []string
	for _, obj := range arr {
		if id, ok := obj["Id"].(string); ok {
			lines = append(lines, "Id  "+truncateRunes(id, 20))
		}
		if name, ok := obj["Name"].(string); ok {
			lines = append(lines, "Name  "+strings.TrimPrefix(name, "/"))
		}
		if st, ok := obj["State"].(map[string]any); ok {
			if s, ok := st["Status"].(string); ok {
				lines = append(lines, "State  "+s)
			}
		}
		if cfg, ok := obj["Config"].(map[string]any); ok {
			if img, ok := cfg["Image"].(string); ok {
				lines = append(lines, "Image  "+img)
			}
		}
		if repos, ok := obj["RepoTags"].([]any); ok && len(repos) > 0 {
			lines = append(lines, "RepoTags  "+joinAny(repos))
		}
	}
	lines, notes := capLines(lines, maxCloudLines)
	return ir.Report{Tool: "docker", Status: ir.StatusOK, Text: strings.Join(lines, "\n"), Notes: notes, Filtered: true, Raw: rawOf(c)}, nil
}

func dockerKeyLines(c engine.CaptureResult, sub string) (ir.Report, error) {
	lines := splitLines(engine.StripANSI(c.Stdout))
	var kept []string
	for _, l := range lines {
		t := strings.TrimSpace(l)
		if t == "" {
			continue
		}
		// Keep "Key: value" style lines (the informative ones).
		if strings.Contains(t, ":") {
			kept = append(kept, t)
		}
	}
	kept, notes := capLines(kept, maxCloudLines)
	return ir.Report{Tool: "docker", Subcommand: sub, Status: ir.StatusOK, Text: strings.Join(kept, "\n"), Notes: notes, Filtered: true, Raw: rawOf(c)}, nil
}

// dockerCompose routes compose subcommands.
func dockerCompose(c engine.CaptureResult, args []string) (ir.Report, error) {
	cs := composeSub(args)
	switch cs {
	case "ps":
		return dockerPS(c)
	case "logs":
		return dockerLogs(c)
	case "up", "build", "down":
		// Strip progress, keep errors.
		lines := splitLines(engine.StripANSI(rawOf(c)))
		var kept []string
		hadErr := c.ExitCode != 0
		for _, l := range lines {
			t := strings.TrimSpace(l)
			if t == "" || dockerPullLayerRE.MatchString(t) || dockerBuildStepRE.MatchString(t) {
				continue
			}
			if dockerBuildErrRE.MatchString(t) {
				hadErr = true
			}
			kept = append(kept, truncateRunes(t, 200))
		}
		kept, notes := capLines(kept, maxCloudLines)
		st := ir.StatusOK
		if hadErr {
			st = ir.StatusFail
		}
		return ir.Report{Tool: "docker", Subcommand: "compose " + cs, Status: st, Text: strings.Join(kept, "\n"), Notes: notes, Filtered: true, Raw: rawOf(c)}, nil
	default:
		return ir.RawReport("docker", rawOf(c), c.ExitCode), nil
	}
}

// dockerExecRun passes program output through; compacts a docker start error.
func dockerExecRun(c engine.CaptureResult) (ir.Report, error) {
	if c.ExitCode != 0 && isDockerStartError(c.Stderr) {
		return ir.Report{Tool: "docker", Status: ir.StatusFail, Summary: "run failed", Text: truncateRunes(strings.TrimSpace(c.Stderr), 300), Filtered: true, Raw: rawOf(c), ExitCode: c.ExitCode}, nil
	}
	return ir.RawReport("docker", rawOf(c), c.ExitCode), nil
}

func isDockerStartError(stderr string) bool {
	s := strings.TrimSpace(stderr)
	return strings.HasPrefix(s, "docker:") || strings.Contains(s, "Unable to find image") || strings.Contains(s, "Error response from daemon")
}

// dockerErrorOrRaw compacts a recognizable docker daemon error to one line,
// else passes the raw output through.
func dockerErrorOrRaw(c engine.CaptureResult) (ir.Report, error) {
	s := strings.TrimSpace(rawOf(c))
	if strings.Contains(s, "Error response from daemon") || strings.HasPrefix(s, "docker:") || strings.Contains(s, "No such") {
		first := splitLines(s)
		msg := ""
		if len(first) > 0 {
			msg = truncateRunes(first[0], 300)
		}
		return ir.Report{Tool: "docker", Status: ir.StatusFail, Summary: msg, Filtered: true, Raw: rawOf(c), ExitCode: c.ExitCode}, nil
	}
	return ir.RawReport("docker", rawOf(c), c.ExitCode), nil
}

// --- helpers shared by docker table parsing ---

// headerIndex returns the byte offset of each named column header in the
// header line, for fixed-width column extraction.
func headerIndex(header string, cols ...string) map[string]int {
	idx := map[string]int{}
	for _, col := range cols {
		if p := strings.Index(header, col); p >= 0 {
			idx[col] = p
		}
	}
	return idx
}

// columnAt extracts the value under column `col` from a docker table row, using
// the header offsets. Falls back to whitespace splitting if offsets unknown.
func columnAt(line string, idx map[string]int, col string) string {
	start, ok := idx[col]
	if !ok || start > len(line) {
		return ""
	}
	// Find the next column start to bound the field.
	end := len(line)
	for _, p := range idx {
		if p > start && p < end {
			end = p
		}
	}
	if end > len(line) {
		end = len(line)
	}
	return strings.TrimSpace(line[start:end])
}

func capItems(items []ir.Item) ([]ir.Item, []string) {
	if len(items) <= maxCloudLines {
		return items, nil
	}
	notes := []string{fmt.Sprintf("… +%d more", len(items)-maxCloudLines)}
	return items[:maxCloudLines], notes
}

func dockerBuildTag(args []string) string {
	for i, a := range args {
		if (a == "-t" || a == "--tag") && i+1 < len(args) {
			return args[i+1]
		}
		if strings.HasPrefix(a, "--tag=") {
			return strings.TrimPrefix(a, "--tag=")
		}
	}
	return ""
}

func dockerImageArg(args []string, sub string) string {
	seen := false
	for _, a := range args {
		if a == sub {
			seen = true
			continue
		}
		if !seen {
			continue
		}
		if a == "" || a[0] == '-' {
			continue
		}
		return a
	}
	return ""
}

func composeSub(args []string) string {
	seen := false
	for _, a := range args {
		if a == "compose" {
			seen = true
			continue
		}
		if !seen {
			continue
		}
		if a == "" || a[0] == '-' {
			continue
		}
		return a
	}
	return ""
}

func joinAny(vals []any) string {
	var parts []string
	for _, v := range vals {
		if s, ok := v.(string); ok {
			parts = append(parts, s)
		}
	}
	return strings.Join(parts, ", ")
}
