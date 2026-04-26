#!/usr/bin/env bash
# T1: encode_cwd() が ~/.claude/projects/ の実値と一致することを確認する
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
LIB_DIR="$(cd "$SCRIPT_DIR/../lib" && pwd)"

# shellcheck source=../lib/encode_path.sh
source "$LIB_DIR/encode_path.sh"

failed=0

assert_eq() {
  local label="$1" expected="$2" actual="$3"
  if [[ "$expected" == "$actual" ]]; then
    printf '  ok   %s\n' "$label"
  else
    printf '  FAIL %s\n    expected: %s\n    actual:   %s\n' "$label" "$expected" "$actual"
    failed=$((failed + 1))
  fi
}

# plan §2.5 の表 (実機検証済の 3 例)
assert_eq "repo root" \
  "-Users-yamamoto-git-ctxd" \
  "$(encode_cwd "/Users/yamamoto/git/ctxd")"

assert_eq "worktree path" \
  "-Users-yamamoto-git-ctxd--worktrees-task-013-1777208046" \
  "$(encode_cwd "/Users/yamamoto/git/ctxd/.worktrees/task-013-1777208046")"

assert_eq "another repo" \
  "-Users-yamamoto-git-ai-code-agent-hub" \
  "$(encode_cwd "/Users/yamamoto/git/ai-code-agent-hub")"

# 連続する /. が -- になることの追加保証
assert_eq "trailing dot" \
  "-tmp-foo--bar" \
  "$(encode_cwd "/tmp/foo/.bar")"

# 加えて、worktree path の encoded が実際に ~/.claude/projects/ に存在することを確認
ENCODED="$(encode_cwd "/Users/yamamoto/git/ctxd/.worktrees/task-013-1777208046")"
PROJECTS_DIR="$HOME/.claude/projects/$ENCODED"
if [[ -d "$PROJECTS_DIR" ]]; then
  printf '  ok   live encoded dir exists: %s\n' "$PROJECTS_DIR"
else
  printf '  WARN encoded dir not found (skipped): %s\n' "$PROJECTS_DIR"
fi

if (( failed > 0 )); then
  printf 'test_encode_path.sh: %d FAILED\n' "$failed" >&2
  exit 1
fi
printf 'test_encode_path.sh: all green\n'
