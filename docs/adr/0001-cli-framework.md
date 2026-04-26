# ADR 0001: CLI フレームワークの選定

## Status

Accepted (2026-04-26)

## Context

`ctxd` は AI エージェント向けに「シェル操作を構造化された JSON で返す」CLI を提供する。`docs/seed.md` で示すとおり、MVP の 3 コマンド (`chdir` / `git-switch` / `env-set`) を皮切りに、将来 20〜30 個程度のサブコマンドにスケールする想定がある。

そこで CLI フレームワークの選定が必要になる。標準ライブラリ `flag` のみで書く案も含め、以下を候補として検討した。

- `spf13/cobra`: kubectl, gh, hugo, helm, docker, kustomize 等の主要 OSS が採用するデファクト。`pflag` ベースで persistent flag、completion 生成、help テンプレ差し替えに対応。
- `urfave/cli/v3`: 軽量な代替。stdlib に近い依存量。v3 系は新しい Go の build tag 対応も良好。
- `flag` (stdlib のみ): subcommand サポートが弱く、20〜30 コマンドへのスケールが難しい。

選定の評価軸は次のとおり。

1. サブコマンド階層の表現力（将来 20〜30 コマンドに耐えるか）。
2. AI エージェントが学習しやすい / 出力する補助スクリプトが既存パターンに沿うか。
3. `--version` / `--help` の即時動作（タスク 006 の完了条件）。
4. Go 1.26.x との互換性。
5. 依存量の許容範囲（static binary 配布を前提にするので moderate な増加は許容）。

## Decision

**`spf13/cobra` の `v1.8.x` 系を採用する。**

採用バージョン: `v1.8.x` 系（minor pin）。`go get github.com/spf13/cobra@v1.8` で取得し、`go.mod` の require に記録する。

採用理由:

1. サブコマンドの木構造が自然にスケールする。`rootCmd.AddCommand(...)` で追加していけるため、20〜30 コマンドに育てても見通しが良い。
2. AI エージェント向けツールという性質上、エージェント自身が読み書きしやすい「コミュニティの慣習に沿った構造」が望ましい。cobra ベースの主要 OSS が多いため、AI が出力する補助スクリプトも自然に整う。
3. `cobra.Command.Version` フィールドで `--version` フラグが自動有効化され、`--help` も自動生成される。タスク 006 の完了条件にぴったり合う。
4. Go 1.26 互換は問題なし（`v1.8.x` 系で動作確認実績あり）。
5. bundle サイズの増加は static binary 配布の範囲では許容できる。

不採用理由:

- `urfave/cli/v3`: 軽量さでは cobra に勝るが、本プロジェクトのスケール想定（サブコマンド 20〜30）と前例数を踏まえると差し引きでメリットが薄い。将来 plugin として外部から CLI を拡張する設計を考えると、cobra の `AddCommand` / persistent flag のシンプルさのほうが扱いやすい。
- `flag` (stdlib): subcommand 階層を自前で組む必要があり、規模に対するコストが高い。

## Consequences

### Positive

- 主要 OSS 群の慣習に揃うため、AI エージェントによる補完・生成が安定する。
- `--version` / `--help` / completion / man page 生成といった「CLI に当然欲しい機能」をフレームワーク任せにできる。
- persistent flag (`--human`, `--expect`) を root レベルで一括宣言できる。

### Negative

- 依存量がわずかに増える（`cobra` + `pflag` + 推移的依存）。static binary 配布の範囲では問題なし。
- 将来差し替えたくなった場合、`cmd/ctxd/main.go` と `internal/runner` のアダプタ層に変更が及ぶ。ただしサブコマンドの本体は `internal/runner.Command` interface に閉じる設計とするため、影響範囲は限定される。

### Follow-ups

- `cobra` バージョンは `v1.8.x` 系で minor pin する。major アップデート時は別 ADR で再評価する。
- exit code の規約（成功 0 / エラー 1 / postcondition 違反の差別化）は将来 `docs/adr/0002-exit-codes.md` で別途議論する。
