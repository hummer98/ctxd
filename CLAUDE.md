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
- **README の統計テーブルを更新する**: 新しい version で本走 (5 scenarios 揃いで harness 完走した run) を取ったら、`README.md` の `### Adherence over plugin versions` と `README.ja.md` の `### バージョン別 SKILL 遵守率` に 1 行追記する。採用 run の選別基準は「5 scenarios 揃い / harness-aborted でない / 同一 version 内で最新」。自動生成スクリプトは現時点で未整備なので手動運用。

## モデル ID の記録 (T022)

`evals/run.sh` は claude のモデル ID を `EVAL_MODEL` 環境変数 (default `claude-opus-4-7`) で受け取り、`claude --model <id>` として渡すと同時に `summary.md` ヘッダ / `evals/results/index.{md,csv}` の `model` 列に記録する。

理由: 同じ `plugin.json` version でも実行モデルが違えば遵守率は別物になる。model は plugin version と並ぶ「計測の identity」の一部。

### 運用ルール

- **モデルを変更したら eval を必ず再計測する**。過去の baseline と直接比較するためには同一モデルで取り直すこと。
- `EVAL_MODEL=claude-sonnet-4-5 bash evals/run.sh` のように他モデルを試すときも、index は新 run として 1 行追加されるだけで、過去 run と混ざらない (識別は `(timestamp, plugin_version, model, git_sha)` の組)。
- `README.md` / `README.ja.md` の統計テーブルにも `model` 列があるので、新 run を追記するときは plugin version と並べて model を必ず埋めること。

## 真のソースの位置

| 対象 | 真のソース |
| --- | --- |
| プラグイン version / name / description | `.claude-plugin/plugin.json` |
| npm package version (`package.json`) | `.claude-plugin/plugin.json` (`scripts/sync-package-version.sh` で同期) |
| Skill 本文 / when-to-use | `skills/ctxd/SKILL.md` |
| ctxd CLI version | `cmd/ctxd/main.go` の `var version` (build 時 `-ldflags -X main.version=...` で上書き)。tag は `.claude-plugin/plugin.json` の `version` と同期する (1 tag = 1 plugin version = 1 CLI version)。release ops の詳細は T030 で別途記録。 |

## plugin の npm publish 手順

`@hummer98/ctxd-claude-plugin` を npm に publish するときの手順。

> **このセクションの手動手順は bootstrap (初回 publish v0.2.0) 用 + 緊急時の fallback**。通常運用では publish は CI 経由 (OIDC trusted publisher; T029 で `.github/workflows/release.yml` 整備) で行う。
>
> **bootstrap publish 自体は T027 の直後に別 surface で user (yamamoto) 監視下に実行する**。T027 (本ファイルでこのセクションを書いたタスク) は publish 経路の整備 (LICENSE / package.json / sync スクリプト / dry-run 検証) までで完了し、実 publish はしない。

なお `.claude-plugin/plugin.json` の `name` (`ctxd`) は Claude plugin の内部 ID、npm package の `name` (`@hummer98/ctxd-claude-plugin`) は npm registry 上の配布名であり、両者は別名前空間で管理する。npm package name は本セクションの「真のソースの位置」表外で固定 (rename には npm 上の package 移行作業を伴うので軽率に変えない)。

### bootstrap publish 手順 (T027 直後に user が別 surface で 1 回だけ実行)

1. `npm whoami` で `hummer98` でログイン済みか確認
2. `cd <ctxd repo root>` (worktree でも main checkout でも OK、ただし v0.2.0 のファイル群が揃った state で行う)
3. `bash scripts/sync-package-version.sh` で `package.json` の version を `.claude-plugin/plugin.json` と一致させる (既に一致なら no-op)
4. `npm publish --access public --dry-run` で tarball 内容を確認 (6 ファイル: `.claude-plugin/plugin.json`, `skills/ctxd/SKILL.md`, `LICENSE`, `README.md`, `README.ja.md`, `package.json`。forbidden パターン: `cmd/`, `internal/`, `go.{mod,sum}`, `evals/`, `.team/`, `.worktrees/`, `.claude/`, `docs/`, `CLAUDE.md`, `scripts/` が含まれていないこと)
5. `npm publish --access public` で本 publish (2FA OTP 要求があれば `--otp=<6桁>` を付与)
6. `npm view @hummer98/ctxd-claude-plugin version` で `0.2.0` が返ることを確認
7. (任意) `claude plugins:install @hummer98/ctxd-claude-plugin` で動作確認

### bootstrap 完了後 / T029 着手前に user が行う設定 (trusted publisher)

T029 で `.github/workflows/release.yml` を整備する前に、user は npmjs.com 上で以下を設定しておくこと:

1. https://www.npmjs.com/package/@hummer98/ctxd-claude-plugin → Settings → Trusted Publishers
2. Provider: GitHub Actions
3. Organization or user: `hummer98`
4. Repository: `ctxd`
5. Workflow filename: `release.yml`
6. Environment name (推奨): `release`

これが未設定のまま T029 の CI が `npm publish` を試みると OIDC token を受け付けず failed publish になる。

### 通常運用 (T029 完了後)

通常の version bump リリースは以下:

1. `.claude-plugin/plugin.json` の `version` を semver で bump (CLAUDE.md 既存ルール)
2. `bash scripts/sync-package-version.sh` で `package.json` を同期
3. commit / push / `git tag v<X.Y.Z>` / `git push --tags`
4. tag push で T029 の CI workflow (`release.yml`) が `npm publish` を OIDC で実行
5. `npm view @hummer98/ctxd-claude-plugin version` で新 version を確認

publish 後 24 時間以内のみ `npm unpublish @hummer98/ctxd-claude-plugin@<X.Y.Z>` で取り消し可。それ以降は `npm deprecate` で警告を付けるのみ。dry-run で必ず内容を確認すること。

`description` は `.claude-plugin/plugin.json` と `package.json` で手動で揃える (現状自動同期はしない)。SKILL.md / plugin.json の description を変えるときは package.json の description も合わせて更新すること。

本 repo はメインが Go プロジェクトだが、plugin 配布のために repo ルートに `package.json` を置いている。`npm install` を実行しても dependencies がないため副作用は空 `node_modules/` が作られるだけで実害はないが、Go 側の作業では基本的に `npm` コマンドは不要。

## 計測結果の commit 方針

`evals/results/<timestamp>/summary.md` と `evals/results/index.{md,csv}` は commit 対象 (軽量メタ)。重い `session-*.jsonl` / `session-*.meta.json` は `.gitignore` で除外。再現したい場合は再 run すれば良い。

git SHA / branch は `summary.md` ヘッダと `index.{md,csv}` に併記される。version が同一でも実装が違う期間の比較を防ぐための trace 用情報。判定の単位はあくまで `plugin.json` の `version`。
