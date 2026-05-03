---
description: 手元の ctxd source から CLI を再ビルドして ~/go/bin/ctxd に上書き (dogfood 用)
allowed-tools: Bash
---

ctxd repo の HEAD source を `go install ./cmd/ctxd` で `~/go/bin/ctxd` に上書きする。session 内で ctxd skill が呼ぶ `ctxd` binary を最新コードに揃えるためのコマンド。

## いつ使うか

- ctxd source を編集した後、同じ session で ctxd skill (`ctxd chdir` / `ctxd env-set` / `ctxd git-switch`) を踏んで動作確認したいとき
- `ctxd --version` が `0.0.0-dev` のままで、build 時刻が古いと気付いたとき

release 時には呼ばなくて良い (release 後の公式 binary を使うなら `brew upgrade ctxd`)。

## 実行手順

```bash
cd /Users/yamamoto/git/ctxd
go install ./cmd/ctxd
ls -la ~/go/bin/ctxd
~/go/bin/ctxd --version
```

最終行が `ctxd version 0.0.0-dev` を返せば成功 (`-ldflags` での version 上書きは goreleaser build 経由のみなので、手元 build では `0.0.0-dev` のまま — これは仕様)。

build error が出たら user に提示して停止する。
