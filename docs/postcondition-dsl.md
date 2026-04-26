# `--expect` postcondition DSL

## 1. 概要

`--expect` は、ctxd のサブコマンド実行後に「結果 (Result.Data) が期待した状態になっているか」を 1 行の命題で表明するための DSL である。AI エージェントが「コマンドを実行 → 期待した状態に至ったか」を 1 つの JSON で判断できるようにすることを目的とする。

- 1 つの `--expect` には 1 つの命題 (assertion) を書く
- 複数の `--expect` を指定すると AND で合成される
- いずれかの命題が不成立なら `result.ok=false` / `error.code=postcondition_failed` で終了する (exit 1)
- 失敗時も `Result.Data` は保持されるため、AI は「期待値 (Postcondition.Checks[].Expected)」と「実観測値 (result.<key>)」を 1 つの JSON で対比できる

## 2. 基本構文

```
KEY OP VALUE
```

- `KEY`: ドット記法のキーパス (例: `cwd`, `diff.added`, `set.FOO`)
- `OP`: 演算子 (`=`, `==`, `!=`, `contains`)
- `VALUE`: 期待値リテラル (bool / null / int / string / quoted string)

複数指定の例:

```bash
ctxd git-switch main \
  --expect "branch=main" \
  --expect "dirty=false" \
  --expect "ahead=0" --expect "behind=0"
```

すべての `--expect` が成立すれば `passed=true`、1 つでも成立しなければ `passed=false`。

## 3. 対応する演算子

| 演算子 | 動作 | 適用可能な実値型 | 備考 |
|---|---|---|---|
| `=` / `==` | 厳密一致 | string / bool / int / null | 型不一致は `passed=false` (`actual = "<type mismatch: ...>"`) |
| `!=` | 不一致 | 同上 | 型不一致は `passed=true` にせず、`passed=false` + `actual = "<type mismatch: ...>"`。理由は §3.1 |
| `contains` | 包含 | array (要素一致) / string (substring) | bool / int / null / map には適用不可 (型不一致) |

`=` と `==` は完全な同義。AI エージェントは shell quoting しやすい `=` を主表記とし、`==` は alias とする。

### 3.1 `!=` の型不一致を `passed=false` にする理由

`git_branch != main` を期待したいのに対象が `null` (git repo 外) であるケースを考える。
`null != "main"` を素朴に「真」と扱うと、AI は「ブランチが main 以外」と誤認する。型違いは「判定不能」として明示的に落とすことで、AI に「対象キーの型を再確認せよ」というシグナルを返す。

## 4. 値リテラルの型

DSL の VALUE は次の優先順位で型推論される:

| リテラル | 型 | 例 | 備考 |
|---|---|---|---|
| `"..."` | quoted string | `branch="main"` | `\"`、`\\` の escape をサポート |
| `true`, `false` | bool | `dirty=true` | |
| `null` | null | `git_branch=null` | `*string` の nil / JSON null と一致 |
| `-?[0-9]+` | int | `ahead=0`, `behind=-1` | int64 範囲。`|n| > 2^53` は精度落ちの可能性あり |
| 上記以外 | bare string | `cwd=/foo`, `branch=main` | 末尾 trailing 空白は除去 |

### 4.1 quoted string の意義

`branch="true"` のように quoted で書くと、bool の `true` ではなく **文字列 `true`** として解釈される。実値が string `"true"` のときと bool `true` のときを書き分けるために使う。

Result JSON 上の `Check.Expected` でも quoted の場合は `"true"` (quote 付き) として残し、bare の場合は `true` (quote 無し) として残す。

## 5. ネストキー

ドット区切りで JSON 上のパスを表現する:

```
diff.added
set.FOO
sub.branch
```

### 5.1 制限

- 中間ノードが配列の場合は MVP 非対応 (`listing.0` のような index アクセスは禁止)。`listing contains "src"` で代替する。
- key segment は `[A-Za-z_][A-Za-z0-9_]*`。env 変数名 (`HOME`, `PATH_WITH_UNDERSCORE`) は許容するが、ハイフンを含む key (`FOO-BAR`) は不可。

## 6. MVP コマンドごとの参照可能キー

### 6.1 `chdir`

| キー | 型 | 例 |
|---|---|---|
| `cwd` | string | `/Users/foo/projects/bar` |
| `git_branch` | string \| null | `"main"`、または `null` (git repo 外 / detached HEAD) |
| `listing` | array<string> | `["src","README.md"]` |

例:

```bash
ctxd chdir /tmp/foo --expect "cwd=/tmp/foo"
ctxd chdir /tmp/foo --expect "git_branch=null"
ctxd chdir /repo    --expect "git_branch=main"
ctxd chdir /repo    --expect "listing contains src"
```

### 6.2 `env-set`

| キー | 型 | 例 |
|---|---|---|
| `set.<KEY>` | string | `set.FOO=bar` (KEY は env 変数名) |
| `diff.added` | array<string> | `[FOO,BAZ]` |
| `diff.changed` | array<string> | `[PATH]` |

例:

```bash
ctxd env-set FOO=bar       --expect "set.FOO=bar"
ctxd env-set FOO=bar       --expect "diff.added contains FOO"
ctxd env-set PATH=/usr/bin --expect "diff.changed contains PATH"
```

**脚注**: `-` を含む env 変数名 (`FOO-BAR` 等) は MVP では `set.<KEY>` 形式では参照できない (key segment の文字種制約)。`diff.added contains FOO-BAR` のように VALUE 側に置く形なら判定可能。

### 6.3 `git-switch`

| キー | 型 | 例 |
|---|---|---|
| `branch` | string \| null | `"main"`、または `null` (detached HEAD) |
| `dirty` | bool | `true` / `false` |
| `ahead` | int | `0`, `2` |
| `behind` | int | `0`, `1` |

例:

```bash
ctxd git-switch main --expect "branch=main"
ctxd git-switch main --expect "dirty=false" --expect "ahead=0" --expect "behind=0"
```

## 7. 失敗時の出力例

`ctxd chdir /tmp/foo --expect "cwd=/nope"` が失敗した場合の Result JSON (整形済み):

```json
{
  "ok": false,
  "cmd": "chdir",
  "args": ["/tmp/foo"],
  "result": {
    "cwd": "/tmp/foo",
    "git_branch": null,
    "listing": []
  },
  "error": {
    "code": "postcondition_failed",
    "message": "postcondition failed: 1 of 1 checks did not pass",
    "retryable": false
  },
  "postcondition": {
    "passed": false,
    "checks": [
      {
        "key": "cwd",
        "expected": "/nope",
        "actual": "/tmp/foo",
        "passed": false
      }
    ]
  },
  "elapsed_ms": 3
}
```

ポイント:

- `result` は **保持される** (Data は失敗時も落とさない)。AI は「期待値」と「実観測値」を 1 JSON で対比できる。
- `error.message` は短いサマリ (`N of M checks did not pass`) で済ませ、詳細は `postcondition.checks[]` を唯一のソースとする。

## 8. エラーコードの使い分け

| ケース | `error.code` | 振る舞い |
|---|---|---|
| `--expect` の DSL 自体が parse できない (例: `--expect "garbage"`) | `postcondition_failed` | Verifier 側で `Check{Passed:false, Actual:"<parse error: ...>"}` として埋める。`postcondition.checks` は維持。 |
| `--expect` の DSL は valid だが命題が不成立 | `postcondition_failed` | 上記と同様 (失敗 Check 種別が違うだけ)。 |
| コマンド本体が失敗 (例: `chdir` の対象パスが存在しない) | `not_found` 等のコマンド固有 code | `--expect` は評価されない。 |

`invalid_args` を parse error に使わないのは、AI エージェントが `--expect` の式を試行錯誤するときに「`postcondition.checks` が読めない」状態にしないためである (Q2 暫定 a)。

## 9. シェル quoting の注意

- 演算子と VALUE の間に空白を入れる場合は引数をクォートする:

  ```bash
  ctxd chdir /tmp --expect "cwd=/tmp"           # 空白が無いのでクォート無しでも可
  ctxd chdir /tmp --expect "diff.added contains FOO"
  ```

- `!` を含む式 (`branch!=main`) は bash / zsh の history expansion に注意。
  - 回避策 1: single quote で全体をくくる: `--expect 'branch!=main'`
  - 回避策 2: `set +H` (bash) で history expansion を無効化する
  - これらは shell 側の挙動であり ctxd CLI 側では緩和不可

## 10. 既知の制限

MVP 範囲外の機能を一覧する。FAQ として参照されたい。

| 機能 | 状況 | 備考 |
|---|---|---|
| 配列 index 参照 (`listing.0=src`) | 非対応 | `listing contains src` で代替 |
| ネストオブジェクトの一括比較 (`diff={...}`) | 非対応 | 個別キー比較で代替 |
| boolean 演算子 (OR / NOT) | 非対応 | `--expect` 複数指定で AND のみ |
| 比較演算子 (`>`, `<`, `>=`, `<=`) | 非対応 | 厳密一致 / 不一致 / 包含のみ |
| 正規表現マッチ (`~=` 等) | 非対応 | 将来 `regex` 演算子の追加余地あり |
| ハイフンを含む key segment | 非対応 | quoted form (`set."FOO-BAR"`) は将来検討 |

### 10.1 `contains` の挙動が実値型で変わる点

`contains` は実値が array なら要素一致、string なら substring 含有として動作する。MVP の 3 コマンドが返すキーで `string contains` が実用上意味を持つキーは現状なし (`listing` / `diff.added` / `diff.changed` はすべて array)。AI エージェントは当面、`contains` を「array 要素一致」として理解してよい。将来コマンドが追加されて string キーが導入された場合、その時点で挙動を再確認すること。
