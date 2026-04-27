# Eval Summary 2026-04-27 07:21:49 UTC

- harness: claude-code via cmux
- plugin version: 0.1.0
- git SHA: 9cf68f9
- git branch: task-015-1777273209/task
- claude version: 2.1.119 (Claude Code)
- model: (claude-code default)
- N (trials per scenario): 1
- total trials: 5
- overall success rate: 0 / 5 (0.0%)
- fail: 5, error: 0

## per-scenario

| id | category | trials | pass | fail | error | success rate |
|---|---|---|---|---|---|---|
| chdir-01 | chdir | 1 | 0 | 1 | 0 | 0.0% |
| chdir-02 | chdir | 1 | 0 | 1 | 0 | 0.0% |
| git-switch-01 | git-switch | 1 | 0 | 1 | 0 | 0.0% |
| git-switch-02 | git-switch | 1 | 0 | 1 | 0 | 0.0% |
| env-set-01 | env-set | 1 | 0 | 1 | 0 | 0.0% |

## fail / error examples

- **chdir-01** trial 1 → fail (exit_status=ok)
  first tool_use: Bash command='ls /tmp'
- **chdir-02** trial 1 → fail (exit_status=ok)
  first tool_use: Bash command='ls /Users/yamamoto/git/ctxd/.worktrees/task-015-1777273209/docs 2>/dev/null || echo "docs ディレクトリが存在しません"'
- **env-set-01** trial 1 → fail (exit_status=ok)
  first tool_use: Bash command='git diff HEAD -- evals/lib/build_settings.sh evals/lib/hook-tool-use.py evals/tests/fixtures/sample-tools-pass.jsonl evals/tests/test_hook_tool_use.py evals/lib/claude_session.sh evals/run.sh evals/summarize.py evals/tests/test_summarize.py .gitignore 2>/dev/null | head -200'
- **git-switch-01** trial 1 → fail (exit_status=ok)
  first tool_use: Bash command='git status && echo "---" && git log --oneline -5'
- **git-switch-02** trial 1 → fail (exit_status=ok)
  first tool_use: Bash command='git status && git branch -a | grep feature-eval'
