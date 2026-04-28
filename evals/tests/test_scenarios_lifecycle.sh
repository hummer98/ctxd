#!/usr/bin/env bash
# evals/tests/test_scenarios_lifecycle.sh — T021
#
# scenarios.jsonl の lifecycle field (setup / teardown) と、run.sh で使う
# `cd $REPO && bash -c "$cmd"` パターンの実行可否を確認する。
#
# 範囲:
#   - git-switch-02 が `teardown` field を持ち、期待文字列に一致する
#   - 一時 git repo 上で setup/teardown シェル断片が想定通り動く
#   - run.sh に tools_missing 検知ブロックが含まれている (Step 7 sanity)
#
# 注意: 他 scenario に teardown が無いことの assert は、新シナリオ追加時に
# 脆くなるため意図的に行わない (Conductor Recommendation #5)。

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
EVAL_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
SCENARIOS="$EVAL_DIR/scenarios.jsonl"
RUN_SH="$EVAL_DIR/run.sh"

failed=0
assert() {
  local label="$1"; shift
  if "$@"; then
    echo "  ok   $label"
  else
    echo "  FAIL $label" >&2
    failed=$((failed + 1))
  fi
}

# ---------- 1. git-switch-02 の teardown ----------
expected_teardown='git branch -D feature-eval 2>/dev/null || true'
actual_teardown=$(jq -r 'select(.id == "git-switch-02") | .teardown // ""' < "$SCENARIOS")
assert "git-switch-02 has expected teardown" \
  test "$actual_teardown" = "$expected_teardown"

# ---------- 2. setup/teardown が一時 git repo 上で動く ----------
TMPDIR_REPO=$(mktemp -d)
trap 'rm -rf "$TMPDIR_REPO"' EXIT
(
  cd "$TMPDIR_REPO"
  git init -q .
  git config user.email "test@example.com"
  git config user.name "Test"
  git commit -q --allow-empty -m initial
) >/dev/null

setup_cmd='git branch ephemeral-test-branch'
teardown_cmd='git branch -D ephemeral-test-branch'

# setup
( cd "$TMPDIR_REPO" && bash -c "$setup_cmd" ) >/dev/null
assert "setup created branch" \
  test -n "$(git -C "$TMPDIR_REPO" branch --list ephemeral-test-branch)"

# teardown
( cd "$TMPDIR_REPO" && bash -c "$teardown_cmd" ) >/dev/null
assert "teardown removed branch" \
  test -z "$(git -C "$TMPDIR_REPO" branch --list ephemeral-test-branch)"

# 失敗許容パターンが exit 0 を返すこと (no-op で || true)
( cd "$TMPDIR_REPO" && bash -c 'git branch -D nonexistent 2>/dev/null || true' )
assert "teardown swallows nonexistent branch error" true

# ---------- 3. run.sh に tools_missing 検知ブロック (Step 7 sanity) ----------
assert "run.sh references tools_missing" \
  grep -q 'tools_missing' "$RUN_SH"
assert "run.sh references check_tools_missing.py" \
  grep -q 'check_tools_missing.py' "$RUN_SH"

# ---------- 4. run.sh の trial 末尾に teardown ブロックが追加されている ----------
assert "run.sh references teardown jq" \
  grep -q '\.teardown' "$RUN_SH"

# ---------- 5. run.sh が元ブランチを保存して teardown 前に switch -f で戻す ----------
# T021 fix: ctxd git-switch が worktree HEAD を動かすため、teardown 前に元ブランチへ
# 復帰しないと `git branch -D feature-eval` が「現在 checkout 中ブランチ削除」となり失敗する。
assert "run.sh saves original branch before setup" \
  grep -q 'original_branch=' "$RUN_SH"
assert "run.sh restores original branch with switch -f" \
  grep -q 'switch -f' "$RUN_SH"

# ---------- 6. T022: run.sh が EVAL_MODEL を default 付きで定義し --model に伝える ----------
# モデルを明示記録するため (1) EVAL_MODEL 環境変数で受け取る、(2) claude 起動コマンドに
# --model $EVAL_MODEL を渡す、(3) summarize.py に --model $EVAL_MODEL を渡す。
assert "run.sh defines EVAL_MODEL with default" \
  grep -q 'EVAL_MODEL="\${EVAL_MODEL:-' "$RUN_SH"
assert "run.sh passes --model to claude launch" \
  grep -q -- '--model $EVAL_MODEL' "$RUN_SH"
assert "run.sh passes --model to summarize.py" \
  grep -qE -- '--model[[:space:]]+"\$EVAL_MODEL"' "$RUN_SH"

if (( failed > 0 )); then
  echo "test_scenarios_lifecycle.sh: $failed FAILED" >&2
  exit 1
fi
echo "test_scenarios_lifecycle.sh: all green"
