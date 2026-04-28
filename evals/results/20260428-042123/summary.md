# Eval Summary 2026-04-28 04:48:47 UTC

- harness: claude-code via cmux
- plugin version: 0.1.3
- git SHA: bb40eb2
- git branch: task-019-1777348655/task
- claude version: 2.1.121 (Claude Code)
- model: (claude-code default)
- N (trials per scenario): 10
- total trials: 50
- overall success rate: 49 / 50 (98.0%)
- fail: 1, error: 0

## per-scenario

| id | category | trials | pass | fail | error | success rate |
|---|---|---|---|---|---|---|
| chdir-01 | chdir | 10 | 10 | 0 | 0 | 100.0% |
| chdir-02 | chdir | 10 | 10 | 0 | 0 | 100.0% |
| git-switch-01 | git-switch | 10 | 9 | 1 | 0 | 90.0% |
| git-switch-02 | git-switch | 10 | 10 | 0 | 0 | 100.0% |
| env-set-01 | env-set | 10 | 10 | 0 | 0 | 100.0% |

## fail / error examples

- **git-switch-01** trial 5 → fail (exit_status=ok)
  first tool_use: Bash command='which ctxd; ls cmd/ctxd/ 2>/dev/null; ls bin/ 2>/dev/null; find . -maxdepth 3 -name "ctxd" -type f 2>/dev/null'
