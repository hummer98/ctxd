# ctxd plugin intervention log

このファイルは [`PLAN.md`](./PLAN.md) で定義した cmux-team cohort study の **cohort 境界の唯一の真のソース (SSOT)** である。
ctxd plugin (`@hummer98/ctxd-claude-plugin`) の install / uninstall / reinstall を append-only で記録する。

## フォーマット規約

- すべてのタイムスタンプは UTC ISO-8601 (`YYYY-MM-DD HH:MM:SSZ`)。
- **append-only**。既存行を編集しない。誤りがあっても上書きせず、新しい行で訂正する (例: 直前の install を取り消す uninstall を追記)。
- 受理する event は `install` / `uninstall` / `reinstall` の 3 種のみ。
- 1 行 = 1 event。列はスペース区切り:
  - 列 1: タイムスタンプ (`YYYY-MM-DD HH:MM:SSZ`)
  - 列 2: event 種別 (`install` / `uninstall` / `reinstall`)
  - 列 3: plugin 識別子 + バージョン (例 `@hummer98/ctxd-claude-plugin@0.2.0`) または理由 (uninstall 時)
- 列 1〜2 の幅は揃え、視覚的に grep しやすくする。

## フォーマット例

```
2026-05-XX HH:MM:SSZ  install    @hummer98/ctxd-claude-plugin@<version>
2026-05-YY HH:MM:SSZ  uninstall  (撤退判定発動 / ユーザー判断)
2026-05-ZZ HH:MM:SSZ  reinstall  @hummer98/ctxd-claude-plugin@<version>
```

## 運用フロー

1. ユーザーが `claude plugins:install @hummer98/ctxd-claude-plugin` を実行する。
2. **install 完了直後に** ユーザーがこのファイルへ 1 行 append する (この規律が崩れると全分析が破綻する。PLAN.md § 10 の最大の脆弱点)。
3. uninstall / reinstall も同様に直後に append する。

---

<!-- log entries below this line -->
