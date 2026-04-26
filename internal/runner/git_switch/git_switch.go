// Package gitswitch は ctxd の git-switch サブコマンドを実装する。
//
// `git switch <branch>` を shell-out し、切り替え後の working tree 状態
// (branch / dirty / ahead / behind) を構造化 JSON で返すだけに徹する。
// 親シェルの cwd は変更しない (docs/seed.md「parent shell 問題への扱い」)。
//
// ディレクトリ名は CLI 名と整合する `git_switch` だが、Go の package 名は
// snake_case 不可のため `gitswitch` とする (plan.md §6.5)。
package gitswitch

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/hummer98/ctxd/internal/gitops"
	"github.com/hummer98/ctxd/internal/output"
	"github.com/hummer98/ctxd/internal/runner"
)

// gitSwitchData は git-switch コマンドの Result.Data に入るペイロード。
//
// Branch は *string にして「detached HEAD」を null で返す。chdir.GitBranch と同方針。
// Dirty / Ahead / Behind はキー必須のため omitempty を付けない。
type gitSwitchData struct {
	Branch *string `json:"branch"`
	Dirty  bool    `json:"dirty"`
	Ahead  int     `json:"ahead"`
	Behind int     `json:"behind"`
}

// GitSwitch は runner.Command 実装。各 git 操作は function field で DI 可能にする。
type GitSwitch struct {
	getwd    func() (string, error)
	switch_  func(ctx context.Context, dir, branch string) error
	status   func(ctx context.Context, dir string) (gitops.Status, error)
	revCount func(ctx context.Context, dir, upstream string) (ahead, behind int, err error)
}

// New は production 用の GitSwitch を返す。
func New() *GitSwitch {
	return &GitSwitch{
		getwd:    os.Getwd,
		switch_:  gitops.Switch,
		status:   gitops.StatusV2,
		revCount: gitops.RevCount,
	}
}

// Name は runner.Command interface 実装。
func (g *GitSwitch) Name() string { return "git-switch" }

// Execute は runner.Command interface 実装。
//
// フロー (plan.md §6.3):
//  1. args 検証 (1 個必須)
//  2. cwd 取得
//  3. git switch
//  4. git status --porcelain=v2 -b -z で切り替え後の状態観測
//  5. upstream があれば git rev-list --count --left-right でクロスチェック
//  6. ペイロード組み立て
func (g *GitSwitch) Execute(ctx context.Context, args []string, _ runner.Flags) (any, *output.Error) {
	if len(args) != 1 {
		return nil, output.NewError(output.ErrInvalidArgs,
			fmt.Sprintf("git-switch requires exactly 1 argument, got %d", len(args)))
	}
	branch := args[0]

	dir, err := g.getwd()
	if err != nil {
		return nil, output.NewError(output.ErrExecFailed, err.Error())
	}

	if err := g.switch_(ctx, dir, branch); err != nil {
		return nil, classifyGitError(err)
	}

	st, err := g.status(ctx, dir)
	if err != nil {
		return nil, classifyGitError(err)
	}

	ahead, behind := st.Ahead, st.Behind
	if st.Upstream != "" {
		a, b, err := g.revCount(ctx, dir, st.Upstream)
		if err == nil {
			// RevCount が成功した場合は最終値とする (porcelain v2 差異への保険)。
			ahead, behind = a, b
		}
		// RevCount の失敗は致命ではない: StatusV2 の値で代替する (plan.md §4.4)。
	}

	var branchPtr *string
	if st.Branch != "" {
		b := st.Branch
		branchPtr = &b
	}

	return gitSwitchData{
		Branch: branchPtr,
		Dirty:  st.Dirty,
		Ahead:  ahead,
		Behind: behind,
	}, nil
}

// classifyGitError は git CLI 由来の error を ErrorCode にマップする。
// 判定優先順位は plan.md §3 の通り: git_not_found → not_a_git_repo → branch_not_found → dirty_tree → exec_failed。
func classifyGitError(err error) *output.Error {
	if gitops.IsGitMissing(err) {
		return output.NewError(output.ErrGitNotFound, "git CLI not found in PATH")
	}
	stderr := ""
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		stderr = string(ee.Stderr)
	}
	switch {
	case gitops.IsNotARepo(err, stderr):
		return output.NewError(output.ErrNotAGitRepo, trimStderr(stderr))
	case gitops.IsBranchNotFound(stderr):
		return output.NewError(output.ErrBranchNotFound, trimStderr(stderr))
	case gitops.IsDirtyTree(stderr):
		return output.NewError(output.ErrDirtyTree, trimStderr(stderr))
	default:
		msg := trimStderr(stderr)
		if msg == "" {
			msg = err.Error()
		}
		return output.NewError(output.ErrExecFailed, msg)
	}
}

// trimStderr は stderr 末尾の改行を削り、長すぎる場合は先頭 1 行のみを残す
// (JSON 肥大化防止)。
func trimStderr(stderr string) string {
	s := strings.TrimRight(stderr, "\n")
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return s
}
