#!/usr/bin/env bash
# evals/tests/run_tests.sh — TDD テストを直列実行する.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
EVAL_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"

failed=0
run() {
  local label="$1"; shift
  printf '\n==> %s\n' "$label"
  if "$@"; then
    printf '<== ok %s\n' "$label"
  else
    printf '<== FAIL %s\n' "$label"
    failed=$((failed + 1))
  fi
}

# bash 構文検証 (受け入れ条件)
run "bash -n run.sh" bash -n "$EVAL_DIR/run.sh"
for f in "$EVAL_DIR"/lib/*.sh "$EVAL_DIR"/tests/*.sh; do
  run "bash -n $(basename "$f")" bash -n "$f"
done

# python 構文検証 (受け入れ条件)
run "py_compile summarize.py" python3 -m py_compile "$EVAL_DIR/summarize.py"
run "py_compile test_summarize.py" python3 -m py_compile "$EVAL_DIR/tests/test_summarize.py"

# T1: encode_cwd の実値突き合わせ
run "test_encode_path.sh" bash "$EVAL_DIR/tests/test_encode_path.sh"

# T2-T6: summarize.py 単体テスト
run "test_summarize.py (unittest)" \
  python3 -m unittest discover -s "$EVAL_DIR/tests" -p 'test_*.py'

# scenarios.jsonl の load (本番 fail-fast の現地確認)
run "scenarios.jsonl load" python3 -c "
import sys; sys.path.insert(0, '$EVAL_DIR')
import summarize
ss = summarize.load_scenarios('$EVAL_DIR/scenarios.jsonl')
assert len(ss) >= 5, f'need >=5 scenarios, got {len(ss)}'
print(f'  {len(ss)} scenarios loaded')
"

if (( failed > 0 )); then
  printf '\nrun_tests.sh: %d FAILED\n' "$failed" >&2
  exit 1
fi
printf '\nrun_tests.sh: all green\n'
