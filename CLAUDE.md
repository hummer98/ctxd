# CLAUDE.md

このリポジトリで作業する Claude (および人間メンテナ) 向けの運用ルール。

## SKILL バージョンアップ運用

`skills/ctxd/SKILL.md` の本文または frontmatter `description` を更新したら、必ず `.claude-plugin/plugin.json` の `version` を semver でバンプすること。

理由: `evals/run.sh` の harness は SKILL の改訂前後の遵守率を比較するためのものであり、`plugin.json` の `version` がその「計測の単位」になる。version を据え置いたまま SKILL.md を変更すると、`evals/results/index.md` 上で同一 version の数値が混ざり、計測が比較不能になる。

### バンプの目安

| 種別 | 目安 |
| --- | --- |
| patch (例: 0.1.0 → 0.1.1) | 表現修正、typo、例の追加など Skill が「選ばれる/呼ばれる」挙動に影響しない範囲 |
| minor (例: 0.1.0 → 0.2.0) | 追加コマンド対応、description の意味的修正、新シナリオ向けの章追加など、エージェントの判断が変わりうる変更 |
| major (例: 0.1.0 → 1.0.0) | 互換破壊 (コマンド削除、JSON envelope schema 変更など) |

### バンプ時の付随作業

- **scenarios の追加・更新を検討する**: 新コマンドや新章を SKILL に足したなら、`evals/scenarios.jsonl` にもそれを試すシナリオを追加すべきか考える。既存 scenario の `expected_args_pattern` も、SKILL.md の変更によって意図が変わっていないか確認する。
- **`.claude-plugin/plugin.json` の `description` の整合性を確認する**: SKILL.md frontmatter の `description` (長文 / when-to-use の本文) と `.claude-plugin/plugin.json` の `description` (短文要約、`claude plugin validate` の上限内) は独立に管理する。SKILL.md の意図が大きく変わったら plugin.json 側の要約も合わせて更新する。Q1 reviewer note 参照。
- **派生先は手動同期しない**: `evals/.eval-plugin/.claude-plugin/plugin.json` の `version` は `evals/run.sh` の起動時に真のソース (`.claude-plugin/plugin.json`) から動的に再生成される。手動同期は不要。

## 真のソースの位置

| 対象 | 真のソース |
| --- | --- |
| プラグイン version / name / description | `.claude-plugin/plugin.json` |
| Skill 本文 / when-to-use | `skills/ctxd/SKILL.md` |
| ctxd CLI version | `cmd/ctxd/main.go` (現状 `0.0.0-dev` 固定、本タスク範囲外) |

## 計測結果の commit 方針

`evals/results/<timestamp>/summary.md` と `evals/results/index.{md,csv}` は commit 対象 (軽量メタ)。重い `session-*.jsonl` / `session-*.meta.json` は `.gitignore` で除外。再現したい場合は再 run すれば良い。

git SHA / branch は `summary.md` ヘッダと `index.{md,csv}` に併記される。version が同一でも実装が違う期間の比較を防ぐための trace 用情報。判定の単位はあくまで `plugin.json` の `version`。
