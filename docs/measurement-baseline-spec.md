# ctxd 効果測定 baseline spec — Before/After 計測の実装仕様

判断の根拠は `docs/adr/0002-ctxd-effect-measurement.md` を参照。本 spec は実装の固定定義 (SQL、出力フォーマット、再現手順) を担う。Before/After の比較可能性のため、本 spec の SQL 定義はバージョン管理し、変更時は ADR 0002 への追記または 0003 ADR を起こすこと。

## 1. データソース

### 1.1 入力ファイル

- `.team/traces/traces.db` (sqlite, cmux-team が記録)
- 本 spec が前提とする schema (2026-05-02 観測時点):

```sql
CREATE TABLE hook_signals (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  timestamp TEXT NOT NULL,
  type TEXT NOT NULL,           -- PRE_TOOL_USE / POST_TOOL_USE / SESSION_* など
  surface TEXT,
  pid INTEGER,
  reason TEXT,
  source TEXT,
  question TEXT,
  task_run_id TEXT,
  payload_json TEXT NOT NULL,   -- tool_input.command を含む JSON
  surface_uuid TEXT,
  workspace_uuid TEXT,
  role TEXT,                    -- agent / conductor / master
  task_id TEXT,
  conductor_surface TEXT,
  agent_role TEXT,
  message TEXT,
  notification_type TEXT,
  session_id TEXT,
  tool_name TEXT                -- Bash / Read / Edit / TaskUpdate ...
);

CREATE TABLE api_usage (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  timestamp TEXT NOT NULL,
  task_id TEXT,
  role TEXT,
  surface TEXT,
  conductor_id TEXT,
  model TEXT,
  request_id TEXT,
  status_code INTEGER,
  input_tokens INTEGER,
  output_tokens INTEGER,
  cache_creation_input_tokens INTEGER,
  cache_read_input_tokens INTEGER,
  stop_reason TEXT,
  duration_ms INTEGER
  -- (rate limit 列省略)
);

CREATE TABLE task_sessions (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  timestamp TEXT NOT NULL,
  task_id TEXT NOT NULL,
  task_run_id TEXT,
  session_id TEXT NOT NULL,
  role TEXT,
  surface TEXT,
  worktree_path TEXT,
  event TEXT NOT NULL,
  base_branch TEXT,
  base_sha TEXT,
  base_source TEXT
);
```

schema が変わったら本セクションを更新する。

### 1.2 母数

- 計測期間: スクリプト引数 `--since YYYY-MM-DD` で指定。default は DB の最古 timestamp から `--until` まで全期間
- 集計単位: `tool 呼び出し件数` / `セッション件数` / `タスク件数` の 3 軸を併記
- 対象 role: 全 role を母数に含めるが、ブレイクダウンで `agent` / `conductor` / `master` を分ける
- 主指標: agent ロール (実装作業の中心、ctxd 効果を最も受ける)

## 2. 抽出 SQL

### 2.1 Bash command の取り出し共通 view

`hook_signals.payload_json` から `tool_input.command` を JSON 抽出する。sqlite3 の `json_extract` を使う:

```sql
-- 共通 CTE: PRE_TOOL_USE で Bash の command を取り出す
WITH bash_calls AS (
  SELECT
    h.id,
    h.timestamp,
    h.role,
    h.task_id,
    h.session_id,
    json_extract(h.payload_json, '$.payload.tool_input.command') AS command
  FROM hook_signals h
  WHERE h.type = 'PRE_TOOL_USE'
    AND h.tool_name = 'Bash'
    AND h.timestamp >= :since
    AND h.timestamp <  :until
)
```

`PRE_TOOL_USE` を採用する理由: claude が「実行しようとした」コマンドを観測するため。ctxd の挿入で claude の意図 (raw bash を選ぶか ctxd を選ぶか) を測りたいので post ではなく pre。

### 2.2 Tier 1: 直接置換指標

```sql
-- Tier 1.1: cd (compound 含む)
SELECT role, COUNT(*) AS hits
FROM bash_calls
WHERE command LIKE 'cd %'
   OR command LIKE 'cd ' -- 単独 cd (ホームへ)
   OR command LIKE '% && cd %'
   OR command LIKE '% ; cd %'
   OR command LIKE 'cd ' || char(10) || '%' -- 改行を含む multi-line
GROUP BY role;

-- Tier 1.2: git checkout / git switch
SELECT role, COUNT(*) AS hits
FROM bash_calls
WHERE command LIKE 'git checkout%'
   OR command LIKE 'git switch%'
   OR command LIKE '% && git checkout%'
   OR command LIKE '% && git switch%'
GROUP BY role;

-- Tier 1.3: export / unset
SELECT role, COUNT(*) AS hits
FROM bash_calls
WHERE command LIKE 'export %'
   OR command LIKE 'unset %'
   OR command LIKE '% && export %'
   OR command LIKE '% && unset %'
GROUP BY role;
```

注意点:

- `cd /tmp/foo && ls` のような compound は Tier 1.1 と Tier 2.x の両方にカウントしたい場合があるが、**初版は Tier 1 を優先**してダブルカウントしない (compound は cd 1 件として計上)
- `git checkout -- file.txt` (revert 用途) も Tier 1.2 にカウントされる。これは ctxd の射程外なので、After で残ってもエラーではない。ドリルダウンで分離して可視化する (将来課題)
- パターンマッチは false positive を含む (例: `cd-rom` を含む文字列)。実用上は無視できるが、サンプル件数が極端に少なくなった場合は手動レビューする

### 2.3 Tier 2: 波及指標

```sql
-- Tier 2.1: 単独 pwd
SELECT role, COUNT(*) AS hits
FROM bash_calls
WHERE TRIM(command) = 'pwd'
GROUP BY role;

-- Tier 2.2: git status / git branch --show-current
SELECT role, COUNT(*) AS hits
FROM bash_calls
WHERE command LIKE 'git status%'
   OR command LIKE 'git branch --show-current%'
   OR command LIKE 'git rev-parse --abbrev-ref%'
GROUP BY role;

-- Tier 2.3: env 確認
SELECT role, COUNT(*) AS hits
FROM bash_calls
WHERE command LIKE 'printenv%'
   OR command LIKE 'echo $%'
   OR command LIKE 'env | %'
GROUP BY role;
```

注意点:

- `git status` は単独確認用と「commit 前の確認」両方の用途がある。ctxd は前者を envelope で代替するが後者は代替しない。**初版はカウントを分離せず、After で減った分だけが ctxd 効果**として読む (ノイズ込みで観測)
- `pwd` は単独時のみカウント。compound (`pwd && ls` 等) は Tier 2.1 に含めない (raw bash の compound そのものが Tier 1 でカウント済み)

### 2.4 Tier 3: 副作用指標 (api_usage 集計)

```sql
-- role 別 1 task あたり avg
SELECT
  role,
  COUNT(DISTINCT task_id) AS tasks,
  AVG(input_tokens)         AS avg_input,
  AVG(output_tokens)        AS avg_output,
  AVG(cache_creation_input_tokens) AS avg_cache_create,
  AVG(cache_read_input_tokens)     AS avg_cache_read,
  AVG(duration_ms)          AS avg_duration_ms
FROM api_usage
WHERE timestamp >= :since
  AND timestamp <  :until
  AND task_id IS NOT NULL
GROUP BY role;

-- role 別 1 session あたり tool_uses (hook_signals)
SELECT
  role,
  COUNT(DISTINCT session_id) AS sessions,
  CAST(COUNT(*) AS REAL) / CAST(COUNT(DISTINCT session_id) AS REAL) AS avg_tool_uses_per_session
FROM hook_signals
WHERE type = 'PRE_TOOL_USE'
  AND timestamp >= :since
  AND timestamp <  :until
GROUP BY role;
```

`model` 列で bucket して claude バージョン差を分離した版も出す:

```sql
SELECT model, role, COUNT(DISTINCT task_id) AS tasks,
       AVG(input_tokens) AS avg_input,
       AVG(output_tokens) AS avg_output,
       AVG(duration_ms) AS avg_duration_ms
FROM api_usage
WHERE timestamp >= :since AND timestamp < :until AND task_id IS NOT NULL
GROUP BY model, role;
```

## 3. 出力フォーマット

### 3.1 ファイルレイアウト

```
evals/baseline/
├── data-before-20260502.json     # 抽出した中間データ (再描画可能)
├── cmux-team-before-20260502.html # HTML レポート
├── data-after-<YYYYMMDD>.json    # 後で生成
└── cmux-team-after-<YYYYMMDD>.html
```

中間 JSON を残す理由: schema 変更や HTML テンプレ改修があっても、過去 baseline を再描画できるようにするため (eval 側の `evals/results/<timestamp>/summary.md` と同じ思想)。

### 3.2 HTML 構成

```
┌─ Header ──────────────────────────────────┐
│ phase: BEFORE / AFTER (大きく)             │
│ 計測期間, 母数 (sessions / tasks / calls) │
│ 対象 repo, model bucket                   │
├─ Tier 1: 直接置換指標 ────────────────────┤
│ カテゴリ別 (cd / git switch / env)        │
│ - 件数 (絶対)                              │
│ - per-session 率                           │
│ - role 別ブレイクダウン (agent 強調)       │
├─ Tier 2: 波及指標 ────────────────────────┤
│ 同上構造                                   │
├─ Tier 3: 副作用指標 ──────────────────────┤
│ role × model bucket × (tokens/duration)   │
│ 表形式                                     │
├─ Footer ──────────────────────────────────┤
│ schema_version, generated_at, git SHA     │
└────────────────────────────────────────────┘
```

- 集計値のみ。生コマンド (`tool_input.command` の中身) は **初版では非表示** (ADR 0002 Open question 3 参照)
- ダーク/ライト不要、片方だけ
- Chart.js は使わず HTML/CSS のみ (Tier 1/2 は単純な棒グラフで十分、Tier 3 は表)。初版は外部 JS なし。将来 chart が欲しくなったら Chart.js 追加

### 3.3 比較ページ (After 取得後の追加実装)

After を取った後に、Before と After を並べる比較 HTML を生成するスクリプトを別タスクで実装する。本 spec は Before/After 単体生成までの範囲。

## 4. 実装

### 4.1 スクリプト

`scripts/build-baseline-report.sh`:

```bash
#!/usr/bin/env bash
# Usage:
#   bash scripts/build-baseline-report.sh --phase before [--since YYYY-MM-DD] [--until YYYY-MM-DD]
set -euo pipefail

REPO_ROOT="$(git rev-parse --show-toplevel)"
DB="$REPO_ROOT/.team/traces/traces.db"
OUT_DIR="$REPO_ROOT/evals/baseline"
mkdir -p "$OUT_DIR"

# arg parse
PHASE="${PHASE:-before}"
SINCE=""
UNTIL=""
while [[ $# -gt 0 ]]; do
  case "$1" in
    --phase) PHASE="$2"; shift 2 ;;
    --since) SINCE="$2"; shift 2 ;;
    --until) UNTIL="$2"; shift 2 ;;
    *) echo "unknown arg: $1" >&2; exit 1 ;;
  esac
done

[[ "$PHASE" =~ ^(before|after)$ ]] || { echo "phase must be before|after" >&2; exit 1; }

DATE="$(date +%Y%m%d)"
DATA_JSON="$OUT_DIR/data-${PHASE}-${DATE}.json"
HTML_OUT="$OUT_DIR/cmux-team-${PHASE}-${DATE}.html"

# 1. SQL 実行 → data JSON 生成 (python3 で sqlite3 + 集計)
python3 "$REPO_ROOT/scripts/lib/extract_baseline.py" \
  --db "$DB" \
  ${SINCE:+--since "$SINCE"} \
  ${UNTIL:+--until "$UNTIL"} \
  --phase "$PHASE" \
  --out "$DATA_JSON"

# 2. data JSON → HTML 生成
python3 "$REPO_ROOT/scripts/lib/render_baseline.py" \
  --data "$DATA_JSON" \
  --out "$HTML_OUT"

echo "wrote: $HTML_OUT"
echo "wrote: $DATA_JSON"
```

抽出ロジック (`extract_baseline.py`) と描画ロジック (`render_baseline.py`) は分離する。schema 変更時は extract 側だけ直し、HTML テンプレ変更時は render 側だけ直せるようにする。

### 4.2 中間 JSON schema

```jsonc
{
  "schema_version": 1,
  "phase": "before",
  "generated_at": "2026-05-02T15:00:00Z",
  "git_sha": "691eca5",
  "window": { "since": "2026-04-25", "until": "2026-05-03" },
  "totals": {
    "sessions": 142,
    "tasks": 28,
    "tool_calls": 1468,
    "bash_calls": 188
  },
  "tier1": {
    "cd":         { "agent": 12, "conductor": 3, "master": 5, "total": 20 },
    "git_switch": { "agent": 4,  "conductor": 1, "master": 0, "total": 5  },
    "env":        { "agent": 0,  "conductor": 0, "master": 1, "total": 1  }
  },
  "tier2": {
    "pwd":        { "...": "..." },
    "git_status": { "...": "..." },
    "env_check":  { "...": "..." }
  },
  "tier3": {
    "by_role": [
      { "role": "agent", "tasks": 18, "avg_input": 23, "avg_output": 1929, "avg_duration_ms": 25600 }
    ],
    "by_model_role": [
      { "model": "claude-opus-4-7", "role": "agent", "...": "..." }
    ]
  },
  "per_session_rate": {
    "tier1.cd": { "agent": 0.084, "...": "..." }
  }
}
```

### 4.3 完了条件

- [ ] `scripts/build-baseline-report.sh --phase before` 実行で `evals/baseline/data-before-<date>.json` と `cmux-team-before-<date>.html` が生成される
- [ ] HTML をブラウザで開いて 3 tier すべて表示される
- [ ] 中間 JSON を `git diff` で確認して、想定の集計値と一致する (手動 SQL で cross-check)
- [ ] 期間引数 `--since` / `--until` が動作する
- [ ] schema 変更追従: 本 spec の SQL 定義と実装スクリプトが一致している (CI チェックは初版では入れない、手動運用)

## 5. After 計測の運用

ctxd を本 repo の cmux-team 環境に投入してから **2 週間以上**経過し、Bash tool 呼び出しが Before と同オーダー以上になった時点で:

```bash
bash scripts/build-baseline-report.sh --phase after --since YYYY-MM-DD --until YYYY-MM-DD
```

を実行。`--since` は ctxd 投入日、`--until` は計測実行日。生成された `cmux-team-after-<date>.html` と既存の Before HTML を見比べる。

Before/After を 1 ページに並べる比較レポートは別タスク (`scripts/build-baseline-diff.sh` 等) で対応。本 spec の射程外。

## 6. 限界と注意点

- **母数の偏り**: ctxd repo の cmux-team 運用 1 週間 = タスク 28 件、Bash 呼び出し 188 件。Tier 1 の hit 件数は数十オーダー。統計的有意性は弱いので、After 比較時は **絶対件数の差分**よりも **per-session/per-task 率**を主指標にする
- **claude 進化との混合**: Before/After 期間に claude code バージョンが上がったら、Tier 3 を `model` bucket で分離して影響を切る
- **匿名化**: 集計値のみの公開を厳守。生コマンド表示を将来追加するなら、別 ADR で範囲と匿名化方針を決める
- **schema drift**: cmux-team の hook schema が変わったら、本 spec の §1.1 と §2 の SQL を更新し、必要なら ADR 0002 に追記する。spec 更新時は `schema_version` をインクリメント
