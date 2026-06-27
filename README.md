# trimdown

**A CLI proxy that compresses tool output to cut LLM token consumption 60–90%.**

`trimdown` sits between an AI coding agent and your shell. It runs the real
command, then filters and compacts the output — failures-only for test runners,
grouped diagnostics for linters, stat+hunks for diffs, schemas for JSON — before
it reaches the model's context. A single static Go binary, no dependencies.

```sh
trimdown go test ./...      # → "12 passed, 1 failed" + only the failing test
trimdown git status         # → compact branch header + porcelain entries
trimdown eslint .           # → errors grouped by rule, not a wall of output
trimdown savings            # → how many tokens you've saved
```

## Why it's built this way (vs. rtk)

trimdown is a ground-up redesign, not a clone:

- **Self-registering filter registry** — adding a tool is one file, no central switch.
- **Parse → typed IR → Render** — one renderer for all tools, and a free `--json` mode.
- **Reusable reducers** — failures-only / group-by / cap written once, shared.
- **One filter interface** for both native Go parsers and declarative YAML specs.
- **Append-only NDJSON event log** for usage tracking — lock-free, zero-dep (no SQLite).
- **Real BPE tokenizer** for credible savings numbers (not `len/4`).
- **Fail-safe**: if a filter errors, you get raw output — it never breaks a command.
- **Exit-code fidelity**: the underlying tool's exit code always propagates (CI-safe).

## Coverage

~70 tools across 9 ecosystems. Complex output gets a **native** parser; simple
line-strip tools are **declarative** YAML specs (`internal/declarative/specs/`).

| Group | Tools |
|---|---|
| VCS | git, gh, glab, gt, jj, yadm |
| Go | go test/build/vet, golangci-lint |
| Python | pytest, ruff, mypy, pip, uv, poetry |
| Ruby | rake, rspec, rubocop, bundle |
| JS/TS | tsc, eslint, jest, vitest, playwright, prettier, npm, npx, pnpm, next, prisma, biome, turbo, nx |
| Cloud/DB | aws, docker, kubectl, oc, psql, curl, terraform, tofu, pulumi, helm, gcloud, ansible, … |
| System | read, grep, ls, tree, find, diff, json, log, env, df, du, ps, … |

Unknown tools/subcommands pass through unfiltered (and are still tracked).

## Install

```sh
# Homebrew (macOS/Linux)
brew install itssoumit/tap/trimdown

# Script
curl -fsSL https://raw.githubusercontent.com/itssoumit/trimdown/main/packaging/install.sh | sh

# Go
go install github.com/itssoumit/trimdown/cmd/trimdown@latest
```

Native installers (`.dmg`/`.pkg`, `.deb`/`.rpm`, Scoop) ship on each release.

## Usage

```
trimdown <tool> [args...]      run a tool with compacted output
trimdown passthrough <cmd>     run unfiltered, but record usage
trimdown savings [--all]       analytics (see below); --all adds time breakdowns
trimdown version

Global flags (before the tool):
  -v        verbose      --json   structured output
  -q        ultra-compact --raw   skip filtering for this run
```

## Agent integration

Run one command and your agent's shell commands compact automatically — no need
to type `trimdown` yourself:

```sh
trimdown install claude-code            # project-local (./.claude/settings.local.json)
trimdown install claude-code --global   # user-wide   (~/.claude/settings.json)
trimdown agents                         # list supported agents
trimdown doctor                         # check integration status
trimdown uninstall claude-code          # remove it
```

This installs a **`PreToolUse` command-rewrite hook**: before the agent runs a
Bash command, trimdown rewrites covered commands (e.g. `git status` →
`trimdown git status`) and leaves everything else untouched. The rewrite is
**context-aware and safe** — it never wraps a command inside `$(…)`, a pipe, a
redirect, an assignment, or an interactive/streaming command (`git commit`
without `-m`, `docker run -it`, `kubectl logs -f`, …). Kill switch:
`TRIMDOWN_DISABLE=1`. See [docs/INTEGRATION.md](docs/INTEGRATION.md) for details
and why hooks (not PATH shims or prompt rules) are the only mechanism supported.

## Savings analytics

`trimdown savings` is built around what's actually actionable, not vanity totals:

- **Coverage** — how many commands trimdown actually intercepted vs ran raw.
- **Value** — tokens saved translated to **dollars** and **context windows freed**
  (configurable via `TRIMDOWN_PRICE_PER_MTOK`, default `3.0`, and
  `TRIMDOWN_CONTEXT_TOKENS`, default `200000`).
- **Top savers** — where it earns its keep (per-command impact bars).
- **Untapped** — commands that ran raw with no filter, ranked by token volume.
  trimdown *measures* even what it can't yet compress (lossless tee), so this
  tells you exactly which filter to build next.
- **⚠ Parse failures** — filters that fell back to raw (a regression signal).
- **Trend** — a sparkline of recent savings + week-over-week delta.

`--all` adds daily/weekly/monthly breakdowns; `--json` emits the full structure;
`-p` scopes to the current project; `--since 24h` limits the window.

## Develop

```sh
make build        # CGO-free binary
make test         # go test -race ./...
make crosscheck   # cross-compile all release targets
make snapshot     # build all release artifacts locally (needs goreleaser)
```

Add a strip-class tool: drop a YAML spec in `internal/declarative/specs/` with
inline `tests:`. Add a structured tool: implement `registry.Filter` in
`internal/filters/<group>/` and blank-import it in `internal/allfilters`.

## License

Apache-2.0
