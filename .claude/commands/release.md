---
description: ctxd の version bump + CHANGELOG 更新 + commit + tag + push を一括実行
argument-hint: <patch|minor|major> [--note "<changelog entry>"]
allowed-tools: Bash, Read, Edit, Write
---

ctxd の release を切る。引数 `$1` は semver bump の種類 (`patch` / `minor` / `major`)。`--note "<text>"` で CHANGELOG entry の本文を指定可能 (省略時は git log から draft 生成)。

このコマンドは **手元の bump 作業のみ** を行い、実 publish は tag push 契機で `.github/workflows/release.yml` (OIDC trusted publishing) が CI 上で実行する。手動 publish への fallback は提供しない。

## 実行手順

### 1. 前提チェック

```bash
# git working tree が clean か (untracked / unstaged 変更が無いか)
git status --porcelain | grep -v '^??' && { echo "ERROR: dirty working tree, commit/stash first" >&2; exit 1; } || true

# 現 branch が main か (release は main で切る)
[ "$(git branch --show-current)" = "main" ] || { echo "ERROR: not on main branch" >&2; exit 1; }

# 引数の妥当性
case "$1" in
  patch|minor|major) ;;
  *) echo "ERROR: $1 is not patch|minor|major" >&2; exit 1 ;;
esac
```

### 2. 現在の version 確認 + sync チェック

```bash
CURRENT=$(jq -r .version .claude-plugin/plugin.json)
echo "current version: $CURRENT"
bash scripts/sync-package-version.sh --check || {
  echo "package.json out of sync; running sync..."
  bash scripts/sync-package-version.sh
  # まだ commit はしない (この後 bump して一緒に commit する)
}
```

### 3. next version を計算

```bash
IFS=. read -r MAJ MIN PAT <<< "$CURRENT"
case "$1" in
  patch) NEXT="$MAJ.$MIN.$((PAT+1))" ;;
  minor) NEXT="$MAJ.$((MIN+1)).0" ;;
  major) NEXT="$((MAJ+1)).0.0" ;;
esac
echo "next version: $NEXT"
```

### 4. eval 取り直しガード (CLAUDE.md SKILL バージョンアップ運用ルール参照)

```bash
PREV_TAG=$(git describe --tags --abbrev=0 2>/dev/null || echo "")
if [ -n "$PREV_TAG" ]; then
  # SKILL.md / plugin.json description が直前 tag から変わっているか
  if ! git diff "$PREV_TAG"..HEAD -- skills/ctxd/SKILL.md .claude-plugin/plugin.json | grep -qE '^[+-]'; then
    echo "ok: SKILL.md / plugin.json unchanged since $PREV_TAG; skipping eval check"
  else
    echo "WARN: SKILL.md or plugin.json changed since $PREV_TAG."
    echo "      CLAUDE.md ルールにより、release を切る前に eval を取り直す必要があります。"
    echo "      取り直し済みであれば 'evals/results/index.md' に新 run が追記されているはず。"
    # ここで user に確認 (会話で yes/no を取り、no なら停止)
  fi
fi
```

> **エージェントへ**: `WARN` が出た場合は user に「`bash evals/run.sh` を取り直しましたか?」と質問し、`no` なら停止する。`yes` なら次に進む。

### 5. `.claude-plugin/plugin.json` の version を bump

```bash
jq --arg v "$NEXT" '.version = $v' .claude-plugin/plugin.json > .claude-plugin/plugin.json.tmp \
  && mv .claude-plugin/plugin.json.tmp .claude-plugin/plugin.json
```

### 6. `package.json` を同期

```bash
bash scripts/sync-package-version.sh
```

### 7. `CHANGELOG.md` に新 entry を追加 / 日付確定

優先順:

1. `^## \[$NEXT\] -` がある → 日付プレースホルダ (`YYYY-MM-DD`, `2026-05-XX` 等) を today (`date +%Y-%m-%d`) に置換
2. `^## \[Unreleased\]` がある → `## [$NEXT] - <today>` に書き換え
3. どちらも無い → `# Changelog` block の直後 (最初の `## [` の直前) に新 section を挿入

`--note "<text>"` 引数が渡されていれば `### Added` 配下に bullet として追加。なければ `git log $PREV_TAG..HEAD --oneline` を抽出して draft bullets として書き、エージェント経由で user に編集を促す。

```bash
TODAY=$(date +%Y-%m-%d)
# 実装は Edit ツールで CHANGELOG.md を読んで該当行を書き換える (sed -i は portable でないので避ける)
```

### 8. commit + tag (push は user 確認後)

```bash
git add .claude-plugin/plugin.json package.json CHANGELOG.md
git commit -m "chore: release v$NEXT"
git tag "v$NEXT"

echo "ローカルで commit + tag を作成しました。push 前に必ず以下を確認:"
echo "  git show HEAD"
echo "  git tag --verify v$NEXT 2>/dev/null || git show v$NEXT"
echo ""
echo "問題なければ:"
echo "  git push origin main"
echo "  git push origin v$NEXT"
echo ""
echo "tag push を契機に .github/workflows/release.yml が起動します。"
```

> **エージェントへ**: `git push` は **user に確認を取ってから** 実行する。本 slash command の主目的は手元 bump の自動化であり、push は不可逆な共有状態への変更なので CLAUDE.md "Executing actions with care" に従う。

### 9. CI 起動の案内 / 検証手順

push 完了後に以下を案内:

- GitHub Actions: https://github.com/hummer98/ctxd/actions
- 完了後の検証コマンド:
  - `npm view @hummer98/ctxd-claude-plugin version` → `<NEXT>` が返ること
  - `gh release view v<NEXT>` → release ノートが CHANGELOG entry と一致すること
  - `brew install hummer98/tap/ctxd && ctxd --version` → `<NEXT>` が返ること (homebrew tap 反映後)

## 注意

- 破壊的変更 (例: コマンド削除、JSON envelope schema 変更) を含む release では `$1=major` を user が明示すること
- `eval 取り直し → release` の順序を遵守 (SKILL.md / plugin.json description を変えた場合)
- CI が失敗したら **手動 publish には逃げない**。CI 設定を直して fix-forward の patch を切る方針 (CLAUDE.md "Release 手順" 参照)
- npm publish は 24 時間以内のみ unpublish 可。それ以降は `npm deprecate` で警告のみ (誤 publish の予防のため dry-run チェックは tag push 前に確認)
