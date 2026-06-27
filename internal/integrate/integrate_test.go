package integrate

import "testing"

func TestEnsurePreservesOtherKeys(t *testing.T) {
	root := map[string]any{"model": "opus", "effortLevel": "high"}
	if !ensureBashHook(root, ccHookCommand) {
		t.Fatal("ensureBashHook should report a change")
	}
	if root["model"] != "opus" || root["effortLevel"] != "high" {
		t.Fatalf("unrelated keys lost: %+v", root)
	}
	if !hasBashHook(root, ccHookCommand) {
		t.Fatal("hook not detectable after install")
	}
}

func TestEnsureIdempotent(t *testing.T) {
	root := map[string]any{}
	ensureBashHook(root, ccHookCommand)
	if ensureBashHook(root, ccHookCommand) {
		t.Fatal("second ensureBashHook should be a no-op")
	}
}

func TestEnsureAppendsToExistingBashMatcher(t *testing.T) {
	// A pre-existing Bash matcher with someone else's hook must be preserved.
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
	if !ensureBashHook(root, ccHookCommand) {
		t.Fatal("should append to existing Bash matcher")
	}
	pre := preToolUse(root)
	if len(pre) != 1 {
		t.Fatalf("should reuse the single Bash matcher, got %d entries", len(pre))
	}
	inner, _ := pre[0].(map[string]any)["hooks"].([]any)
	if len(inner) != 2 {
		t.Fatalf("existing hook should be kept alongside ours, got %d", len(inner))
	}
}

func TestRemoveCleansUpAndPreserves(t *testing.T) {
	root := map[string]any{"model": "opus"}
	ensureBashHook(root, ccHookCommand)
	if !removeBashHook(root, ccHookCommand) {
		t.Fatal("removeBashHook should report a change")
	}
	if _, ok := root["hooks"]; ok {
		t.Fatalf("empty hooks structure should be cleaned up: %+v", root)
	}
	if root["model"] != "opus" {
		t.Fatal("unrelated key lost on uninstall")
	}
	if removeBashHook(root, ccHookCommand) {
		t.Fatal("second removeBashHook should be a no-op")
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
	if !removeBashHook(root, ccHookCommand) {
		t.Fatal("should remove our hook")
	}
	inner, _ := preToolUse(root)[0].(map[string]any)["hooks"].([]any)
	if len(inner) != 1 || asString(inner[0].(map[string]any)["command"]) != "other-tool" {
		t.Fatalf("other tool's hook should remain: %+v", inner)
	}
}
