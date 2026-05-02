#!/usr/bin/env bash
# scripts/build-baseline-report.sh
#
# .team/traces/traces.db を入力に ctxd baseline 計測の中間 JSON と HTML を生成する。
#
# Usage:
#   bash scripts/build-baseline-report.sh --phase before \
#        [--source-name <name>] \
#        [--since YYYY-MM-DD] [--until YYYY-MM-DD] [--db <path>]
#
# Output:
#   evals/baseline/data-<source>-<phase>-<YYYYMMDD>.json
#   evals/baseline/<source>-<phase>-<YYYYMMDD>.html
#
# --source-name は kebab-case ([a-z0-9][a-z0-9-]*); default は "ctxd".
#
# Exit codes (plan §2.0):
#   0 success
#   1 arg parse error / unknown arg / invalid phase / invalid date
#   2 input file (DB) not found
#   3 empty window (no tool_calls in [since, until))
#   4 unexpected python failure
#   5 schema mismatch / malformed intermediate JSON
#
# 仕様: docs/measurement-baseline-spec.md §1〜§4

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

PHASE="before"
SINCE=""
UNTIL=""
DB="$REPO_ROOT/.team/traces/traces.db"
OUT_DIR="$REPO_ROOT/evals/baseline"
SOURCE_NAME="ctxd"

usage() {
  sed -n '2,20p' "$0" | sed 's/^# \{0,1\}//'
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --phase) PHASE="${2:-}"; shift 2 ;;
    --since) SINCE="${2:-}"; shift 2 ;;
    --until) UNTIL="${2:-}"; shift 2 ;;
    --db) DB="${2:-}"; shift 2 ;;
    --source-name) SOURCE_NAME="${2:-}"; shift 2 ;;
    -h|--help) usage; exit 0 ;;
    *) echo "unknown arg: $1" >&2; usage >&2; exit 1 ;;
  esac
done

# arg validation
if [[ ! "$PHASE" =~ ^(before|after)$ ]]; then
  echo "phase must be 'before' or 'after' (got: $PHASE)" >&2
  exit 1
fi
date_re='^[0-9]{4}-[0-9]{2}-[0-9]{2}$'
if [[ -n "$SINCE" && ! "$SINCE" =~ $date_re ]]; then
  echo "--since must be YYYY-MM-DD (got: $SINCE)" >&2
  exit 1
fi
if [[ -n "$UNTIL" && ! "$UNTIL" =~ $date_re ]]; then
  echo "--until must be YYYY-MM-DD (got: $UNTIL)" >&2
  exit 1
fi
if ! [[ "$SOURCE_NAME" =~ ^[a-z0-9][a-z0-9-]*$ ]]; then
  echo "--source-name must be kebab-case [a-z0-9][a-z0-9-]* (got: $SOURCE_NAME)" >&2
  exit 1
fi

# DB existence
if [[ ! -f "$DB" ]]; then
  echo "DB not found: $DB" >&2
  exit 2
fi

mkdir -p "$OUT_DIR"

# git_sha (best-effort; outside repo will fall back to "unknown")
GIT_SHA="$(cd "$REPO_ROOT" && git rev-parse --short HEAD 2>/dev/null || echo unknown)"

# <YYYYMMDD> 決定 (plan §1.3): --until 指定時 → そのまま、未指定時 → DB MAX timestamp の UTC 日付
if [[ -n "$UNTIL" ]]; then
  WINDOW_END_DATE="${UNTIL//-/}"
else
  WINDOW_END_DATE="$(sqlite3 -batch "$DB" \
    "SELECT strftime('%Y%m%d', MAX(timestamp)) FROM hook_signals;" 2>/dev/null \
    || echo "")"
  if [[ -z "$WINDOW_END_DATE" ]]; then
    echo "no data in DB (could not derive window end date)" >&2
    exit 3
  fi
fi

DATA_JSON="$OUT_DIR/data-${SOURCE_NAME}-${PHASE}-${WINDOW_END_DATE}.json"
HTML_OUT="$OUT_DIR/${SOURCE_NAME}-${PHASE}-${WINDOW_END_DATE}.html"

# 1. extract: sqlite3 → 中間 JSON
python3 "$SCRIPT_DIR/lib/extract_baseline.py" \
  --db "$DB" \
  --phase "$PHASE" \
  --git-sha "$GIT_SHA" \
  --source-name "$SOURCE_NAME" \
  ${SINCE:+--since "$SINCE"} \
  ${UNTIL:+--until "$UNTIL"} \
  --out "$DATA_JSON"

# 2. render: 中間 JSON → HTML
python3 "$SCRIPT_DIR/lib/render_baseline.py" \
  --data "$DATA_JSON" \
  --out "$HTML_OUT"

echo "wrote: $DATA_JSON"
echo "wrote: $HTML_OUT"
