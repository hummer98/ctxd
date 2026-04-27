# claude_session.sh — runner で使う heavy-lifting helpers (plan §5).
#
# このファイルは run.sh から `source` で読み込む前提。
# `set -euo pipefail` 下で動く。

# ---------- ensure_eval_plugin ----------
# 一時的な最小 plugin (`evals/.eval-plugin/`) を整備する。
# 既存があっても `ln -snf` で symlink を強制再作成し、worktree 切り替えや
# SKILL.md 編集が即時反映されるようにする (plan §3 / m6).
#
# Usage: ensure_eval_plugin <plugin-dir> <skill-md-path> <plugin-version>
#
# 注: name は eval 専用に "ctxd-eval"。真のソース (.claude-plugin/plugin.json) の
# "ctxd" と意図的に分離する (plugin loader 衝突回避、Q2 reviewer note 参照).
# version のみ真のソースから動的書き換えされる。
ensure_eval_plugin() {
  local plugin_dir="$1" skill_md="$2" plugin_version="$3"
  local meta_dir="$plugin_dir/.claude-plugin"
  local manifest="$meta_dir/plugin.json"
  local skills_dir="$plugin_dir/skills/ctxd"
  local skill_link="$skills_dir/SKILL.md"

  mkdir -p "$meta_dir" "$skills_dir"

  # heredoc は非クォート (`<<JSON`) で `${plugin_version}` を展開させる (S2 reviewer note).
  # `${plugin_version}` は呼び出し側で .claude-plugin/plugin.json から読み出した値.
  local desired
  desired=$(cat <<JSON
{
  "name": "ctxd-eval",
  "version": "${plugin_version}",
  "description": "Eval-only wrapper that ships skills/ctxd as a plugin so the Skills loader picks it up during evaluation.",
  "skills": ["./skills/ctxd"]
}
JSON
)
  if [[ ! -f "$manifest" ]] || [[ "$(cat "$manifest")" != "$desired" ]]; then
    printf '%s\n' "$desired" > "$manifest"
  fi

  # symlink を毎回強制再作成 (-snf 相当)
  ln -snf "$skill_md" "$skill_link"
}

# ---------- record_meta ----------
# Trial の meta.json を書き出す。run.sh 側のローカル変数 $out_meta /
# $session_id / $started_at / $id / $trial に依存する (plan §5 擬似コード準拠).
#
# Usage: record_meta <exit_status> <detail>
record_meta() {
  local exit_status="$1" detail="${2:-}"
  local ended_at wall
  ended_at=$(date -u +%FT%TZ)
  # wall_seconds: started_at から ended_at までの秒。BSD date でも GNU date でも動くよう epoch で計算
  local s_epoch e_epoch
  s_epoch=$(date -j -u -f "%Y-%m-%dT%H:%M:%SZ" "$started_at" +%s 2>/dev/null || echo 0)
  e_epoch=$(date -j -u -f "%Y-%m-%dT%H:%M:%SZ" "$ended_at" +%s 2>/dev/null || echo 0)
  if [[ "$s_epoch" -gt 0 && "$e_epoch" -ge "$s_epoch" ]]; then
    wall=$((e_epoch - s_epoch))
  else
    wall=0
  fi
  cat > "$out_meta" <<JSON
{
  "scenario_id": "$id",
  "trial": $trial,
  "session_id": "$session_id",
  "started_at": "$started_at",
  "ended_at": "$ended_at",
  "wall_seconds": $wall,
  "exit_status": "$exit_status",
  "detail": "$detail"
}
JSON
}

# ---------- poll_for_ready ----------
# claude が起動して `❯` プロンプトが出るのを待つ。Trust 確認が出ていたら enter で承認する。
#
# Usage: poll_for_ready <workspace> <timeout-seconds>
# 戻り値: 0 = ready, 非 0 = timeout
poll_for_ready() {
  local ws="$1" timeout="$2"
  local deadline=$(( $(date +%s) + timeout ))
  while (( $(date +%s) < deadline )); do
    local screen
    screen=$(cmux read-screen --workspace "$ws" --lines 60 2>/dev/null || true)
    # Trust 確認 (`Yes, I trust` や `Do you trust` 等) が見えたら enter
    if printf '%s' "$screen" | grep -qE 'trust|Trust this|Do you want to proceed'; then
      cmux send-key --workspace "$ws" enter 2>/dev/null || true
      sleep 1
      continue
    fi
    # `❯` 単独行 + Esc to interrupt が見えない = ready (claude のプロンプト)
    if printf '%s' "$screen" | grep -q '❯'; then
      return 0
    fi
    sleep 1
  done
  return 1
}

# ---------- poll_for_completion ----------
# 応答完了を待つ: 末尾近くに `❯` が単独で見えて、かつ `Esc to interrupt` が消えていること。
# 2 秒間隔で polling。
#
# Usage: poll_for_completion <workspace> <timeout-seconds>
# 戻り値: 0 = 完了, 非 0 = timeout
poll_for_completion() {
  local ws="$1" timeout="$2"
  local deadline=$(( $(date +%s) + timeout ))
  while (( $(date +%s) < deadline )); do
    local screen
    screen=$(cmux read-screen --workspace "$ws" --lines 60 2>/dev/null || true)
    if printf '%s' "$screen" | grep -q 'Esc to interrupt'; then
      sleep 2
      continue
    fi
    if printf '%s' "$screen" | grep -q '❯'; then
      return 0
    fi
    sleep 2
  done
  return 1
}

# ---------- wait_for_jsonl ----------
# 指定 path の JSONL ファイルが存在し size > 0 になるのを 0.5 秒間隔で待つ。
#
# Usage: wait_for_jsonl <path> <timeout-seconds>
wait_for_jsonl() {
  local path="$1" timeout="$2"
  local deadline=$(( $(date +%s) + timeout ))
  while (( $(date +%s) < deadline )); do
    if [[ -f "$path" ]] && [[ -s "$path" ]]; then
      return 0
    fi
    sleep 1
  done
  return 1
}
