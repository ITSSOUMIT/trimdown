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
trimdown savings [--json]      show token savings
trimdown version

Global flags (before the tool):
  -v        verbose      --json   structured output
  -q        ultra-compact --raw   skip filtering for this run
```

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
