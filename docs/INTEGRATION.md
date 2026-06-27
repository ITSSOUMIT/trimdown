# Agent integration

trimdown only saves tokens if it sits in front of the commands your agent runs.
Install a hook once and covered commands compact automatically.

```sh
trimdown install claude-code            # project-local: ./.claude/settings.local.json
trimdown install claude-code --global   # user-wide:     ~/.claude/settings.json
trimdown agents                         # list supported agents
trimdown doctor                         # check status (both scopes)
trimdown uninstall claude-code [--global]
```

`install` is idempotent, backs up the target file to `*.bak`, and preserves
every unrelated setting (it only adds/removes its own hook entries).

## How it works

`trimdown install claude-code` wires **two** hooks (one command serves both):

**1. `PreToolUse` on `Bash` — command rewrite.** Before the agent runs a shell
command, Claude Code pipes it to `trimdown hook claude-code`, which rewrites it
via `hookSpecificOutput.updatedInput`:

```
git status            →  trimdown git status
go test ./...         →  trimdown go test ./...
cat x && git status   →  cat x && trimdown git status   (only the git part)
```

**2. `PostToolUse` on `Grep|Glob` — output compaction.** Claude Code's *native*
Grep/Glob tools don't run a shell command, so the rewrite hook can't reach them.
The PostToolUse hook instead caps long results and truncates wide lines via
`hookSpecificOutput.updatedToolOutput`, preserving the result's shape. (A
PreToolUse-only integration — including rtk — can't do this.) Large Read results
are left untouched by design: dropping file content the agent asked for is lossy.

The installed hooks look like:

```json
{
  "hooks": {
    "PreToolUse": [
      { "matcher": "Bash",
        "hooks": [ { "type": "command", "command": "trimdown hook claude-code" } ] }
    ],
    "PostToolUse": [
      { "matcher": "Grep|Glob",
        "hooks": [ { "type": "command", "command": "trimdown hook claude-code" } ] }
    ]
  }
}
```

> Debugging: set `TRIMDOWN_HOOK_LOG=/path/to/log` to append the raw hook payloads
> (useful for inspecting exact tool-result shapes).

## The safety boundary (command rewrite)

The rewriter (`trimdown rewrite "<cmd>"` to preview it) wraps **only** a
top-level simple command whose first word is a covered tool. It **never** wraps
a command segment that involves:

- command substitution `$(…)` or backticks — e.g. `FILES=$(git diff)` is left alone
- a pipe feeding another command — `git log | head`
- a redirect — `git diff > out.txt`
- a `VAR=value` assignment prefix
- a control-flow keyword or command runner (`if`, `for`, `sudo`, `env`, `xargs`, …)
- an interactive/streaming invocation — `git commit` without `-m`, `git rebase -i`,
  `git add -p`, `docker run -it`, `kubectl exec -it`, `kubectl logs -f`, `tail -f`
- anything already starting with `trimdown` (idempotent)

When in doubt it leaves the command untouched: a missed compaction is harmless,
a corrupted command is not. If the hook ever errors it emits a no-op, so the
original command/output always runs — trimdown can never break your shell.

## What is and isn't intercepted

The hooks match specific tools. The `Bash` rewrite catches shell commands; the
`Grep|Glob` PostToolUse hook catches those two native tools. **Other native
tools — `Read`, `Edit`, `Write`, and any MCP tools — are not touched.** If your
`permissions.allow` grants the native search tools (`Grep`, `Glob`, …), the agent
uses those directly; the Grep/Glob hook still compacts their output, but a native
`Read` is passed through as-is.

## Kill switch

Set `TRIMDOWN_DISABLE=1` to pass every command through unchanged without
uninstalling. `trimdown <tool> --raw` skips filtering for a single run.

## A note on allow-list re-prompts

Wrapping prepends `trimdown `, so a wrapped command no longer matches your
existing `Bash(...)` allow rules (e.g. `Bash(bundle exec *)` won't match
`trimdown bundle exec …`). Add a broad rule to avoid re-prompts:

```json
"Bash(trimdown *)"
```

## Why hooks only (not PATH shims or prompt rules)

trimdown deliberately supports only the safe, deterministic interception tier.

- **PATH shims** (a `git` shim on `PATH`) are rejected: they also fire inside
  `FILES=$(git diff --name-only)`, corrupting output a script parses; they risk
  recursion; and they don't exist natively on Windows.
- **Prompt-level rules files** (telling the model to prefer `trimdown`) are
  rejected: they're non-deterministic.
- **Hooks** see the whole command line / the tool result, apply the safety
  boundary, and fire deterministically — so they're the only mechanism we use.

## Supported agents

| Agent | Mechanism | Config file |
|---|---|---|
| Claude Code | `PreToolUse` Bash rewrite + `PostToolUse` Grep/Glob compaction | `./.claude/settings.local.json` (project) or `~/.claude/settings.json` (`--global`) |

## Measuring the savings

The hook path records every command to the usage log:

```sh
trimdown savings            # coverage, savings rate, top savers, trend
trimdown savings --all      # + daily/weekly/monthly breakdowns
trimdown savings --json     # structured
```
