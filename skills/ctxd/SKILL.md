---
name: ctxd
description: Wraps shell state mutations (cd, export, unset, git checkout, git switch) with declarative ctxd commands that emit structured JSON describing cwd, environment, and git state. Use whenever the agent is about to run cd, export/unset, or git checkout/switch in Bash and needs to observe the resulting cwd, env diff, or branch state, or when a task asks to verify directory, env-var, or branch changes.
license: Apache-2.0
metadata:
  project: ctxd
  source: https://github.com/hummer98/ctxd
  version: "0.1.0-draft"
---

## When to use ctxd

`ctxd` is a declarative companion to common shell state mutations. It does not replace POSIX; it runs alongside `Bash` and returns one structured JSON payload that captures the resulting state in a single observation. Reach for it whenever a step would otherwise mutate process state silently and force the agent to reason about the outcome from scratch.

Prefer the matching `ctxd` command in these situations:

- About to run `cd <path>` → use `ctxd chdir <path>` to also see `cwd`, `git_branch`, and a directory listing in one JSON payload.
- About to run `export KEY=val` or `unset KEY` → use `ctxd env-set KEY=val` to observe the diff against the prior environment (added vs changed keys).
- About to run `git checkout <branch>` or `git switch <branch>` → use `ctxd git-switch <branch>` to confirm the resulting `branch`, `dirty`, `ahead`, and `behind` in one shot.
- A task asks the agent to verify directory, env-var, or branch state after a change.
- The next planning step needs to read the post-mutation state (e.g. "after switching, list the staged files" — start from `result.dirty`).

This skill is a nudge, not a guard. `Bash(cd ...)` is still legal; ctxd just lets the agent skip an extra observation step when the structured payload would be more useful than free-form shell output.

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

- ctxd is a **declarative companion** to POSIX, not a replacement. `Bash(cd ...)` and friends keep working; ctxd is preferred when a structured observation would save the agent an extra step.
- The skill is a **nudge by preference, not a guard**. The agent is free to fall back to raw shell when ctxd has no matching command.
- ctxd runs every command in a child process. **The parent shell's `cwd` and environment are never modified** (see `docs/seed.md` "parent shell 問題への扱い"). Treat each invocation as the source of truth for the state it just observed, and re-pass arguments on the next call as needed.
- ctxd does not require network access; all commands are local and side effects stay inside the child process.
- Out of scope: read-only inspection that doesn't mutate state (`ls`, `cat`, `git status`), long-running processes (`npm start`, `go run ./...`), and any command without a matching ctxd subcommand. Use plain `Bash` for those.
