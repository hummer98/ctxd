# ctxd — AI エージェント向け宣言的 CLI コマンド群

## このドキュメントの目的

このプロジェクトを引き継ぐ AI エージェントが「なぜ作るか・何を作るか・どう作るか」を一読で把握できる申し送り文書。

---

## プロジェクトミッション

**transformer の state tracking 弱さ × POSIX CLI の silent state mutation 問題を、宣言的 CLI + Skill bundle で補強する。**

AI エージェントは shell コマンドの "rule of silence"（成功時無音）に弱い。`cd`, `export`, `git checkout` 等は実行しても何も出力しないため、エージェントは「自分が今どこにいるか・何が変わったか」を推論で復元しなければならない。`ctxd` はこれらを宣言的なラッパーで置き換え、構造化 JSON で状態変化を出力する。

### 解決する問題

```
# 現状（AI が状態を見失う）
cd /foo                   # 無音。AI は次のコマンドで pwd を確認する必要がある
export FOO=bar            # 無音。環境変数が設定されたかどうか不明
git checkout main         # 出力はあるが構造化されていない

# ctxd（状態変化を AI に渡す）
ctxd chdir /foo           # {"cwd":"/foo","git_branch":"main","listing":["src","docs",...]}
ctxd env-set FOO=bar      # {"set":{"FOO":"bar"},"diff":{"added":["FOO"]}}
ctxd git-switch main      # {"branch":"main","dirty":false,"ahead":0,"behind":0}
```

---

## 背景・先行研究

- Mozer et al. "The Topological Trouble With Transformers" (arXiv:2604.17121) — transformer が sequential state tracking を苦手とする理論的根拠
- InfoQ "Keep the Terminal Relevant" — agent-friendly CLI design pattern の解説
- CLI vs MCP 比較: CLI が token で 10〜32 倍安く、信頼性も高い
- cmux-team (hummer98/cmux-team) の設計判断（worktree 隔離・FSM 外部化・CLI 強制）はすべてこの問題への構造的対応として収束 → 詳細は issue hummer98/cmux-team#41

---

## 配布アーキテクチャ

各エージェントプラットフォームのネイティブプラグインとして個別配布する。単一 npm package に dual manifest を詰め込む方式は採らない。

```
ctxd-claude-plugin/   ← npm publish → claude plugins:install @hummer98/ctxd-claude-plugin
  .claude-plugin/plugin.json
  skills/ctxd/SKILL.md
  bin/ctxd             (Go static binary, platform 別 prebuilt)

ctxd-opencode/        ← OpenCode Plugin として配布
  opencode.json
  skills/ctxd/SKILL.md
  bin/ctxd

ctxd/                 ← CLI only（エージェント非依存の素インストール用）
  bin/ctxd
```

SKILL.md の本文（nudge 内容・postcondition 構文）は共通、プラットフォーム固有のメタデータだけ差分。

---

## 実装言語: Go

- Static binary → プラグインへの bundle が最もシンプル
- Cross-platform prebuilt binary の npm 配布パターンが確立済み（esbuild / biome と同手法）
- 起動オーバーヘッドなし（AI がループで呼ぶことを想定）
- cmux-team (TypeScript/Bun) と言語分離 → 依存の独立性が明確

---

## 設計原則

1. **既存コマンドを上書きしない** — 新コマンド名で並走。`cd` を alias で潰さない
2. **Output: JSON first, human optional** — `--human` フラグで整形出力
3. **Postcondition は optional** — `--expect` で書きたいときだけ書く
4. **Skill side の nudge で adoption を駆動** — 強制ではなく AI に選好させる
5. **狭く深く** — top 20〜30 コマンドの quality を高く保つ。網羅性は捨てる
6. **Plugin 化可能** — ユーザーが独自の declarative wrapper を追加できる拡張 API

---

## 実装方針 (Implementation Strategy)

候補コマンド 20〜30 個（FS / env / git / プロセス / 外部ツール wrapper / ネットワーク / テキスト）の分布を踏まえ、以下の方針で実装する。

### 一次方針: stdlib + shell-out のハイブリッド

- **FS / env / プロセス / ネットワーク / テキスト**: Go stdlib (`os`, `os/exec`, `net/http`, `path/filepath`) で完結
- **git 系**: `git` CLI を shell-out。`--porcelain=v2` / `-z`（NUL 区切り）で構造化出力を取り、parsing helper を `internal/gitops` に集約
- **外部ツール wrapper (`npm-install` / `build` / `test` 等)**: shell-out 一択

### SDK 取り込み方針

- **採用しない**:
  - `libgit2` 系（cgo 必要 → static binary 原則違反）
  - `go-git/go-git`（bundle +5MB、MVP では shell-out で十分。将来 hot path で必要になれば `internal/gitops` interface 越しに差し替え可能）
- **局所採用**:
  - `godotenv` — `env-load` の `.env` parsing
  - `doublestar` — glob が必要になった場合のみ
- **CLI フレームワーク**: `spf13/cobra` または `urfave/cli/v3` を MVP から採用

### parent shell 問題への扱い

`ctxd chdir` / `ctxd env-set` は子プロセスとして起動されるため親シェルに副作用を返せない。MVP では **JSON で結果を返すのみ** とし、AI エージェントが次のコマンドで cwd / env を渡し直す前提で設計する。`eval $(ctxd env-set ...)` のような shell-eval モードは将来オプションとして検討余地を残す。

---

## MVP スコープ（最初の 3 コマンド）

| コマンド | 置き換え対象 | 出力フィールド |
|---|---|---|
| `ctxd chdir <path>` | `cd` | `cwd`, `git_branch`, `listing` |
| `ctxd git-switch <branch>` | `git checkout / switch` | `branch`, `dirty`, `ahead`, `behind` |
| `ctxd env-set <KEY=val>` | `export` | `set`, `diff.added`, `diff.changed` |

### 出力仕様（共通）

```json
{
  "ok": true,
  "cmd": "chdir",
  "args": ["/foo"],
  "result": { ... },
  "postcondition": { "passed": true, "checks": [] },
  "elapsed_ms": 3
}
```

エラー時は `"ok": false` + `"error": { "code": "...", "message": "...", "retryable": false }`

---

## SKILL.md の位置づけ

Anthropic Agent Skills 仕様 (agentskills.so) 準拠の `SKILL.md` を同梱する。役割は **nudge**：

- AI が `Bash` で `cd` / `export` / `git checkout` を呼ぼうとしたとき `ctxd` の対応コマンドを検討させる
- postcondition syntax と使いどころを例示する
- 強制ではなく「こっちの方が状態が見えるよ」と誘導する

---

## ディレクトリ構造

```
ctxd/
├── cmd/ctxd/main.go        エントリポイント
├── internal/
│   ├── runner/             コマンドディスパッチ
│   ├── output/             JSON / human 出力フォーマット
│   └── postcondition/      --expect 検証ロジック
├── skills/ctxd/SKILL.md    Agent Skills 仕様準拠
├── .claude-plugin/
│   └── plugin.json         Claude Code Plugin マニフェスト
├── docs/
│   └── seed.md             このファイル
├── go.mod
└── README.md
```

---

## コーディング規約

- **コメント・ドキュメント**: 日本語
- **コード（変数名・関数名・フラグ名）**: 英語
- **出力 JSON のキー**: snake_case
- **CLI フラグ**: `--long-form`（短縮形は主要なものだけ）

---

## オープンな問い（次にやること）

- [ ] MVP 3 コマンドの実装
- [ ] `--expect` postcondition の DSL 設計（シンプルな key=value チェックから始める）
- [ ] Cross-platform prebuilt binary の npm 配布スクリプト
- [ ] SKILL.md の初稿（nudge 条件の書き方）
- [ ] Claude Code Plugin マニフェスト (`plugin.json`) の初稿
- [ ] OpenCode Plugin マニフェスト (`opencode.json`) の初稿
- [ ] プロジェクト名/コマンド名の競合再確認（`ctxd` は 2026-04-26 時点でクリーン）
- [ ] 効果測定方法（state tracking 失敗率の before/after 比較プロトコル）

---

## 関連リンク

- 発案 issue: https://github.com/hummer98/cmux-team/issues/41
- cmux-team（dogfooding 環境）: https://github.com/hummer98/cmux-team
- Agent Skills 仕様: https://agentskills.so/
- GitHub `gh skill`: https://github.blog/changelog/2026-04-16-manage-agent-skills-with-github-cli/
