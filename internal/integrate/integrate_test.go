package integrate

import (
	"strings"
	"testing"
)

func TestEnsurePreservesOtherKeys(t *testing.T) {
	root := map[string]any{"model": "opus", "effortLevel": "high"}
	if !ensureHook(root, "PreToolUse", "Bash", ccHookCommand) {
		t.Fatal("ensureHook should report a change")
	}
	if root["model"] != "opus" || root["effortLevel"] != "high" {
		t.Fatalf("unrelated keys lost: %+v", root)
	}
	if !hasHook(root, "PreToolUse", ccHookCommand) {
		t.Fatal("hook not detectable after install")
	}
}

func TestEnsureIdempotent(t *testing.T) {
	root := map[string]any{}
	ensureHook(root, "PreToolUse", "Bash", ccHookCommand)
	if ensureHook(root, "PreToolUse", "Bash", ccHookCommand) {
		t.Fatal("second ensureHook should be a no-op")
	}
}

func TestEnsureAppendsToExistingMatcher(t *testing.T) {
	root := map[string]any{
		"hooks": map[string]any{
			"PreToolUse": []any{
				map[string]any{
					"matcher": "Bash",
					"hooks":   []any{map[string]any{"type": "command", "command": "other-tool"}},
				},
			},
		},
	}
	if !ensureHook(root, "PreToolUse", "Bash", ccHookCommand) {
		t.Fatal("should append to existing Bash matcher")
	}
	pre := hookList(root, "PreToolUse")
	if len(pre) != 1 {
		t.Fatalf("should reuse the single Bash matcher, got %d entries", len(pre))
	}
	inner, _ := pre[0].(map[string]any)["hooks"].([]any)
	if len(inner) != 2 {
		t.Fatalf("existing hook should be kept alongside ours, got %d", len(inner))
	}
}

func TestInstallAddsBothHooks(t *testing.T) {
	root := map[string]any{"model": "opus"}
	ensureHook(root, "PreToolUse", "Bash", ccHookCommand)
	ensureHook(root, "PostToolUse", ccPostMatcher, ccHookCommand)
	if !hasHook(root, "PreToolUse", ccHookCommand) || !hasHook(root, "PostToolUse", ccHookCommand) {
		t.Fatalf("both hooks should be present: %+v", root)
	}
	// The PostToolUse matcher targets the native search tools.
	post := hookList(root, "PostToolUse")
	if m, _ := post[0].(map[string]any); asString(m["matcher"]) != "Grep|Glob" {
		t.Fatalf("post matcher = %q, want Grep|Glob", asString(post[0].(map[string]any)["matcher"]))
	}
}

func TestRemoveCleansUpAndPreserves(t *testing.T) {
	root := map[string]any{"model": "opus"}
	ensureHook(root, "PreToolUse", "Bash", ccHookCommand)
	ensureHook(root, "PostToolUse", ccPostMatcher, ccHookCommand)
	removeHook(root, "PreToolUse", ccHookCommand)
	removeHook(root, "PostToolUse", ccHookCommand)
	if _, ok := root["hooks"]; ok {
		t.Fatalf("empty hooks structure should be cleaned up: %+v", root)
	}
	if root["model"] != "opus" {
		t.Fatal("unrelated key lost on uninstall")
	}
}

func TestRemoveKeepsOtherHooks(t *testing.T) {
	root := map[string]any{
		"hooks": map[string]any{
			"PreToolUse": []any{
				map[string]any{
					"matcher": "Bash",
					"hooks": []any{
						map[string]any{"type": "command", "command": "other-tool"},
						map[string]any{"type": "command", "command": ccHookCommand},
					},
				},
			},
		},
	}
	if !removeHook(root, "PreToolUse", ccHookCommand) {
		t.Fatal("should remove our hook")
	}
	inner, _ := hookList(root, "PreToolUse")[0].(map[string]any)["hooks"].([]any)
	if len(inner) != 1 || asString(inner[0].(map[string]any)["command"]) != "other-tool" {
		t.Fatalf("other tool's hook should remain: %+v", inner)
	}
}

func TestCompactOutputCapsAndTruncates(t *testing.T) {
	// Small output is left untouched.
	if _, changed := compactOutput("a\nb\nc"); changed {
		t.Fatal("small output should not be compacted")
	}
	// Many lines get capped with a note.
	var big strings.Builder
	for i := 0; i < 200; i++ {
		big.WriteString("match line\n")
	}
	out, changed := compactOutput(big.String())
	if !changed {
		t.Fatal("large output should be compacted")
	}
	if n := strings.Count(out, "\n") + 1; n > maxOutputLines+1 {
		t.Fatalf("not capped: %d lines", n)
	}
	if !strings.Contains(out, "more lines hidden by trimdown") {
		t.Fatalf("missing overflow note:\n%s", out)
	}
}

func TestToolTextShapes(t *testing.T) {
	// string shape → string back
	txt, rewrap, ok := toolText("hello")
	if !ok || txt != "hello" {
		t.Fatalf("string shape failed: %q %v", txt, ok)
	}
	if s, _ := rewrap("x").(string); s != "x" {
		t.Fatalf("string rewrap failed: %T", rewrap("x"))
	}
	// content-block array → array back
	arr := []any{map[string]any{"type": "text", "text": "a"}, map[string]any{"type": "text", "text": "b"}}
	txt, rewrap, ok = toolText(arr)
	if !ok || txt != "ab" {
		t.Fatalf("array shape failed: %q %v", txt, ok)
	}
	if _, isArr := rewrap("z").([]any); !isArr {
		t.Fatalf("array rewrap should return []any, got %T", rewrap("z"))
	}
	// unknown shape → not ok (no-op, original kept)
	if _, _, ok := toolText(map[string]any{"stdout": "x"}); ok {
		t.Fatal("object shape should be unrecognized (no-op)")
	}
}
