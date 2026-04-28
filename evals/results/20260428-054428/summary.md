# Eval Summary 2026-04-28 05:53:09 UTC

- harness: claude-code via cmux
- plugin version: 0.1.3
- git SHA: cf50a94
- git branch: task-021-1777353851/task
- claude version: 2.1.121 (Claude Code)
- model: (claude-code default)
- N (trials per scenario): 3
- total trials: 15
- overall success rate: 14 / 15 (93.3%)
- fail: 1, error: 0

## per-scenario

| id | category | trials | pass | fail | error | success rate |
|---|---|---|---|---|---|---|
| chdir-01 | chdir | 3 | 3 | 0 | 0 | 100.0% |
| chdir-02 | chdir | 3 | 2 | 1 | 0 | 66.7% |
| git-switch-01 | git-switch | 3 | 3 | 0 | 0 | 100.0% |
| git-switch-02 | git-switch | 3 | 3 | 0 | 0 | 100.0% |
| env-set-01 | env-set | 3 | 3 | 0 | 0 | 100.0% |

## fail / error examples

- **chdir-02** trial 1 → fail (exit_status=ok)
  first tool_use: Bash command='ls -la /Users/yamamoto/git/ctxd/.worktrees/task-021-1777353851/ | grep -E "^d" | awk \'{print $NF}\''
