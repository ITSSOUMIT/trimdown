package integrate

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/itssoumit/trimdown/internal/rewrite"
)

// ccHookCommand is the command Claude Code runs for matched tool calls. The same
// command serves both events; it branches on hook_event_name.
const (
	ccHookCommand = "trimdown hook claude-code"
	ccPostMatcher = "Grep|Glob" // native tools we compact on PostToolUse
)

// Output-compaction limits for native tool results.
const (
	maxOutputLines  = 80
	outputLineWidth = 240
)

func init() {
	registerAgent(&Agent{
		Name:       "claude-code",
		Display:    "Claude Code",
		ConfigPath: ccConfigPath,
		Install:    ccInstall,
		Uninstall:  ccUninstall,
		Status:     ccStatus,
		Hook:       ccHook,
	})
}

// ccConfigPath returns the settings file to edit. Project-local writes to the
// gitignored personal file so shared/committed config is never touched.
func ccConfigPath(global bool) (string, error) {
	if global {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, ".claude", "settings.json"), nil
	}
	return filepath.Join(".claude", "settings.local.json"), nil
}

// ccInstall wires both hooks: PreToolUse Bash (command rewrite) and PostToolUse
// Grep|Glob (output compaction). Idempotent.
func ccInstall(path string) (bool, error) {
	root, err := readJSONObject(path)
	if err != nil {
		return false, err
	}
	changed := ensureHook(root, "PreToolUse", "Bash", ccHookCommand)
	changed = ensureHook(root, "PostToolUse", ccPostMatcher, ccHookCommand) || changed
	if !changed {
		return false, nil
	}
	if err := writeJSONWithBackup(path, root); err != nil {
		return false, err
	}
	return true, nil
}

func ccUninstall(path string) (bool, error) {
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	root, err := readJSONObject(path)
	if err != nil {
		return false, err
	}
	changed := removeHook(root, "PreToolUse", ccHookCommand)
	changed = removeHook(root, "PostToolUse", ccHookCommand) || changed
	if !changed {
		return false, nil
	}
	return true, writeJSONWithBackup(path, root)
}

func ccStatus(global bool) string {
	path, err := ccConfigPath(global)
	if err != nil {
		return "error: " + err.Error()
	}
	root, err := readJSONObject(path)
	if err != nil {
		return path + " — unreadable (" + err.Error() + ")"
	}
	pre := hasHook(root, "PreToolUse", ccHookCommand)
	post := hasHook(root, "PostToolUse", ccHookCommand)
	switch {
	case pre && post:
		return "✓ installed — Bash rewrite + Grep/Glob compaction (" + path + ")"
	case pre:
		return "◐ partial — Bash rewrite only (" + path + ")"
	case post:
		return "◐ partial — Grep/Glob compaction only (" + path + ")"
	default:
		return "✗ not installed (" + path + ")"
	}
}

// ccHook is the single hook entry point. It branches on the event: PreToolUse
// rewrites the Bash command; PostToolUse compacts a Grep/Glob result. Any error
// path emits a no-op so the original command/output is always used — trimdown
// can never break the agent.
func ccHook() int {
	data, err := io.ReadAll(os.Stdin)
	if err != nil {
		return emitNoop()
	}
	debugLog(data)
	var ev map[string]any
	if json.Unmarshal(data, &ev) != nil {
		return emitNoop()
	}
	if asString(ev["hook_event_name"]) == "PostToolUse" {
		return ccCompactOutput(ev)
	}
	return ccRewriteCommand(ev)
}

// ccRewriteCommand handles PreToolUse: rewrite the Bash command via the safety
// boundary and emit updatedInput.
func ccRewriteCommand(ev map[string]any) int {
	if tn, ok := ev["tool_name"].(string); ok && tn != "Bash" {
		return emitNoop()
	}
	ti, _ := ev["tool_input"].(map[string]any)
	if ti == nil {
		return emitNoop()
	}
	cmd, _ := ti["command"].(string)
	if cmd == "" {
		return emitNoop()
	}
	rewritten, changed := rewrite.Rewrite(cmd)
	if !changed {
		return emitNoop()
	}
	ti["command"] = rewritten // preserve other tool_input fields (description, …)
	return emitJSON(map[string]any{
		"hookSpecificOutput": map[string]any{
			"hookEventName": "PreToolUse",
			"updatedInput":  ti,
		},
	})
}

// ccCompactOutput handles PostToolUse for Grep/Glob: replace the tool result
// with a capped/truncated version, preserving the result's shape. If the shape
// is unrecognized it no-ops (Claude Code then keeps the original).
func ccCompactOutput(ev map[string]any) int {
	switch asString(ev["tool_name"]) {
	case "Grep", "Glob":
	default:
		return emitNoop()
	}
	resp, ok := ev["tool_response"]
	if !ok {
		return emitNoop()
	}
	text, rewrap, ok := toolText(resp)
	if !ok {
		return emitNoop()
	}
	compacted, changed := compactOutput(text)
	if !changed {
		return emitNoop()
	}
	return emitJSON(map[string]any{
		"hookSpecificOutput": map[string]any{
			"hookEventName":     "PostToolUse",
			"updatedToolOutput": rewrap(compacted),
		},
	})
}

// toolText extracts the text payload from a tool_response and returns a function
// that re-wraps a compacted string back into the SAME shape (string ↔ string,
// content-block array ↔ content-block array). Unknown shapes → ok=false.
func toolText(resp any) (text string, rewrap func(string) any, ok bool) {
	switch v := resp.(type) {
	case string:
		return v, func(s string) any { return s }, true
	case []any:
		var sb strings.Builder
		for _, el := range v {
			m, _ := el.(map[string]any)
			if m == nil || asString(m["type"]) != "text" {
				return "", nil, false
			}
			sb.WriteString(asString(m["text"]))
		}
		return sb.String(), func(s string) any {
			return []any{map[string]any{"type": "text", "text": s}}
		}, true
	default:
		return "", nil, false
	}
}

// compactOutput caps the number of lines and truncates very wide lines. It
// reports whether anything changed (so small results are left untouched).
func compactOutput(text string) (string, bool) {
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	changed := false
	for i, l := range lines {
		if t := truncateLine(l, outputLineWidth); t != l {
			lines[i] = t
			changed = true
		}
	}
	if len(lines) > maxOutputLines {
		omitted := len(lines) - maxOutputLines
		lines = append(lines[:maxOutputLines:maxOutputLines],
			fmt.Sprintf("… +%d more lines hidden by trimdown — narrow the search to see them", omitted))
		changed = true
	}
	if !changed {
		return text, false
	}
	return strings.Join(lines, "\n"), true
}

func truncateLine(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max-1]) + "…"
}

func emitJSON(v map[string]any) int {
	b, err := json.Marshal(v)
	if err != nil {
		return emitNoop()
	}
	fmt.Println(string(b))
	return 0
}

func emitNoop() int {
	fmt.Println("{}")
	return 0
}

// debugLog appends the raw hook payload to TRIMDOWN_HOOK_LOG when set, so the
// exact tool_response shapes can be inspected against real runs.
func debugLog(data []byte) {
	p := os.Getenv("TRIMDOWN_HOOK_LOG")
	if p == "" {
		return
	}
	f, err := os.OpenFile(p, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer func() { _ = f.Close() }()
	_, _ = f.Write(append(bytes.TrimRight(data, "\n"), '\n'))
}

// RewriteCmd implements `trimdown rewrite <command…>`: print the rewritten line
// (the safety boundary applied), for debugging and other integrations.
func RewriteCmd(args []string) int {
	out, _ := rewrite.Rewrite(strings.Join(args, " "))
	fmt.Println(out)
	return 0
}

// --- settings.json manipulation (preserves all unrelated keys) ---

func readJSONObject(path string) (map[string]any, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return map[string]any{}, nil
	}
	if err != nil {
		return nil, err
	}
	if b = bytes.TrimSpace(b); len(b) == 0 {
		return map[string]any{}, nil
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if m == nil {
		m = map[string]any{}
	}
	return m, nil
}

func writeJSONWithBackup(path string, root map[string]any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if existing, err := os.ReadFile(path); err == nil {
		if err := os.WriteFile(path+".bak", existing, 0o644); err != nil {
			return err
		}
	}
	b, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(b, '\n'), 0o644)
}

func asString(v any) string { s, _ := v.(string); return s }

// hookList returns the array of matcher entries for a hook event.
func hookList(root map[string]any, event string) []any {
	hooks, _ := root["hooks"].(map[string]any)
	if hooks == nil {
		return nil
	}
	arr, _ := hooks[event].([]any)
	return arr
}

// hasHook reports whether our command is registered under the given event.
func hasHook(root map[string]any, event, cmd string) bool {
	for _, e := range hookList(root, event) {
		m, _ := e.(map[string]any)
		if m == nil {
			continue
		}
		inner, _ := m["hooks"].([]any)
		for _, h := range inner {
			if hm, _ := h.(map[string]any); hm != nil && asString(hm["command"]) == cmd {
				return true
			}
		}
	}
	return false
}

// ensureHook adds our command under event+matcher (creating structure as
// needed) and reports whether it changed root.
func ensureHook(root map[string]any, event, matcher, cmd string) bool {
	if hasHook(root, event, cmd) {
		return false
	}
	hooks, _ := root["hooks"].(map[string]any)
	if hooks == nil {
		hooks = map[string]any{}
		root["hooks"] = hooks
	}
	arr, _ := hooks[event].([]any)
	hookObj := map[string]any{"type": "command", "command": cmd}
	for _, e := range arr {
		m, _ := e.(map[string]any)
		if m == nil || asString(m["matcher"]) != matcher {
			continue
		}
		inner, _ := m["hooks"].([]any)
		m["hooks"] = append(inner, hookObj)
		hooks[event] = arr
		return true
	}
	hooks[event] = append(arr, map[string]any{"matcher": matcher, "hooks": []any{hookObj}})
	return true
}

// removeHook strips our command from an event (cleaning up emptied structures)
// and reports whether it changed root.
func removeHook(root map[string]any, event, cmd string) bool {
	hooks, _ := root["hooks"].(map[string]any)
	if hooks == nil {
		return false
	}
	arr, _ := hooks[event].([]any)
	if arr == nil {
		return false
	}
	changed := false
	var kept []any
	for _, e := range arr {
		m, _ := e.(map[string]any)
		if m == nil {
			kept = append(kept, e)
			continue
		}
		inner, _ := m["hooks"].([]any)
		var keepHooks []any
		removedHere := false
		for _, h := range inner {
			if hm, _ := h.(map[string]any); hm != nil && asString(hm["command"]) == cmd {
				changed, removedHere = true, true
				continue
			}
			keepHooks = append(keepHooks, h)
		}
		if removedHere && len(keepHooks) == 0 {
			continue // drop the matcher we emptied
		}
		if removedHere {
			m["hooks"] = keepHooks
		}
		kept = append(kept, m)
	}
	if !changed {
		return false
	}
	if len(kept) == 0 {
		delete(hooks, event)
	} else {
		hooks[event] = kept
	}
	if len(hooks) == 0 {
		delete(root, "hooks")
	}
	return true
}
