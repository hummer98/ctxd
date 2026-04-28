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

# ---------- plugin version + git meta (T014) ----------
# `.claude-plugin/plugin.json` の version を真のソースとして読み出す (fail-fast).
PLUGIN_VERSION="$(python3 "$REPO_ROOT/evals/lib/read_plugin_version.py" "$REPO_ROOT")"
# T018: author も真のソースから動的取得 (派生先 eval-plugin manifest に継承する).
PLUGIN_AUTHOR_NAME="$(python3 "$REPO_ROOT/evals/lib/read_plugin_meta.py" "$REPO_ROOT" author.name)"
PLUGIN_AUTHOR_EMAIL="$(python3 "$REPO_ROOT/evals/lib/read_plugin_meta.py" "$REPO_ROOT" author.email)"
GIT_SHA="$(git -C "$REPO_ROOT" rev-parse --short=7 HEAD 2>/dev/null || echo unknown)"
GIT_BRANCH="$(git -C "$REPO_ROOT" rev-parse --abbrev-ref HEAD 2>/dev/null || echo unknown)"

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
# T014: 真のソースの version を ensure_eval_plugin に渡し、heredoc を経由して
# `evals/.eval-plugin/.claude-plugin/plugin.json` の version を再生成する.
# T018: author も真のソースから継承して書き換え (warning 0 維持).
ensure_eval_plugin "$PLUGIN_DIR" "$SKILL_MD" "$PLUGIN_VERSION" \
  "$PLUGIN_AUTHOR_NAME" "$PLUGIN_AUTHOR_EMAIL"
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
  local tool_uses_out stop_sentinel settings_path
  local original_branch
  id=$(jq -r '.id' <<<"$scenario_json")
  prompt=$(jq -r '.prompt' <<<"$scenario_json")
  # T021 fix: setup の前に worktree の現在ブランチを保存する。
  # cmux workspace 内で `ctxd git-switch <branch>` が走ると worktree HEAD が動いてしまい、
  # その状態で teardown の `git branch -D <branch>` を呼ぶと「現在 checkout 中ブランチの削除」
  # となり silent fail する。teardown 直前にこの保存値で switch -f して戻す。
  original_branch=$(git -C "$REPO_ROOT" rev-parse --abbrev-ref HEAD 2>/dev/null || echo "")
  # T018: scenarios の任意 setup フィールドを worktree (cwd = REPO_ROOT) で実行する.
  # cmux workspace は REPO_ROOT の同じ git repo (同 worktree) を共有するため、
  # 親で作ったブランチは workspace 内からも見える. 失敗しても trial は続行.
  local setup
  setup=$(jq -r '.setup // empty' <<<"$scenario_json")
  if [[ -n "$setup" ]]; then
    if ! ( cd "$REPO_ROOT" && bash -c "$setup" ); then
      echo "[$id #$2] WARN setup failed: $setup" >&2
    fi
  fi
  session_id=$(uuidgen | tr 'A-Z' 'a-z')
  started_at=$(date -u +%FT%TZ)
  out_jsonl="$RESULTS_DIR/session-$id-$trial.jsonl"
  out_meta="$RESULTS_DIR/session-$id-$trial.meta.json"
  # T015: hook-based completion detection 用の per-trial 成果物.
  tool_uses_out="$RESULTS_DIR/session-$id-$trial.tools.jsonl"
  stop_sentinel="$RESULTS_DIR/session-$id-$trial.done"
  settings_path="$RESULTS_DIR/session-$id-$trial.settings.json"

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

  # ---------- T015: settings.json 生成 ----------
  # PostToolUse hook で tool_use を JSONL に記録 + Stop hook で sentinel を touch.
  bash "$REPO_ROOT/evals/lib/build_settings.sh" \
    --tool-uses-out "$tool_uses_out" \
    --stop-sentinel "$stop_sentinel" \
    --out           "$settings_path" >/dev/null

  # ---------- claude 起動 (M3: send は raw、enter は send-key 別途 / T015: --settings 追加) ----------
  cmux send --workspace "$WS" \
    "claude --session-id $session_id --plugin-dir $PLUGIN_DIR --settings $settings_path --dangerously-skip-permissions"
  cmux send-key --workspace "$WS" enter

  if ! poll_for_ready "$WS" 30; then
    record_meta error "ready_timeout"
    cmux close-workspace --workspace "$WS" 2>/dev/null || true
    LAST_WS=""
    return 0  # m7
  fi

  # ---------- prompt 投入 (raw 送信 + 別途 enter, M3) ----------
  # 注: `cmux send` は stdin を読まない。プロンプトは引数として渡す必要あり (T013 で発覚)。
  # `--` 以降は raw 文字列として扱われる (cmux send --help 参照)。
  # 現状 scenarios の prompt は単純な日本語文字列のみ (改行/二重引用符なし)。
  # 改行を含める必要が出たら lib helper 経由で再検討する (plan §6.1)。
  cmux send --workspace "$WS" -- "$prompt"
  cmux send-key --workspace "$WS" enter

  # ---------- T015: sentinel 待ち (画面スクレイピング poll_for_completion を置換) ----------
  # Stop hook が touch する sentinel ファイルの出現を 0.5 秒粒度で待つ.
  local elapsed=0
  local max_iters=$((RUN_TIMEOUT_SECONDS * 2))
  local timed_out=1
  while (( elapsed < max_iters )); do
    if [[ -f "$stop_sentinel" ]]; then
      timed_out=0
      break
    fi
    sleep 0.5
    elapsed=$((elapsed + 1))
  done
  if (( timed_out == 1 )); then
    cmux send-key --workspace "$WS" 'ctrl+c' 2>/dev/null || true
    record_meta error "timeout"
    # JSONL は途中まででも回収を試みるので return しない
  fi

  # ---------- JSONL 取得 (fallback 経路は維持) ----------
  local encoded src
  encoded=$(encode_cwd "$REPO_ROOT")
  src="$HOME/.claude/projects/$encoded/$session_id.jsonl"
  if wait_for_jsonl "$src" 10; then
    cp "$src" "$out_jsonl"
    # timeout 等で先に error を書いた trial を上書きしない
    if [[ ! -f "$out_meta" ]]; then
      # T021: hook 出力 + session JSONL 双方で tool_use 0 件なら tools_missing で error 化.
      # exit 0 = missing → record_meta error "tools_missing".
      if python3 "$REPO_ROOT/evals/lib/check_tools_missing.py" \
          --tools-jsonl "$tool_uses_out" \
          --session-jsonl "$out_jsonl"; then
        record_meta error "tools_missing"
      else
        record_meta ok ""
      fi
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

  # ---------- T021 fix: teardown 前に元ブランチへ復帰 ----------
  # ctxd git-switch <branch> が worktree HEAD を動かしてしまっているため、teardown
  # で feature-eval を `git branch -D` する前に元ブランチへ戻す必要がある (Conductor 採用案)。
  # `switch -f` は uncommitted changes を踏み潰すが、eval harness は worktree を
  # 共有作業ツリーとして扱う前提で動いており、各 trial で副作用は許容範囲とする。
  # 元ブランチが取れていない / "HEAD" (detached) の場合はスキップする。
  if [[ -n "$original_branch" && "$original_branch" != "HEAD" ]]; then
    local current_branch
    current_branch=$(git -C "$REPO_ROOT" rev-parse --abbrev-ref HEAD 2>/dev/null || echo "")
    if [[ "$current_branch" != "$original_branch" ]]; then
      if ! git -C "$REPO_ROOT" switch -f "$original_branch" >/dev/null 2>&1; then
        echo "[$id #$trial] WARN failed to restore branch $original_branch (was on $current_branch)" >&2
      fi
    fi
  fi

  # ---------- T021: trial teardown ----------
  # setup と対称の処理。worktree (REPO_ROOT) で実行し、失敗は WARN のみ。
  # timeout / jsonl_missing 経路でも cleanup の前を通るので teardown は走る。
  # 早期 return している経路は new_workspace_parse_failed (L117) と ready_timeout
  # (L138) の 2 つのみで、いずれも workspace 起動前なので teardown 必要なし。
  local teardown
  teardown=$(jq -r '.teardown // empty' <<<"$scenario_json")
  if [[ -n "$teardown" ]]; then
    if ! ( cd "$REPO_ROOT" && bash -c "$teardown" ); then
      echo "[$id #$trial] WARN teardown failed: $teardown" >&2
    fi
  fi
}

# ---------- main loop ----------
while IFS= read -r line; do
  [[ -z "$line" || "$line" == \#* ]] && continue
  for trial in $(seq 1 "$N"); do
    run_one "$line" "$trial"
  done
done < "$SCENARIOS"

# ---------- 集計 (m2: --claude-version, T014: meta + index) ----------
# S3: 同一の summarize.py 呼び出しに `--plugin-version` / `--git-sha` /
# `--git-branch` / `--index-md` / `--index-csv` を追加する (1 回の invocation で
# summary.md の生成と index.{md,csv} への append を済ませる).
python3 "$REPO_ROOT/evals/summarize.py" \
  --results-dir    "$RESULTS_DIR" \
  --scenarios      "$SCENARIOS" \
  --claude-version "$CLAUDE_VERSION" \
  --plugin-version "$PLUGIN_VERSION" \
  --git-sha        "$GIT_SHA" \
  --git-branch     "$GIT_BRANCH" \
  --n              "$N" \
  --out            "$RESULTS_DIR/summary.md" \
  --index-md       "$REPO_ROOT/evals/results/index.md" \
  --index-csv      "$REPO_ROOT/evals/results/index.csv"

echo "done: $RESULTS_DIR/summary.md"
