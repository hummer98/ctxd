# Eval Summary 2026-04-27 16:13:53 UTC

- harness: claude-code via cmux
- plugin version: 0.1.2
- git SHA: e362a1d
- git branch: task-017-1777304859/task
- claude version: 2.1.119 (Claude Code)
- model: (claude-code default)
- N (trials per scenario): 3
- total trials: 15
- overall success rate: 8 / 15 (53.3%)
- fail: 6, error: 1

## per-scenario

| id | category | trials | pass | fail | error | success rate |
|---|---|---|---|---|---|---|
| chdir-01 | chdir | 3 | 2 | 1 | 0 | 66.7% |
| chdir-02 | chdir | 3 | 3 | 0 | 0 | 100.0% |
| git-switch-01 | git-switch | 3 | 1 | 2 | 0 | 33.3% |
| git-switch-02 | git-switch | 3 | 0 | 3 | 0 | 0.0% |
| env-set-01 | env-set | 3 | 2 | 0 | 1 | 66.7% |

## fail / error examples

- **chdir-01** trial 3 → fail (exit_status=ok)
  first tool_use: Skill input={"skill": "ctxd-eval:ctxd", "args": "cd /tmp"}
- **env-set-01** trial 3 → error (exit_status=error)
  `exit_status=error` detail=timeout
- **git-switch-01** trial 2 → fail (exit_status=ok)
  first tool_use: Skill input={"skill": "ctxd-eval:ctxd", "args": "git checkout main"}
- **git-switch-02** trial 1 → fail (exit_status=ok)
  first tool_use: Bash command='git status'
