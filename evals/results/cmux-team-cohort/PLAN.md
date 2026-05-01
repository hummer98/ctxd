# cmux-team cohort study — ctxd plugin field measurement PLAN

本計画は A001 (`.team/artifacts/A001-ctxd-effect-measurement-plan.md`) Stage 2 を cmux-team の実フィールドで運用するための準備文書である。介入 (`@hummer98/ctxd-claude-plugin` の install) の前後を retroactive baseline + 時系列 evaluation cohort として比較する。

各セクションの番号は固定で、後続タスク (T026〜T029) およびレポート (README 追記) はこの番号を参照する。

---

## 1. 目的

A001 Stage 2 の cmux-team 観察的フィールド計測。ctxd plugin が実プロジェクトでの作業効率・失敗率・コストへ与える効果を、cmux-team が日々生成している `events.jsonl` / `hook_signals` / `api_usage` から `cmux-team metrics` パイプラインで観測する。

Stage 1 (A001) の合成 eval (`evals/run.sh`) は SKILL.md frontmatter の trigger 挙動を計測するためのものであり、「実 task における ctxd の効用」までは捉えない。Stage 2 は本物のユーザー作業で観察し、実運用での効果を評価することを狙う。実験室統制 (RCT) は行わず、subject-within の時系列比較 + retroactive baseline を活用する観察的研究として設計する。

---

## 2. 採用する metric

`cmux-team metrics compare` の出力主要列を採用する。`--group-by week` で出力される per-bucket 指標と per-task 指標を組み合わせ、以下 8 列を最低限の判定対象とする:

| 列名 | 区分 | 意味 |
| --- | --- | --- |
| `duration_ms` | 主要 KPI | task assigned → completed までの所要時間。短縮を期待 |
| `tool_call_total` | 副次 | tool 呼び出し総数。試行錯誤の量。減少を期待 |
| `tool_failure_rate` | 副次 | tool 失敗率。減少を期待 |
| `time_to_first_edit_ms` | 副次 | task 開始から最初の Edit/Write までの時間。短縮を期待 |
| `tokens_total` | 副作用監視 | input + output + cache tokens の合計。著増は撤退判定対象 |
| `completion_rate` | 主要 KPI | assigned に対する completed 比率。上昇を期待 |
| `abort_rate` | 副次 | 中断率 (ユーザー abort)。減少を期待 |
| `forced_close_rate` | 副次 | 強制クローズ率。減少を期待 |

このうち `completion_rate` / `abort_rate` / `forced_close_rate` / `tool_failure_rate` は比率系として 2-prop z-test、それ以外の連続値は Welch t-test と Mann-Whitney U で判定する (§ 7)。

---

## 3. 対象 project (10 個の固定リスト)

選定基準: **直近 30 日で `hook_signals > 100` を持つ project** (= 計測に足る活動量がある). 本計画開始時点 (2026-05) で以下 10 個に固定する。途中追加・削除はしない (cohort identity を変えないため):

- `cmux-team`
- `Dear`
- `bun-mot`
- `slack-chan`
- `KDG-lab`
- `ctxd`
- `mado`
- `slaido`
- `AIview`
- `nanobanana-adc`

各 project 内の `.team/metrics/snapshots/YYYY-MM-DD.json` を真のソースとし、本 ctxd repo には集計値しか持ち込まない (§ 9)。

---

## 4. cohort 境界

| cohort | 期間 | 状態 |
| --- | --- | --- |
| baseline | ctxd install 日より前 (retroactive、可能な限り遡る) | ctxd 無効 (= 全 project の過去履歴) |
| evaluation preliminary | install 日 +1 〜 +28 day | ctxd 有効 |
| evaluation 本報告 | install 日 +29 〜 +56 day | ctxd 有効 |
| evaluation 最終確定 | install 日 +57 〜 +84 day | ctxd 有効 |

cohort 境界の唯一の真のソース (SSOT) は本ディレクトリの [`intervention-log.md`](./intervention-log.md) である。ファイルに記録された install タイムスタンプが境界日であり、後続タスク (T026〜T029) のすべての SQL / `cmux-team metrics compare` の `--baseline` / `--comparison` 引数はこの記録に従って組み立てる。

---

## 5. 介入の実体

介入は次の 1 コマンドのみ:

```bash
claude plugins:install @hummer98/ctxd-claude-plugin
```

claude plugin は **user スコープ install** であり、project ごとに on/off できない。本 PLAN.md commit 直後 (本タスク T025 完了直後) にユーザーが手動で実行する。実行と同時に `intervention-log.md` に install タイムスタンプを 1 行追記する責務をユーザーが負う。

撤退・再導入が発生した場合は同様に `uninstall` / `reinstall` 行を append-only で追記する。

---

## 6. 「ctxd 有効」marker の取得方法

trace DB だけでは plugin install 状態を判定できない (cmux-team の `SESSION_STARTED` hook payload は loaded plugin リストを含まない)。よって以下 3 層で運用する:

### L1: 手動 install timestamp (cohort 境界の SSOT、必須)

`intervention-log.md` に append-only で記録する。format:

```
2026-05-XX HH:MM:SSZ  install    @hummer98/ctxd-claude-plugin@<version>
2026-05-YY HH:MM:SSZ  uninstall  (撤退判定発動 / ユーザー判断)
2026-05-ZZ HH:MM:SSZ  reinstall  @hummer98/ctxd-claude-plugin@<version>
```

- すべて UTC ISO-8601。
- install 直後にユーザーが 1 行追記する規律を約束する。
- 撤退や入れ替えがあれば必ず追記する。
- **このファイルが cohort 境界の唯一の真のソース**。本 PLAN.md / 後続レポートはすべてこのファイルを参照する。

### L2: per-task ctxd 使用量 (strata 分析用、自動)

`hook_signals` から SQL で per-task の `ctxd ` Bash 呼び出し回数を集計する:

```sql
SELECT s2t.task_id,
       COUNT(*) AS ctxd_calls
FROM hook_signals h
JOIN (
  SELECT session_id, MIN(task_id) AS task_id
  FROM task_sessions
  WHERE event IN ('assigned','agent_spawned')
    AND task_id IS NOT NULL AND session_id != ''
  GROUP BY session_id
) s2t ON h.session_id = s2t.session_id AND h.session_id != ''
WHERE h.type='PRE_TOOL_USE'
  AND h.tool_name='Bash'
  AND JSON_EXTRACT(h.payload_json,'$.payload.tool_input.command') LIKE 'ctxd %'
GROUP BY s2t.task_id;
```

evaluation 期間の task を `ctxd_calls > 0` (実使用) と `ctxd_calls = 0` (未使用) に strata 分割し、effect の濃淡を見る。baseline 期間の task は SQL 結果に含まれないはずなので、intervention-log.md と一致しない場合は install 日の記録漏れ検出に使える (sanity check)。

### L3: SessionStart 時の loaded plugin 情報 (将来、未実装)

cmux-team の `SessionStart` hook payload に loaded plugin リストを含めれば、session 単位で確実に install 状態が取れる。**本ラウンドの scope 外**。T025 完了後、cmux-team へ feature request issue を出す候補とする (任意作業)。

---

## 7. 統計手法

`cmux-team metrics compare` 標準 (cmux-team spec § 4.3) を継承する:

| 指標種別 | 主検定 | 補助検定 |
| --- | --- | --- |
| 連続値 (`duration_ms`, `tool_call_total`, `time_to_first_edit_ms`, `tokens_total`) | Welch t-test (不等分散頑健) | Mann-Whitney U (非正規・外れ値耐性、副次確認) |
| 比率 (`completion_rate`, `abort_rate`, `forced_close_rate`, `tool_failure_rate`) | 2-prop z-test | — |

多重比較補正は **Benjamini-Hochberg** (cmux-team spec § 4.3 に従う)。判定基準:

- adjusted p < 0.05 を有意とする。
- effect size は連続値で Cohen's d、比率で risk difference を併記する (`compare` 出力にて確認)。
- preliminary (+4w) / 本報告 (+8w) / 最終 (+12w) の各時点で同じ手法を当てる。

---

## 8. 撤退判定

cmux-team spec § 4.4 を継承する。具体的には副作用系 (`tokens.input`, `tokens.output`, `tokens.cache`) のいずれかが **baseline +30% 超 かつ adjusted p < 0.05** の場合に撤退する。

撤退手順:

1. ユーザーが `claude plugins:uninstall @hummer98/ctxd-claude-plugin` を実行する。
2. `intervention-log.md` に `uninstall` エントリを append する (理由欄に「撤退判定発動」と書く)。
3. README の研究セクション (§ 11) に撤退理由と判定根拠 (どの token 系列が +X% かつ p=Y か) を明記する。

副作用系以外 (主要 KPI が劣化、副次が劣化) も撤退対象だが、副作用系がトリガーとして最優先。effect が中立で副作用も無視できる範囲なら継続観察 (= 維持) とする。

---

## 9. 公開ポリシー

ctxd 公開リポジトリ (本 repo) に commit するのは **集計値のみ**:

- 本 `PLAN.md` / `intervention-log.md`
- `cmux-team metrics compare` の出力 JSON (= 集計値)
- 集計表 (Markdown 表) — README に追記する分

raw の per-task / raw snapshot は各 project の `.team/metrics/snapshots/` に閉じ、ctxd 公開リポジトリには **絶対に commit しない**。理由は task 単位のログにユーザーの作業内容や周辺ファイル名が含まれる可能性があるため。

raw を共有する必要が出た場合は集計レイヤを増やす (例: project 名を匿名 ID に置換した二次集計) ことで対応し、本 repo への直接 commit は禁止する。

---

## 10. 限界の明記

本観察的計測は以下の 5 つの限界を持ち、結論はこれらを踏まえて読む:

- **time confounder**: 学習効果・季節性・ユーザー作業内容の時期変動を分離不能。「ctxd 導入後に効率化した」が「同時期にユーザー側のスキル向上で効率化した」と区別できない。
- **selection bias**: ctxd を実際に使った task は使わなかった task と既に違う性質を持つ (難しい task でだけ ctxd を呼ぶ等)。L2 strata 分析で緩和を試みるが完全解消不可。
- **subject-between (project 間) 差を pooled 集計で平均化することの粗さ**: 10 project の特性差 (規模・言語・チーム慣習) を flat に平均すると、特定の project だけで効いている効果が薄まる/誇張される可能性がある。
- **検出力の限界**: n=10 project / 4-12 week では **medium effect size 以上** しか有意化しない可能性がある。small effect は検出できないことを明記しておく。
- **L1 marker の運用依存性**: `intervention-log.md` 追記漏れがあると全分析が破綻する。ユーザーが install 直後に必ず追記する規律に依存しており、これが本計測の最大の脆弱点。

---

## 11. README 追記の構成案

`README.md` / `README.ja.md` の双方に `## Empirical effect study (cmux-team field)` セクションを新設する。段階更新の方針:

| 時点 | 追記内容 |
| --- | --- |
| install +4w (T027) | preliminary 結果。サンプルサイズ、主要 KPI の方向性 (有意/非有意問わず数値)、限界の再掲 |
| install +8w (T028) | 本報告。adjusted p 付きで table を提示、撤退判定の現状を併記 |
| install +12w (T029) | 最終確定。総合判定 (継続/撤退)、次フェーズ (Stage 3) への申し送り |

各時点とも cohort 範囲 (`intervention-log.md` 記載のタイムスタンプ + ±N day) を明記し、再現可能な形で書く。

---

## 12. 後続タスクの index

| Task ID | 内容 | 依存 |
| --- | --- | --- |
| T026 | retroactive baseline 生成 (10 project × 過去 N 日の snapshot 一括生成) + ユーザーによる ctxd install 実施 + `intervention-log.md` 記録 | T025 完了 |
| T027 | install +4w で `cmux-team metrics compare` を全 project 横断実行 + preliminary を README に追記 | T026 完了 + install から 4 週経過 |
| T028 | install +8w で本報告 (`compare` 再実行 + README 更新 + 撤退判定チェック) | T027 完了 + install から 8 週経過 |
| T029 | install +12w で最終確定 (`compare` 再実行 + README 最終更新 + Stage 3 への申し送り) | T028 完了 + install から 12 週経過 |
