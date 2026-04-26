#!/usr/bin/env bash
# evals/run.sh — SKILL eval harness runner.
#
# 実行: bash evals/run.sh
#   EVAL_N=<n>                  シナリオあたり試行回数 (default 3)
#   RUN_TIMEOUT_SECONDS=<sec>   1 試行の応答待ち上限 (default 180)
#
# 必要 CLI: cmux, claude, uuidgen, jq, python3
#
# 注意: 実行時に Anthropic API 課金が走る (plan §10.4: N=3 × 5 シナリオで数十 cent〜$1).
# 非対話で動くため `--dangerously-skip-permissions` を使う点に留意.
set -euo pipefail

# ---------- 設定 ----------
REPO_ROOT="$(git rev-parse --show-toplevel)"
N="${EVAL_N:-3}"
RUN_TIMEOUT_SECONDS="${RUN_TIMEOUT_SECONDS:-180}"
TIMESTAMP="$(date -u +%Y%m%d-%H%M%S)"
RESULTS_DIR="$REPO_ROOT/evals/results/$TIMESTAMP"
PLUGIN_DIR="$REPO_ROOT/evals/.eval-plugin"
SCENARIOS="$REPO_ROOT/evals/scenarios.jsonl"
SKILL_MD="$REPO_ROOT/skills/ctxd/SKILL.md"

mkdir -p "$RESULTS_DIR"

# ---------- 必須コマンド確認 ----------
for cmd in cmux claude uuidgen jq python3; do
  if ! command -v "$cmd" >/dev/null 2>&1; then
    echo "run.sh: required command not found: $cmd" >&2
    exit 1
  fi
done

# ---------- claude version (m2 反映) ----------
CLAUDE_VERSION="$(claude --version 2>&1 | head -1 || true)"

# ---------- helpers ----------
# shellcheck source=lib/encode_path.sh
source "$REPO_ROOT/evals/lib/encode_path.sh"
# shellcheck source=lib/claude_session.sh
source "$REPO_ROOT/evals/lib/claude_session.sh"

# ---------- scenarios の事前 validation (fail-fast) ----------
python3 -c "
import sys; sys.path.insert(0, '$REPO_ROOT/evals')
import summarize
summarize.load_scenarios('$SCENARIOS')
"

# ---------- 一時 plugin の整備 ----------
ensure_eval_plugin "$PLUGIN_DIR" "$SKILL_MD"
# 構文確認 (失敗しても warning に留める。fallback への移行は手動判断)
if claude plugin validate "$PLUGIN_DIR" >/dev/null 2>&1; then
  echo "run.sh: plugin manifest validated"
else
  echo "run.sh: WARN plugin validate failed; continuing (may need --append-system-prompt-file fallback)" >&2
fi

# ---------- 全体 trap ----------
LAST_WS=""
cleanup_outer() {
  if [[ -n "$LAST_WS" ]]; then
    cmux close-workspace --workspace "$LAST_WS" 2>/dev/null || true
  fi
}
trap cleanup_outer EXIT INT TERM

# ---------- 1 シナリオ 1 試行 ----------
run_one() {
  local scenario_json="$1" trial="$2"
  local id prompt session_id started_at out_jsonl out_meta
  id=$(jq -r '.id' <<<"$scenario_json")
  prompt=$(jq -r '.prompt' <<<"$scenario_json")
  session_id=$(uuidgen | tr 'A-Z' 'a-z')
  started_at=$(date -u +%FT%TZ)
  out_jsonl="$RESULTS_DIR/session-$id-$trial.jsonl"
  out_meta="$RESULTS_DIR/session-$id-$trial.meta.json"

  echo "[$id #$trial] session_id=$session_id"

  # ---------- workspace 生成 (C1 反映) ----------
  # cmux new-workspace の出力は "OK workspace:<id>" 形式。awk '/^OK/ {print $2}' で抽出.
  local raw WS
  raw=$(cmux new-workspace --cwd "$REPO_ROOT" 2>&1 || true)
  WS=$(printf '%s\n' "$raw" | awk '/^OK/ {print $2}' | head -1)
  if [[ "$WS" != workspace:* ]]; then
    echo "run.sh: new-workspace parse failed: $raw" >&2
    record_meta error "new_workspace_parse_failed"
    return 0  # m7: 個別 trial の失敗で main loop を止めない
  fi
  LAST_WS="$WS"
  cmux rename-workspace --workspace "$WS" "eval-$id-$trial" >/dev/null 2>&1 || true

  # ---------- claude 起動 (M3: send は raw、enter は send-key 別途) ----------
  cmux send --workspace "$WS" \
    "claude --session-id $session_id --plugin-dir $PLUGIN_DIR --dangerously-skip-permissions"
  cmux send-key --workspace "$WS" enter

  if ! poll_for_ready "$WS" 30; then
    record_meta error "ready_timeout"
    cmux close-workspace --workspace "$WS" 2>/dev/null || true
    LAST_WS=""
    return 0  # m7
  fi

  # ---------- prompt 投入 (raw 送信 + 別途 enter, M3) ----------
  printf '%s' "$prompt" | cmux send --workspace "$WS"
  cmux send-key --workspace "$WS" enter

  # ---------- 完了 polling ----------
  if ! poll_for_completion "$WS" "$RUN_TIMEOUT_SECONDS"; then
    cmux send-key --workspace "$WS" 'ctrl+c' 2>/dev/null || true
    record_meta error "timeout"
    # JSONL は途中まででも回収を試みるので return しない
  fi

  # ---------- JSONL 取得 ----------
  local encoded src
  encoded=$(encode_cwd "$REPO_ROOT")
  src="$HOME/.claude/projects/$encoded/$session_id.jsonl"
  if wait_for_jsonl "$src" 10; then
    cp "$src" "$out_jsonl"
    if [[ ! -f "$out_meta" ]]; then
      record_meta ok ""
    fi
  else
    record_meta error "jsonl_missing"
  fi

  # ---------- cleanup (M1: send-key enter) ----------
  cmux send --workspace "$WS" "/exit" 2>/dev/null || true
  cmux send-key --workspace "$WS" enter 2>/dev/null || true
  sleep 2
  cmux close-workspace --workspace "$WS" 2>/dev/null || true
  LAST_WS=""
}

# ---------- main loop ----------
while IFS= read -r line; do
  [[ -z "$line" || "$line" == \#* ]] && continue
  for trial in $(seq 1 "$N"); do
    run_one "$line" "$trial"
  done
done < "$SCENARIOS"

# ---------- 集計 (m2: --claude-version) ----------
python3 "$REPO_ROOT/evals/summarize.py" \
  --results-dir    "$RESULTS_DIR" \
  --scenarios      "$SCENARIOS" \
  --claude-version "$CLAUDE_VERSION" \
  --n              "$N" \
  --out            "$RESULTS_DIR/summary.md"

echo "done: $RESULTS_DIR/summary.md"
