---
name: ctxd
description: MUST be used instead of running raw `cd`, `export`, `unset`, `git checkout`, or `git switch` in Bash. Wraps shell state mutations with declarative ctxd commands (`ctxd chdir`, `ctxd env-set`, `ctxd git-switch`) that emit structured JSON describing cwd, environment, and git state in one observation. Trigger when the user asks to change directory, switch git branches, or set environment variables for the running task — not for Claude Code settings, hooks, or permissions (use `update-config` for those). Examples - "cd to /tmp", "switch to main branch", "set LOG_LEVEL=debug as env var", "環境変数にセット", "ブランチに切り替え", "ディレクトリに移って".
license: Apache-2.0
metadata:
  project: ctxd
  source: https://github.com/hummer98/ctxd
---

## Precondition: do not bypass with raw Bash

Before running any of the following in `Bash`, **stop and use the matching `ctxd` subcommand instead**:

| Raw Bash (do NOT use) | Use this instead |
|---|---|
| `cd <path>` | `ctxd chdir <path>` |
| `export KEY=val`, `unset KEY` | `ctxd env-set KEY=val` |
| `git checkout <branch>`, `git switch <branch>` | `ctxd git-switch <branch>` |

This applies even when the task seems trivial (e.g. "just `cd /tmp` and `ls`"). The point is the structured JSON observation, not the side effect: running raw `cd && ls` forces an extra reasoning step on the agent and loses branch / env context that ctxd would have surfaced for free.

Out of scope (use plain `Bash`): read-only inspection (`ls`, `cat`, `git status`, `git log`), commands without a matching ctxd subcommand, and Claude Code harness configuration (`settings.json`, hooks, permissions — those belong to the `update-config` skill, not this one).

## When to use ctxd

`ctxd` is a declarative companion to common shell state mutations. It does not replace POSIX; it runs alongside `Bash` and returns one structured JSON payload that captures the resulting state in a single observation. Reach for it whenever a step would otherwise mutate process state silently and force the agent to reason about the outcome from scratch.

Prefer the matching `ctxd` command in these situations:

- About to run `cd <path>` → use `ctxd chdir <path>` to also see `cwd`, `git_branch`, and a directory listing in one JSON payload.
- About to run `export KEY=val` or `unset KEY` → use `ctxd env-set KEY=val` to observe the diff against the prior environment (added vs changed keys).
- About to run `git checkout <branch>` or `git switch <branch>` → use `ctxd git-switch <branch>` to confirm the resulting `branch`, `dirty`, `ahead`, and `behind` in one shot.
- A task asks the agent to verify directory, env-var, or branch state after a change.
- The next planning step needs to read the post-mutation state (e.g. "after switching, list the staged files" — start from `result.dirty`).

This skill is **the required path** for `cd`, `export`/`unset`, and `git checkout`/`git switch`. Falling back to raw `Bash(cd ...)` is reserved for cases where ctxd genuinely has no matching subcommand (e.g. `pushd`, `popd`, `git rebase`). When in doubt, prefer the ctxd subcommand.

## Output contract

Every `ctxd` command returns the same envelope:

```json
{
  "ok": true,
  "cmd": "<name>",
  "args": ["..."],
  "result": { "...": "command-specific payload" },
  "elapsed_ms": 3
}
```

- Successful runs always have `ok: true` and place the command-specific payload under `result`.
- Failures set `ok: false`, omit `result`, and populate `error.code`, `error.message`, and `error.retryable`:

```json
{
  "ok": false,
  "cmd": "<name>",
  "args": ["..."],
  "error": {
    "code": "<error_code>",
    "message": "...",
    "retryable": false
  },
  "elapsed_ms": 4
}
```

- `args` is always a JSON array (even with zero arguments).
- `cmd` is the canonical CLI name (`chdir`, `git-switch`, `env-set`).
- `elapsed_ms` is wall-clock duration of the command in milliseconds.
- The next decision should be driven by keys under `result` (or `error` on failure), not by parsing prose from shell output.

## chdir — replace `cd`

About to `cd /repo/src`? Run `ctxd chdir /repo/src` to land in the directory and observe its contents and git branch in one JSON payload.

```bash
ctxd chdir /repo/src
```

**❌ Wrong** (loses branch + listing context):

```bash
cd /tmp && ls
```

**✅ Correct** (one structured observation):

```bash
ctxd chdir /tmp
```

The `result.listing` already contains the directory entries — no need to chain `&& ls` afterwards.

Expected JSON:

```json
{
  "ok": true,
  "cmd": "chdir",
  "args": ["/repo/src"],
  "result": {
    "cwd": "/repo/src",
    "git_branch": "main",
    "listing": ["README.md", "cmd", "internal", "go.mod"]
  },
  "elapsed_ms": 3
}
```

What to do with the output:

- `result.cwd` is the resolved absolute working directory — use it as the canonical path in subsequent steps.
- `result.git_branch` is a string when the directory is inside a git repository on a named branch, and `null` for detached HEAD or non-repo directories. Treat `null` as "no branch context to assume".
- `result.listing` is a directory listing equivalent to `ls` — consult it before deciding whether to read or create files.

Note: ctxd runs in a child process, so the parent shell's `cwd` is not modified. Pass the same path again on the next `ctxd` invocation, or rely on the agent's own working-directory tracking.

## git-switch — replace `git checkout` / `git switch`

About to `git switch feature-x`? Run `ctxd git-switch feature-x` to verify the branch landed cleanly and see the working-tree state in one payload.

```bash
ctxd git-switch feature-x
```

**❌ Wrong** (forces a separate `git status` to see the working tree):

```bash
git switch main && git status
```

**✅ Correct** (`dirty`, `ahead`, `behind` come back in one payload):

```bash
ctxd git-switch main
```

Expected JSON:

```json
{
  "ok": true,
  "cmd": "git-switch",
  "args": ["feature-x"],
  "result": {
    "branch": "feature-x",
    "dirty": false,
    "ahead": 0,
    "behind": 2
  },
  "elapsed_ms": 18
}
```

What to do with the output:

- Confirm `result.branch` matches the requested target. A `null` value means detached HEAD.
- Check `result.dirty == false` before committing or running formatters that assume a clean tree.
- Use `result.ahead` and `result.behind` to decide whether to push, pull, or rebase against the upstream.

Failure example — branch does not exist:

```json
{
  "ok": false,
  "cmd": "git-switch",
  "args": ["typo"],
  "error": {
    "code": "branch_not_found",
    "message": "fatal: invalid reference: typo",
    "retryable": false
  },
  "elapsed_ms": 12
}
```

Common `error.code` values include `git_not_found`, `not_a_git_repo`, `branch_not_found`, `dirty_tree`, and `exec_failed`. Branch name typos surface as `branch_not_found`; an uncommitted local change blocking the switch surfaces as `dirty_tree`.

## env-set — replace `export`

About to `export DATABASE_URL=postgres://localhost/foo`? Run `ctxd env-set DATABASE_URL=postgres://localhost/foo` so the diff against the prior environment is logged.

```bash
ctxd env-set DATABASE_URL=postgres://localhost/foo LOG_LEVEL=debug
```

**❌ Wrong** (no diff, parent shell un-mutated anyway):

```bash
export LOG_LEVEL=debug
```

**❌ Also wrong** (this skill is for the running task's env vars, NOT for Claude Code's `settings.json`):

> Skill: `update-config` with `set LOG_LEVEL=debug as environment variable`

`update-config` is for Claude Code's harness configuration (settings.json, hooks, permissions). For the running task's shell environment variables, use `ctxd env-set` instead.

**✅ Correct** (one envelope, with `diff.added` listing new keys):

```bash
ctxd env-set LOG_LEVEL=debug
```

Expected JSON:

```json
{
  "ok": true,
  "cmd": "env-set",
  "args": ["DATABASE_URL=postgres://localhost/foo", "LOG_LEVEL=debug"],
  "result": {
    "set": {
      "DATABASE_URL": "postgres://localhost/foo",
      "LOG_LEVEL": "debug"
    },
    "diff": {
      "added": ["DATABASE_URL"],
      "changed": ["LOG_LEVEL"]
    }
  },
  "elapsed_ms": 1
}
```

What to do with the output:

- `result.set` echoes the keys and values that ctxd wrote — treat it as the authoritative record of what was applied.
- `result.diff.added` lists keys that did not exist in the prior environment.
- `result.diff.changed` lists keys whose value differs from the prior environment.
- Both diff arrays are sorted; an empty array means "nothing in that category" (not "unknown").

Heads-up: the parent shell's environment is not modified — the assignments live in this child process only. Pass the same `KEY=val` on the next `ctxd` invocation, or rely on the agent's own env-passing mechanism (see `docs/seed.md` "parent shell 問題への扱い").

Multiple arguments are accepted in a single call; values may contain `=` (the first `=` is the split point), which lets URLs and DSNs pass through unmolested.

## Postcondition assertions (`--expect`)

All ctxd subcommands accept a repeatable `--expect KEY=VALUE` flag for asserting state after the command runs.

> Note: the postcondition DSL is finalized in T10. The MVP accepts `--expect` flags but the verifier is currently a NoOp (`postcondition.passed` is always `true`, `checks` is always `[]`). The example below shows the *intended shape*; the left-hand key resolution and right-hand value syntax may change in T10.

Intended usage (subject to change):

```bash
ctxd chdir /repo --expect cwd=/repo --expect git_branch=main
```

Once T10 lands, the envelope will include a `postcondition` object alongside `result`:

```json
{
  "ok": true,
  "cmd": "chdir",
  "args": ["/repo"],
  "result": { "cwd": "/repo", "git_branch": "main", "listing": ["..."] },
  "postcondition": {
    "passed": true,
    "checks": []
  },
  "elapsed_ms": 5
}
```

Until T10 finalizes the DSL, treat `--expect` as a hint to the agent only; do not rely on its return value to gate a workflow.

## Tone & non-goals

- ctxd is a **declarative companion** to POSIX. `Bash(cd ...)`, `export`, `git switch` etc. still execute, but the agent **MUST** prefer the matching ctxd subcommand whenever one exists, because ctxd's structured observation removes a reasoning step the agent would otherwise have to do manually.
- Fallback to raw shell is allowed **only when ctxd has no matching subcommand** (e.g. `pushd`, `popd`, `git rebase`). Read-only inspection (`ls`, `cat`, `git status`) is also out of scope per the Precondition section above.
- ctxd runs every command in a child process. **The parent shell's `cwd` and environment are never modified** (see `docs/seed.md` "parent shell 問題への扱い"). Treat each invocation as the source of truth for the state it just observed, and re-pass arguments on the next call as needed.
- ctxd does not require network access; all commands are local and side effects stay inside the child process.
- Out of scope: read-only inspection that doesn't mutate state (`ls`, `cat`, `git status`), long-running processes (`npm start`, `go run ./...`), and any command without a matching ctxd subcommand. Use plain `Bash` for those.
