# ctxd

**English** | [日本語](README.ja.md)

**Declarative CLI commands that pass structured context to AI agents.**

`ctxd` wraps the shell operations that AI agents get lost in — `cd`, `export`, `git checkout` — and returns structured JSON so the agent knows exactly what changed.

```sh
# Before: silent, agent has to guess
cd /foo && git checkout main

# After: agent sees the state
ctxd chdir /foo
# {"ok":true,"cmd":"chdir","result":{"cwd":"/foo","git_branch":"main","listing":["src","docs","go.mod"]}}

ctxd git-switch main
# {"ok":true,"cmd":"git-switch","result":{"branch":"main","dirty":false,"ahead":0,"behind":0}}
```

---

## Why

### The silent CLI problem

Unix's "rule of silence" — successful commands produce no output — was designed for human operators who can perceive context implicitly. For AI agents, it is a systematic blind spot.

| Command | What it does silently | What the agent loses |
|---|---|---|
| `cd /foo` | Changes working directory | New cwd, git branch, file listing |
| `export FOO=bar` | Sets environment variable | Which variables changed, what their values are |
| `git checkout main` | Switches branch | Branch, dirty state, divergence from remote |
| `kill -STOP <pid>` | Pauses a process | Process state |
| `umask 022` | Changes file creation mask | Effective permissions for new files |

The agent's only recovery is to emit a follow-up command (`pwd`, `env`, `git status`) — burning extra tokens and an extra round-trip — or to reason from context, which drifts under long conversations.

### Transformers can't track sequential state

Mozer et al. ["The Topological Trouble With Transformers"](https://arxiv.org/abs/2604.17121) (2026) formalizes the issue: feedforward architectures cannot maintain evolving state across depth. The longer an agent session runs, the more it relies on external signals to reconstruct where it is.

This isn't a prompt-engineering problem. It's structural. The fix is structural too: **make the state external and machine-readable at the point of mutation.**

### Infrastructure did this already

Server orchestration faced the same problem in the 2010s — imperative shell scripts drifted, state was implicit, failures were opaque. The industry converged on Terraform, Kubernetes, and GitOps: declare intent, verify postconditions, report structured diffs.

`ctxd` applies that same pattern to the local shell, scoped to the operations AI agents use most.

---

## How it works

Every `ctxd` command:

1. **Executes** the underlying operation
2. **Observes** the resulting state
3. **Returns** a structured JSON report

```json
{
  "ok": true,
  "cmd": "chdir",
  "args": ["/foo"],
  "result": {
    "cwd": "/foo",
    "git_branch": "main",
    "listing": ["src", "docs", "go.mod", "README.md"]
  },
  "postcondition": { "passed": true, "checks": [] },
  "elapsed_ms": 4
}
```

On failure:

```json
{
  "ok": false,
  "cmd": "chdir",
  "args": ["/nonexistent"],
  "error": {
    "code": "path_not_found",
    "message": "no such file or directory: /nonexistent",
    "retryable": false
  }
}
```

### Postconditions

Declare what you expect the state to be after the command. `ctxd` verifies it and reports a clear pass/fail:

```sh
ctxd git-switch main --expect branch=main --expect dirty=false
```

```json
{
  "ok": true,
  "postcondition": {
    "passed": true,
    "checks": [
      {"key": "branch", "expected": "main", "actual": "main", "passed": true},
      {"key": "dirty",  "expected": "false","actual": "false","passed": true}
    ]
  }
}
```

---

## Commands (MVP)

| Command | Replaces | Key output fields |
|---|---|---|
| `ctxd chdir <path>` | `cd` | `cwd`, `git_branch`, `listing` |
| `ctxd git-switch <branch>` | `git checkout` / `git switch` | `branch`, `dirty`, `ahead`, `behind` |
| `ctxd env-set <KEY=val>…` | `export` | `set`, `diff.added`, `diff.changed` |

`--human` flag switches to human-readable output for debugging.

### `ctxd chdir`

Resolve a path, list its contents, and report the git branch (if any):

```sh
ctxd chdir /path/to/repo
```

```json
{
  "ok": true,
  "cmd": "chdir",
  "args": ["/path/to/repo"],
  "result": {
    "cwd": "/path/to/repo",
    "git_branch": "main",
    "listing": ["docs", "go.mod", "src"]
  },
  "elapsed_ms": 3
}
```

`git_branch` is `null` when the path is outside a git working tree or HEAD is detached.
Errors return `ok: false` with `error.code` of `not_found` (path missing) or `not_a_directory` (path is a file).

The parent shell's cwd is not modified — pass the resolved `cwd` to the next command instead.

### `ctxd git-switch`

Switch a git branch and report the resulting working tree state:

```sh
ctxd git-switch main
```

```json
{
  "ok": true,
  "cmd": "git-switch",
  "args": ["main"],
  "result": {
    "branch": "main",
    "dirty": false,
    "ahead": 0,
    "behind": 0
  },
  "elapsed_ms": 32
}
```

`branch` is `null` when HEAD is detached. `ahead` / `behind` are `0` when no upstream is configured.
On failure, `error.code` is one of `not_a_git_repo`, `branch_not_found`, `dirty_tree`, or `git_not_found`.

The parent shell's HEAD is updated (the switch is real) but cwd is not changed.

### `ctxd env-set`

Set one or more environment variables in this child process and report
the resulting set map and diff (added / changed):

```sh
ctxd env-set FOO=bar BAZ=qux
```

```json
{
  "ok": true,
  "cmd": "env-set",
  "args": ["FOO=bar", "BAZ=qux"],
  "result": {
    "set": {"FOO": "bar", "BAZ": "qux"},
    "diff": {
      "added": ["BAZ"],
      "changed": ["FOO"]
    }
  },
  "elapsed_ms": 1
}
```

`set` is the final KEY → value map of this invocation (last-write-wins
when the same KEY is repeated). `diff.added` lists keys that were not
previously in the environment; `diff.changed` lists keys whose value
differed from the previous value. Keys whose value is unchanged appear
in neither list.

The argument format is `KEY=VAL`. The first `=` is the separator, so
values may contain `=` (e.g. `URL=http://x?a=b`). An empty value
(`KEY=`) is valid. On failure, `error.code` is `invalid_args` (missing
`=`, empty `KEY`, or zero arguments) or `exec_failed` (the underlying
`os.Setenv` call failed).

The parent shell's environment is not modified — pass the resolved
`set` to the next command, or read the JSON to know which variables
the child process saw.

---

## Installation

> Work in progress. Binary releases coming soon.

**Claude Code Plugin** (recommended for Claude Code users):

```sh
claude plugins:install @hummer98/ctxd-claude-plugin
```

Installs both the `ctxd` binary and the Skill that teaches Claude to use it.

**Standalone CLI:**

```sh
npm install -g @hummer98/ctxd
# or
brew install hummer98/tap/ctxd
```

---

## Skill bundle

`ctxd` ships with a [SKILL.md](skills/ctxd/SKILL.md) compliant with the [Anthropic Agent Skills specification](https://agentskills.so/).

The skill does not enforce usage. It nudges: when the agent reaches for `cd`, `export`, or `git checkout`, the skill surfaces the `ctxd` equivalent and explains what context would be gained. Adoption is the agent's choice.

Compatible with Claude Code, OpenCode, Codex, Cursor, Gemini CLI, and any host that supports Agent Skills.

---

## Design principles

- **Don't shadow existing commands** — new command names only, no aliases over `cd`
- **JSON by default, human optional** — `--human` for readable output
- **Postconditions are opt-in** — useful when you want them, invisible when you don't
- **Narrow and deep** — top 20–30 commands done well, not full POSIX coverage
- **Pluggable** — users can add their own declarative wrappers

---

## Development

### Prerequisites

- Go 1.26 or later

### Build

```sh
go build -o ctxd ./cmd/ctxd
```

### Run

```sh
./ctxd --version
./ctxd --help
```

### Test

```sh
go test ./...
```

### Architecture decisions

See [`docs/adr/`](docs/adr/) for design decisions (CLI framework selection etc.).

---

## Eval harness

`evals/` contains a SKILL adherence harness that drives a real `claude` process inside an isolated `cmux` workspace and measures whether the agent reaches for `ctxd chdir` / `ctxd git-switch` / `ctxd env-set` when the SKILL says it should.

```sh
bash evals/run.sh
# or override the per-scenario trial count
EVAL_N=1 bash evals/run.sh
```

Each trial spins up `claude --settings <per-trial>.json` so a `Stop` hook
touches a sentinel file when the session ends, and a `PostToolUse` hook
appends each tool_use to `session-<id>-<trial>.tools.jsonl`. The runner
waits on the sentinel instead of scraping the screen, and `summarize.py`
reads tool_use from the hook JSONL first (falling back to the raw Claude
session JSONL if the hook output is empty).

Outputs land in `evals/results/<UTC-timestamp>/`:

- `session-<id>-<trial>.jsonl` — raw Claude Code session JSONL (one per trial, git-ignored)
- `session-<id>-<trial>.meta.json` — `exit_status`, wall time, session id (git-ignored)
- `session-<id>-<trial>.tools.jsonl` — PostToolUse hook output, one tool_use per line (git-ignored)
- `session-<id>-<trial>.done` — Stop hook sentinel marking session completion (git-ignored)
- `session-<id>-<trial>.settings.json` — per-trial `claude --settings` payload wiring the hooks (git-ignored)
- `summary.md` — overall and per-scenario success rate, plus the first failing tool_use quoted for context. Header records `plugin version`, `git SHA`, `git branch`, and `claude version` so each run is uniquely traceable.

Cross-run trend lives in `evals/results/index.md` and `evals/results/index.csv` (one row per run). Both are committed; the heavy JSONL / meta files are not — re-running the harness regenerates them.

The plugin version comes from `.claude-plugin/plugin.json` and acts as the canonical unit for comparing measurements. See [`CLAUDE.md`](CLAUDE.md) for the bump policy when SKILL.md changes.

Cost / time budget: each trial spends a few model cents. Default `EVAL_N=3` × 5 scenarios ≈ a handful of dimes to ~$1 and 5–10 minutes wall-clock, depending on the model claude-code resolves to. See [`evals/scenarios.jsonl`](evals/scenarios.jsonl) for the prompts and expected patterns.

`evals/.eval-plugin/` (plugin shim that wires `skills/ctxd` into the Skills loader) is git-ignored — the harness regenerates the shim on every run, dynamically writing the `version` from `.claude-plugin/plugin.json`.

### Adherence over plugin versions

How often the agent reaches for `ctxd` when the SKILL says it should, across plugin versions.
The figures below come from the `evals/run.sh` harness — per-scenario breakdown below.
All runs to date used **`claude-opus-4-7`** (resolved from claude-code's default at the time the runs were taken).

| plugin version | N | trials | overall | chdir | git-switch | env-set | notes |
|---|---:|---:|---:|---:|---:|---:|---|
| 0.1.0 | 3 | 15 | 0.0% | 0/6 | 0/6 | 0/3 | Initial baseline; hook-based harness landed (T013–T015) |
| 0.1.1 | 3 | 15 | 6.7% | 0/6 | 0/6 | 1/3 | SKILL.md trigger reinforced (description, ❌→✅ examples) (T016) |
| 0.1.2 | 3 | 15 | 53.3% | 5/6 | 1/6 | 2/3 | disambiguation + NEVER phrasing + Precondition section (T017) |
| 0.1.3 | 3 | 15 | 100.0% | 6/6 | 6/6 | 3/3 | pattern matcher tightened + scenario setup hooks + plugin author (T018) |
| 0.1.3 | 10 | 50 | 98.0% | 20/20 | 19/20 | 10/10 | Variance check at N=10 (T019) |

`N` is trials per scenario; `trials` is N × 5 scenarios. Each cell shows passes / trials for that command family.
The latest baseline lives in [`evals/results/index.md`](evals/results/index.md) — the table here is updated by hand, see [`CLAUDE.md`](CLAUDE.md).

---

## Status

Early development. The design is settled; the implementation is not.

Contributions, feedback, and use-case reports welcome via [issues](https://github.com/hummer98/ctxd/issues).

---

## License

MIT
