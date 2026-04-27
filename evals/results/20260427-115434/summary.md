# Eval Summary 2026-04-27 11:56:57 UTC

- harness: claude-code via cmux
- plugin version: 0.1.1
- git SHA: 37d33d9
- git branch: task-016-1777290007/task
- claude version: 2.1.119 (Claude Code)
- model: (claude-code default)
- N (trials per scenario): 1
- total trials: 5
- overall success rate: 1 / 5 (20.0%)
- fail: 4, error: 0

## per-scenario

| id | category | trials | pass | fail | error | success rate |
|---|---|---|---|---|---|---|
| chdir-01 | chdir | 1 | 0 | 1 | 0 | 0.0% |
| chdir-02 | chdir | 1 | 0 | 1 | 0 | 0.0% |
| git-switch-01 | git-switch | 1 | 0 | 1 | 0 | 0.0% |
| git-switch-02 | git-switch | 1 | 0 | 1 | 0 | 0.0% |
| env-set-01 | env-set | 1 | 1 | 0 | 0 | 100.0% |

## fail / error examples

- **chdir-01** trial 1 → fail (exit_status=ok)
  first tool_use: Bash command='ls /tmp'
- **chdir-02** trial 1 → fail (exit_status=ok)
  first tool_use: Bash command='ls /Users/yamamoto/git/ctxd/.worktrees/task-016-1777290007/docs 2>/dev/null || echo "docs ディレクトリが存在しません"'
- **git-switch-01** trial 1 → fail (exit_status=ok)
  first tool_use: Bash command='git status && echo "---" && git diff && echo "---" && git diff --cached'
- **git-switch-02** trial 1 → fail (exit_status=ok)
  first tool_use: Bash command='git status && git branch -a | grep feature-eval'
