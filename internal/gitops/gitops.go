// Package gitops は git CLI を shell-out して構造化情報を取り出すための窓口。
//
// 実装は別タスク（git-switch コマンド実装と同時）。本ファイルは interface と
// プレースホルダ TODO のみ。
package gitops

import "context"

// Repo は git リポジトリの状態を取得する操作群。
//
// TODO(task: git commands): git --porcelain=v2 / -z 出力の parser を実装する。
// 各メソッドは GIT_DIR / cwd を内部で扱い、外部からは不可視にする想定。
type Repo interface {
	// CurrentBranch は HEAD のブランチ名を返す。detached HEAD の場合は ("", nil)。
	CurrentBranch(ctx context.Context) (string, error)
	// Dirty はワーキングツリーに未コミット変更があれば true を返す。
	Dirty(ctx context.Context) (bool, error)
	// AheadBehind は upstream に対する ahead/behind コミット数を返す。
	// upstream 未設定の場合は (0, 0, nil) を返す想定。
	AheadBehind(ctx context.Context) (ahead, behind int, err error)
}
