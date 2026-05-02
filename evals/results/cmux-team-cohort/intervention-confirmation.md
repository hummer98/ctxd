# cmux-team field study — intervention confirmation

このドキュメントは PLAN.md で定義した cohort study の **介入完了 (Phase 2) の集計エビデンス**。
intervention-log.md の install entry に対応する 検証結果を 1 ファイルにまとめる。

## 介入の発生

- **install timestamp** (UTC): `2026-05-02 04:22:35Z`
- **plugin id**: `ctxd@hummer98-ctxd@0.2.0`
- **install scope**: `user` (`~/.claude/plugins/cache/hummer98-ctxd/ctxd/0.2.0/`)
- **install 経路**:
  ```
  claude plugin marketplace add hummer98/ctxd
  claude plugin install ctxd@hummer98-ctxd
  ```

`claude plugins list --json` の該当エントリ:

```json
{
  "id": "ctxd@hummer98-ctxd",
  "version": "0.2.0",
  "scope": "user",
  "enabled": true,
  "installPath": "/Users/yamamoto/.claude/plugins/cache/hummer98-ctxd/ctxd/0.2.0",
  "installedAt": "2026-05-02T04:22:35.678Z",
  "lastUpdated": "2026-05-02T04:22:35.678Z"
}
```

## SESSION_STARTED の loadedPlugins 検証 (T410 marker)

cmux-team v4.24.0 (T410 含む) の daemon が capture した最初の post-install SESSION_STARTED:

```sql
SELECT timestamp, JSON_EXTRACT(payload_json, '$.source') AS src,
       JSON_EXTRACT(payload_json, '$.loadedPlugins') AS plugins
FROM hook_signals
WHERE type='SESSION_STARTED'
  AND JSON_EXTRACT(payload_json, '$.loadedPlugins') IS NOT NULL
ORDER BY rowid DESC LIMIT 1;
```

結果:

```
2026-05-02T06:20:50.596Z | clear | [
  "claude-code-setup@claude-plugins-official",
  "claude-md-management@claude-plugins-official",
  "cmux-team@hummer98-cmux-team",
  "code-review@claude-plugins-official",
  "code-simplifier@claude-plugins-official",
  "context7@claude-plugins-official",
  "ctxd@hummer98-ctxd",                            ← 介入対象
  "dx@ykdojo",
  "skill-creator@claude-plugins-official",
  "slack-chan@slack-chan-marketplace",
  "swift-lsp@claude-plugins-official",
  "typescript-lsp@claude-plugins-official",
  "using-cmux@hummer98-using-cmux"
]
```

`ctxd@hummer98-ctxd` が配列に含まれていることを確認。これにより cohort 分析時に
SQL filter で「ctxd plugin が loaded だった session」を抽出可能 (PLAN.md § 6 の
3 層 marker のうち L3 が稼働開始)。

cohort 分析の SQL idiom:

```sql
SELECT session_id
FROM hook_signals
WHERE type = 'SESSION_STARTED'
  AND JSON_TYPE(payload_json, '$.loadedPlugins') = 'array'
  AND EXISTS (
    SELECT 1 FROM JSON_EACH(payload_json, '$.loadedPlugins')
    WHERE value = 'ctxd@hummer98-ctxd'
  );
```

## retroactive baseline snapshot 生成結果

T026 plan の Step 3 (batch script) を `unset PROJECT_ROOT` 修正版で実行した結果:

| project | snapshots (28 day window) | events.jsonl |
|---|---|---|
| cmux-team | **27** | ✓ |
| Dear | **28** | ✓ |
| bun-mot | **27** | ✓ |
| slack-chan | **28** | ✓ |
| ctxd | **28** | ✓ |
| mado | **28** | ✓ |
| slaido | **28** | ✓ |
| KDG-lab | 0 | ✗ (events.jsonl 不在 → cohort 対象外) |
| AIview | 0 | ✗ (同上) |
| nanobanana-adc | 0 | ✗ (同上) |

合計 **194 snapshots** が `<project>/.team/metrics/snapshots/<YYYY-MM-DD>.json` に
schema_version=1 で生成された (cmux-team 2 + ctxd 2 = 4 件は T025/T026 PoC で先行生成済)。

cohort は当初 10 project から **7 project に縮小** (events.jsonl の有無で判定)。
n=194 snapshot は +4w/+8w/+12w cohort 比較に十分な統計力を持つ。

## 技術的気付き (PROJECT_ROOT 環境変数の挙動)

T026 Conductor の当初診断 ("daemon binding により snapshot CLI が cwd を無視する") は
**誤りだった**。実際は cmux-team daemon が `PROJECT_ROOT` 環境変数を export してこの env を
継承した子 process では `findProjectRoot()` がその値を返すため。

`unset PROJECT_ROOT` してから `cmux-team metrics snapshot` を呼べば cwd が projectRoot として
解決される (`main.ts:150-166` の `findProjectRoot()` 第 2 ロジック)。daemon 操作は不要。

この気付きは cmux-team の `metrics snapshot` を batch 用途で使う際の重要なノウハウ:

```bash
# 各 project の snapshot を一気に作る
for proj in <projects>; do
  (cd "$proj" && env -u PROJECT_ROOT cmux-team metrics snapshot --date X --force)
done
```

PLAN.md / docs/spec/11-metrics.md には現状この note が無い。後続改善候補:
1. cmux-team metrics に `--project-root <path>` flag を追加 (CLI 側で完結、env 操作不要)
2. cmux-team docs/spec/11-metrics.md §3.5.1 系列に "batch use case" の節を追加

## cohort 構成 (確定)

- **対象 project (7)**: `cmux-team` / `Dear` / `bun-mot` / `slack-chan` / `ctxd` / `mado` / `slaido`
- **baseline cohort 期間**: 介入時刻 (2026-05-02 04:22:35Z) より前。retroactive snapshot で
  生成済の `<project>/.team/metrics/snapshots/YYYY-MM-DD.json` (各 project 27〜28 日分)
- **evaluation cohort**:
  - +4w 終端: 2026-05-30 04:22:35Z
  - +8w 終端: 2026-06-27 04:22:35Z
  - +12w 終端: 2026-07-25 04:22:35Z

## 後続タスク

- **T031 (新規起票予定)**: install +4w で `cmux-team metrics compare` を実行
  - schedule 予定日: 2026-05-30 (UTC)
  - 入力: baseline = retroactive 28d / comparison = +1〜+28d (= 介入後 28d)
  - 出力: `evals/results/cmux-team-cohort/+4w/compare.json` に集計値を commit
  - README.md / README.ja.md の `## Empirical effect study (cmux-team field)` セクションに
    preliminary 結果を追記
- **T032 (+8w)**: 2026-06-27 schedule、本報告
- **T033 (+12w)**: 2026-07-25 schedule、最終確定

T031〜T033 の起票は Master が install +28 day 直前に行う。
