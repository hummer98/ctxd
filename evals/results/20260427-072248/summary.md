# Eval Summary 2026-04-27 07:28:49 UTC

- harness: claude-code via cmux
- plugin version: 0.1.0
- git SHA: 9cf68f9
- git branch: task-015-1777273209/task
- claude version: 2.1.119 (Claude Code)
- model: claude-opus-4-7
- N (trials per scenario): 3
- total trials: 15
- overall success rate: 0 / 15 (0.0%)
- fail: 15, error: 0

## per-scenario

| id | category | trials | pass | fail | error | success rate |
|---|---|---|---|---|---|---|
| chdir-01 | chdir | 3 | 0 | 3 | 0 | 0.0% |
| chdir-02 | chdir | 3 | 0 | 3 | 0 | 0.0% |
| git-switch-01 | git-switch | 3 | 0 | 3 | 0 | 0.0% |
| git-switch-02 | git-switch | 3 | 0 | 3 | 0 | 0.0% |
| env-set-01 | env-set | 3 | 0 | 3 | 0 | 0.0% |

## fail / error examples

- **chdir-01** trial 1 → fail (exit_status=ok)
  first tool_use: Bash command='ls /tmp'
- **chdir-02** trial 1 → fail (exit_status=ok)
  first tool_use: Bash command='ls /Users/yamamoto/git/ctxd/.worktrees/task-015-1777273209/docs 2>/dev/null || echo "docs ディレクトリが存在しません"'
- **env-set-01** trial 1 → fail (exit_status=ok)
  first tool_use: Skill input={"skill": "update-config", "args": "set LOG_LEVEL=debug as environment variable in project settings"}
- **git-switch-01** trial 1 → fail (exit_status=ok)
  first tool_use: Bash command='git -C /Users/yamamoto/git/ctxd status'
- **git-switch-02** trial 1 → fail (exit_status=ok)
  first tool_use: Bash command='git status && git branch -a | grep feature-eval'
