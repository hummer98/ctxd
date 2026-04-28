# Eval Summary 2026-04-27 12:08:01 UTC

- harness: claude-code via cmux
- plugin version: 0.1.1
- git SHA: 37d33d9
- git branch: task-016-1777290007/task
- claude version: 2.1.119 (Claude Code)
- model: claude-opus-4-7
- N (trials per scenario): 3
- total trials: 15
- overall success rate: 1 / 15 (6.7%)
- fail: 13, error: 1

## per-scenario

| id | category | trials | pass | fail | error | success rate |
|---|---|---|---|---|---|---|
| chdir-01 | chdir | 3 | 0 | 3 | 0 | 0.0% |
| chdir-02 | chdir | 3 | 0 | 3 | 0 | 0.0% |
| git-switch-01 | git-switch | 3 | 0 | 3 | 0 | 0.0% |
| git-switch-02 | git-switch | 3 | 0 | 3 | 0 | 0.0% |
| env-set-01 | env-set | 3 | 1 | 1 | 1 | 33.3% |

## fail / error examples

- **chdir-01** trial 1 → fail (exit_status=ok)
  first tool_use: Bash command='ls /tmp'
- **chdir-02** trial 1 → fail (exit_status=ok)
  first tool_use: Bash command='ls /Users/yamamoto/git/ctxd/.worktrees/task-016-1777290007/docs 2>/dev/null || echo "docs ディレクトリなし"'
- **env-set-01** trial 1 → error (exit_status=error)
  `exit_status=error` detail=timeout
- **env-set-01** trial 3 → fail (exit_status=ok)
  first tool_use: Skill input={"skill": "update-config", "args": "set LOG_LEVEL=debug as environment variable"}
- **git-switch-01** trial 1 → fail (exit_status=ok)
  first tool_use: Bash command='git status && echo "---" && git worktree list'
- **git-switch-02** trial 1 → fail (exit_status=ok)
  first tool_use: Bash command='git status'
