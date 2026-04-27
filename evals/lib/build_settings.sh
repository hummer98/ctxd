#!/usr/bin/env bash
# evals/lib/build_settings.sh — 1 trial 用の claude settings.json を生成する (T015).
#
# Usage:
#   bash evals/lib/build_settings.sh \
#     --tool-uses-out /abs/path/session-<id>-<trial>.tools.jsonl \
#     --stop-sentinel /abs/path/session-<id>-<trial>.done \
#     --out          /abs/path/session-<id>-<trial>.settings.json
#
# 生成 settings.json は以下を含む:
#   - PostToolUse hook: python3 <repo>/evals/lib/hook-tool-use.py <tool-uses-out>
#   - Stop hook:        bash -c 'touch <stop-sentinel>'
#
# 前提:
#   - 引数で渡される path に空白 / シングルクォート / 制御文字は含めない (実用上問題ない).
#   - JSON 値は python3 の json.dumps を経由してエスケープするので、bash の word splitting
#     とは独立に safe.
#
# 失敗条件: 必須引数欠落で exit 2.

set -euo pipefail

TOOL_USES_OUT=""
STOP_SENTINEL=""
OUT=""

while (( $# > 0 )); do
  case "$1" in
    --tool-uses-out) TOOL_USES_OUT="$2"; shift 2 ;;
    --stop-sentinel) STOP_SENTINEL="$2"; shift 2 ;;
    --out)           OUT="$2"; shift 2 ;;
    *)
      echo "build_settings.sh: unknown arg: $1" >&2
      exit 2
      ;;
  esac
done

if [[ -z "$TOOL_USES_OUT" || -z "$STOP_SENTINEL" || -z "$OUT" ]]; then
  echo "build_settings.sh: missing required args (--tool-uses-out / --stop-sentinel / --out)" >&2
  exit 2
fi

# repo root + hook script の絶対 path を解決.
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
HOOK_PY="$SCRIPT_DIR/hook-tool-use.py"
if [[ ! -f "$HOOK_PY" ]]; then
  echo "build_settings.sh: hook script missing: $HOOK_PY" >&2
  exit 2
fi

# 出力先ディレクトリを保証.
mkdir -p "$(dirname "$OUT")"

# JSON 生成は python3 の json.dumps に任せる (bash heredoc + sed より安全).
TOOL_USES_OUT="$TOOL_USES_OUT" \
STOP_SENTINEL="$STOP_SENTINEL" \
HOOK_PY="$HOOK_PY" \
OUT="$OUT" \
python3 - <<'PY'
import json, os, sys

hook_py        = os.environ["HOOK_PY"]
tool_uses_out  = os.environ["TOOL_USES_OUT"]
stop_sentinel  = os.environ["STOP_SENTINEL"]
out            = os.environ["OUT"]

post_cmd = f"python3 {hook_py} {tool_uses_out}"
stop_cmd = f"bash -c 'touch {stop_sentinel}'"

settings = {
    "hooks": {
        "PostToolUse": [
            {
                "matcher": "",
                "hooks": [
                    {"type": "command", "command": post_cmd, "timeout": 5000},
                ],
            }
        ],
        "Stop": [
            {
                "matcher": "",
                "hooks": [
                    {"type": "command", "command": stop_cmd, "timeout": 5000},
                ],
            }
        ],
    }
}

with open(out, "w", encoding="utf-8") as f:
    json.dump(settings, f, ensure_ascii=False, indent=2)
PY

echo "build_settings.sh: wrote $OUT"
