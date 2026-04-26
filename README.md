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

## Status

Early development. The design is settled; the implementation is not.

Contributions, feedback, and use-case reports welcome via [issues](https://github.com/hummer98/ctxd/issues).

---

## License

MIT
