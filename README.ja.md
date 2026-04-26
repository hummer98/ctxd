# ctxd

[English](README.md) | **日本語**

**AI エージェントに構造化されたコンテキストを渡す、宣言的な CLI コマンド群です。**

`ctxd` は AI エージェントが見失いがちなシェル操作 — `cd`、`export`、`git checkout` — をラップし、何が変わったのかをエージェントが正確に把握できるよう構造化された JSON を返します。

```sh
# Before: 何も出力されず、エージェントは推測するしかない
cd /foo && git checkout main

# After: エージェントが状態を直接観測できる
ctxd chdir /foo
# {"ok":true,"cmd":"chdir","result":{"cwd":"/foo","git_branch":"main","listing":["src","docs","go.mod"]}}

ctxd git-switch main
# {"ok":true,"cmd":"git-switch","result":{"branch":"main","dirty":false,"ahead":0,"behind":0}}
```

---

## なぜ必要か

### サイレント CLI 問題

Unix の「沈黙の規則」 — 成功したコマンドは何も出力しない — は、コンテキストを暗黙のうちに知覚できる人間のオペレータを前提に設計されています。AI エージェントにとって、これは構造的な盲点となります。

| コマンド | 暗黙に行うこと | エージェントが失う情報 |
|---|---|---|
| `cd /foo` | カレントディレクトリ変更 | 新しい cwd、git ブランチ、ファイル一覧 |
| `export FOO=bar` | 環境変数を設定 | どの変数が変わり、現在の値は何か |
| `git checkout main` | ブランチ切替 | ブランチ、dirty 状態、リモートとの差分 |
| `kill -STOP <pid>` | プロセス停止 | プロセスの状態 |
| `umask 022` | ファイル作成マスクを変更 | 新規ファイルの実効パーミッション |

エージェントの唯一の挽回手段は追加コマンド（`pwd`、`env`、`git status`）を発行することですが、追加トークンと往復が発生します。あるいは文脈から推論することになりますが、長い会話ではドリフトしてしまいます。

### Transformer は逐次的状態を追跡できない

Mozer ら ["The Topological Trouble With Transformers"](https://arxiv.org/abs/2604.17121) (2026) はこの問題を形式化しています。フィードフォワード型アーキテクチャは深さ方向に進化する状態を保持できません。エージェントセッションが長くなるほど、自分が今どこにいるのかを再構築するために外部信号への依存が高まります。

これはプロンプトエンジニアリングで解決できる問題ではありません。構造的な問題です。だから解決策も構造的でなければなりません。**変化が起きたその時点で、状態を外部化し機械可読にする** ことが鍵となります。

### インフラ業界では既にこれを解決している

サーバーオーケストレーションは 2010 年代に同じ問題に直面しました。命令的なシェルスクリプトは状態がドリフトし、暗黙的で、失敗が不透明でした。業界は Terraform、Kubernetes、GitOps へ収束しました。意図を宣言し、事後条件を検証し、構造化された差分を報告する、というアプローチです。

`ctxd` は同じパターンを、AI エージェントが最も多用する操作に絞ってローカルシェルに適用します。

---

## 仕組み

`ctxd` のすべてのコマンドは次の流れで動作します。

1. 対応する操作を**実行する**
2. 結果として生じた状態を**観測する**
3. 構造化された JSON レポートを**返す**

```json
{
  "ok": true,
  "cmd": "chdir",
  "args": ["/foo"],
  "result": {
    "cwd": "/foo",
    "git_branch": "main",
    "listing": ["src", "docs", "go.mod", "README.md"]
  },
  "postcondition": { "passed": true, "checks": [] },
  "elapsed_ms": 4
}
```

失敗時は次のようになります。

```json
{
  "ok": false,
  "cmd": "chdir",
  "args": ["/nonexistent"],
  "error": {
    "code": "path_not_found",
    "message": "no such file or directory: /nonexistent",
    "retryable": false
  }
}
```

### 事後条件 (Postconditions)

コマンド実行後にどのような状態であるべきかを宣言できます。`ctxd` がそれを検証し、明確な pass/fail を返します。

```sh
ctxd git-switch main --expect branch=main --expect dirty=false
```

```json
{
  "ok": true,
  "postcondition": {
    "passed": true,
    "checks": [
      {"key": "branch", "expected": "main", "actual": "main", "passed": true},
      {"key": "dirty",  "expected": "false","actual": "false","passed": true}
    ]
  }
}
```

---

## コマンド (MVP)

| コマンド | 置き換え対象 | 主な出力フィールド |
|---|---|---|
| `ctxd chdir <path>` | `cd` | `cwd`, `git_branch`, `listing` |
| `ctxd git-switch <branch>` | `git checkout` / `git switch` | `branch`, `dirty`, `ahead`, `behind` |
| `ctxd env-set <KEY=val>…` | `export` | `set`, `diff.added`, `diff.changed` |

`--human` フラグでデバッグ用の人間可読出力に切り替えられます。

### `ctxd chdir`

パスを絶対化し、ディレクトリ内容と git ブランチ (あれば) を JSON で返します。

```sh
ctxd chdir /path/to/repo
```

```json
{
  "ok": true,
  "cmd": "chdir",
  "args": ["/path/to/repo"],
  "result": {
    "cwd": "/path/to/repo",
    "git_branch": "main",
    "listing": ["docs", "go.mod", "src"]
  },
  "elapsed_ms": 3
}
```

`git_branch` はパスが git working tree の外にある場合や detached HEAD の場合は `null` を返します。
エラー時は `ok: false` となり、`error.code` は `not_found`（パスが存在しない）または `not_a_directory`（ファイルを指定した）になります。

親シェルの cwd は変更されません。返却された `cwd` を次のコマンドへ渡し直す前提です。

---

## インストール

> 開発中です。バイナリリリースは近日公開予定です。

**Claude Code プラグイン** (Claude Code ユーザー向け推奨):

```sh
claude plugins:install @hummer98/ctxd-claude-plugin
```

`ctxd` バイナリと、Claude にその使い方を教える Skill の両方をインストールします。

**スタンドアロン CLI:**

```sh
npm install -g @hummer98/ctxd
# もしくは
brew install hummer98/tap/ctxd
```

---

## Skill バンドル

`ctxd` には [Anthropic Agent Skills 仕様](https://agentskills.so/)に準拠した [SKILL.md](skills/ctxd/SKILL.md) が同梱されています。

この Skill は使用を強制しません。あくまでナッジです。エージェントが `cd`、`export`、`git checkout` に手を伸ばしたとき、`ctxd` の同等コマンドを提示し、それを使うことで何のコンテキストが得られるかを説明します。採用するかどうかはエージェントの判断に委ねられます。

Claude Code、OpenCode、Codex、Cursor、Gemini CLI など Agent Skills をサポートするあらゆるホストと互換です。

---

## 設計原則

- **既存コマンドを上書きしない** — 新しいコマンド名のみ採用し、`cd` などへの alias は提供しません
- **JSON をデフォルトに、人間向けはオプション** — 読みやすい出力が必要なときは `--human` を使います
- **事後条件はオプトイン** — 必要なときに使え、不要なときには見えません
- **狭く深く** — POSIX 全体ではなく、上位 20〜30 コマンドを質高く実装します
- **拡張可能** — ユーザーが独自の宣言的ラッパーを追加できます

---

## 開発手順

### 前提

- Go 1.26 以降

### ビルド

```sh
go build -o ctxd ./cmd/ctxd
```

### 実行

```sh
./ctxd --version
./ctxd --help
```

### テスト

```sh
go test ./...
```

### 設計判断

CLI フレームワーク選定など、設計上の判断は [`docs/adr/`](docs/adr/) を参照してください。

---

## ステータス

初期開発段階です。設計は固まっていますが、実装はこれからです。

コントリビューション、フィードバック、ユースケース報告は [issues](https://github.com/hummer98/ctxd/issues) で歓迎します。

---

## ライセンス

MIT
