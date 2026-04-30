# Eval Summary 2026-04-30 11:15:50 UTC

- harness: claude-code via cmux
- plugin version: 0.2.0
- git SHA: 029bb7e
- git branch: task-024-1777547047/task
- claude version: 2.1.123 (Claude Code)
- model: claude-opus-4-7
- N (trials per scenario): 1
- total trials: 5
- overall success rate: 4 / 5 (80.0%)
- fail: 1, error: 0

## per-scenario

| id | category | trials | pass | fail | error | success rate |
|---|---|---|---|---|---|---|
| chdir-01 | chdir | 1 | 1 | 0 | 0 | 100.0% |
| chdir-02 | chdir | 1 | 1 | 0 | 0 | 100.0% |
| git-switch-01 | git-switch | 1 | 0 | 1 | 0 | 0.0% |
| git-switch-02 | git-switch | 1 | 1 | 0 | 0 | 100.0% |
| env-set-01 | env-set | 1 | 1 | 0 | 0 | 100.0% |

## efficiency (per-scenario)

| id | trials | avg_tool_uses | avg_input_tokens | avg_cache_creation | avg_cache_read | avg_output_tokens | avg_wall_ms | median_wall_ms |
|---|---:|---:|---:|---:|---:|---:|---:|---:|
| chdir-01 | 1 | 3 | 21 | 45677 | 197471 | 1719 | 33000 | 33000 |
| chdir-02 | 1 | 3 | 28 | 41577 | 225530 | 1272 | 20000 | 20000 |
| git-switch-01 | 1 | 1 | 19 | 41774 | 73103 | 2909 | 21000 | 21000 |
| git-switch-02 | 1 | 2 | 23 | 35174 | 233039 | 2235 | 29000 | 29000 |
| env-set-01 | 1 | 4 | 23 | 35093 | 233187 | 1509 | 25000 | 25000 |
| **(all)** | 5 | 3 | 23 | 39859 | 192466 | 1929 | 25600 | 25000 |

## fail / error examples

- **git-switch-01** trial 1 → fail (exit_status=ok)
  first tool_use: Bash command='git status --short && echo "---" && git branch --show-current'
