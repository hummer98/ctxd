# Eval Summary 2026-04-27 04:11:14 UTC

- harness: claude-code via cmux
- plugin version: 0.1.0
- git SHA: 7a95621
- git branch: task-014-1777261475/task
- claude version: 2.1.119 (Claude Code)
- model: (claude-code default)
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
  no tool_use; assistant text: ''
- **chdir-02** trial 1 → fail (exit_status=ok)
  no tool_use; assistant text: ''
- **env-set-01** trial 1 → fail (exit_status=ok)
  no tool_use; assistant text: ''
- **git-switch-01** trial 1 → fail (exit_status=ok)
  no tool_use; assistant text: ''
- **git-switch-02** trial 1 → fail (exit_status=ok)
  no tool_use; assistant text: ''
