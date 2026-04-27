# Eval Summary 2026-04-27 15:59:37 UTC

- harness: claude-code via cmux
- plugin version: 0.1.2
- git SHA: e362a1d
- git branch: task-017-1777304859/task
- claude version: 2.1.119 (Claude Code)
- model: (claude-code default)
- N (trials per scenario): 1
- total trials: 1
- overall success rate: 0 / 1 (0.0%)
- fail: 1, error: 0

## per-scenario

| id | category | trials | pass | fail | error | success rate |
|---|---|---|---|---|---|---|
| chdir-01 | chdir | 1 | 0 | 1 | 0 | 0.0% |

## fail / error examples

- **chdir-01** trial 1 → fail (exit_status=ok)
  first tool_use: Bash command='ls /tmp'
