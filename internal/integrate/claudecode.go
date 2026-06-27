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

// ccHookCommand is the command Claude Code runs for each Bash tool call.
const ccHookCommand = "trimdown hook claude-code"

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

func ccInstall(path string) (bool, error) {
	root, err := readJSONObject(path)
	if err != nil {
		return false, err
	}
	if !ensureBashHook(root, ccHookCommand) {
		return false, nil // already present
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
	if !removeBashHook(root, ccHookCommand) {
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
	if hasBashHook(root, ccHookCommand) {
		return "✓ installed (" + path + ")"
	}
	return "✗ not installed (" + path + ")"
}

// ccHook is the PreToolUse adapter: read the event on stdin, rewrite the Bash
// command, and emit the decision. Any error path emits a no-op so the original
// command always runs — trimdown can never break the agent's shell.
func ccHook() int {
	data, err := io.ReadAll(os.Stdin)
	if err != nil {
		return emitNoop()
	}
	var ev map[string]any
	if json.Unmarshal(data, &ev) != nil {
		return emitNoop()
	}
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
	out := map[string]any{
		"hookSpecificOutput": map[string]any{
			"hookEventName": "PreToolUse",
			"updatedInput":  ti,
		},
	}
	b, err := json.Marshal(out)
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

func preToolUse(root map[string]any) []any {
	hooks, _ := root["hooks"].(map[string]any)
	if hooks == nil {
		return nil
	}
	pre, _ := hooks["PreToolUse"].([]any)
	return pre
}

func asString(v any) string { s, _ := v.(string); return s }

func hasBashHook(root map[string]any, cmd string) bool {
	for _, e := range preToolUse(root) {
		m, _ := e.(map[string]any)
		if m == nil || asString(m["matcher"]) != "Bash" {
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

// ensureBashHook adds our hook to the PreToolUse Bash matcher (creating the
// structure as needed) and reports whether it changed root.
func ensureBashHook(root map[string]any, cmd string) bool {
	if hasBashHook(root, cmd) {
		return false
	}
	hooks, _ := root["hooks"].(map[string]any)
	if hooks == nil {
		hooks = map[string]any{}
		root["hooks"] = hooks
	}
	pre, _ := hooks["PreToolUse"].([]any)
	hookObj := map[string]any{"type": "command", "command": cmd}

	for _, e := range pre {
		m, _ := e.(map[string]any)
		if m == nil || asString(m["matcher"]) != "Bash" {
			continue
		}
		inner, _ := m["hooks"].([]any)
		m["hooks"] = append(inner, hookObj)
		hooks["PreToolUse"] = pre
		return true
	}
	hooks["PreToolUse"] = append(pre, map[string]any{
		"matcher": "Bash",
		"hooks":   []any{hookObj},
	})
	return true
}

// removeBashHook strips our hook (and any structures it leaves empty) and
// reports whether it changed root.
func removeBashHook(root map[string]any, cmd string) bool {
	hooks, _ := root["hooks"].(map[string]any)
	if hooks == nil {
		return false
	}
	pre, _ := hooks["PreToolUse"].([]any)
	if pre == nil {
		return false
	}
	changed := false
	var newPre []any
	for _, e := range pre {
		m, _ := e.(map[string]any)
		if m == nil || asString(m["matcher"]) != "Bash" {
			newPre = append(newPre, e)
			continue
		}
		inner, _ := m["hooks"].([]any)
		var keep []any
		for _, h := range inner {
			if hm, _ := h.(map[string]any); hm != nil && asString(hm["command"]) == cmd {
				changed = true
				continue
			}
			keep = append(keep, h)
		}
		if len(keep) > 0 {
			m["hooks"] = keep
			newPre = append(newPre, m)
		}
	}
	if !changed {
		return false
	}
	if len(newPre) == 0 {
		delete(hooks, "PreToolUse")
	} else {
		hooks["PreToolUse"] = newPre
	}
	if len(hooks) == 0 {
		delete(root, "hooks")
	}
	return true
}
