#!/usr/bin/env bash
# evals/tests/test_build_settings.sh — T021
#
# build_settings.sh が hook timeout を引数化しており、default 10000ms に
# 引き上げられていることを確認する。Stop hook も同 timeout を共有。

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
EVAL_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
BUILD_SH="$EVAL_DIR/lib/build_settings.sh"

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

TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT

# ---------- 1. default timeout = 10000 ----------
out1="$TMP/settings-default.json"
bash "$BUILD_SH" \
  --tool-uses-out "$TMP/tools.jsonl" \
  --stop-sentinel "$TMP/stop.done" \
  --out           "$out1" >/dev/null

post_to=$(jq -r '.hooks.PostToolUse[0].hooks[0].timeout' < "$out1")
stop_to=$(jq -r '.hooks.Stop[0].hooks[0].timeout' < "$out1")
assert "default PostToolUse timeout == 10000" test "$post_to" = "10000"
assert "default Stop timeout == 10000 (shared)" test "$stop_to" = "10000"

# ---------- 2. --hook-timeout-ms 15000 で上書き ----------
out2="$TMP/settings-explicit.json"
bash "$BUILD_SH" \
  --tool-uses-out "$TMP/tools.jsonl" \
  --stop-sentinel "$TMP/stop.done" \
  --hook-timeout-ms 15000 \
  --out           "$out2" >/dev/null

post_to2=$(jq -r '.hooks.PostToolUse[0].hooks[0].timeout' < "$out2")
stop_to2=$(jq -r '.hooks.Stop[0].hooks[0].timeout' < "$out2")
assert "explicit PostToolUse timeout == 15000" test "$post_to2" = "15000"
assert "explicit Stop timeout == 15000" test "$stop_to2" = "15000"

# ---------- 3. command 構造は壊れていない ----------
post_cmd=$(jq -r '.hooks.PostToolUse[0].hooks[0].command' < "$out1")
stop_cmd=$(jq -r '.hooks.Stop[0].hooks[0].command' < "$out1")
case "$post_cmd" in
  *hook-tool-use.py*) assert "PostToolUse command references hook-tool-use.py" true ;;
  *) assert "PostToolUse command references hook-tool-use.py" false ;;
esac
case "$stop_cmd" in
  *touch*) assert "Stop command references touch sentinel" true ;;
  *) assert "Stop command references touch sentinel" false ;;
esac

if (( failed > 0 )); then
  echo "test_build_settings.sh: $failed FAILED" >&2
  exit 1
fi
echo "test_build_settings.sh: all green"
