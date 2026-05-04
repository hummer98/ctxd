// Package status は ctxd の status サブコマンドを実装する。
//
// mutation を起こさずに「現在の cwd / git ブランチ / dirty / ahead / behind / HEAD SHA」を
// 1 つの JSON envelope で返す read-only クエリ。`git status` / `pwd` / `git rev-parse HEAD` を
// 個別に叩いて output を parse する代わりに、構造化データで状態を把握できる。
package status

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

// statusData は status コマンドの Result.Data に入るペイロード。
//
// IsGitRepo で「git repo 外」と「detached HEAD」を曖昧さなく区別する。
// Branch は *string で detached HEAD / repo 外を null として返す。
// HeadCommit も *string で「commit が 1 つも無い repo」「repo 外」を null で表す。
// Dirty / Ahead / Behind はキー必須のため omitempty を付けない (repo 外でも 0 / false で出る)。
type statusData struct {
	Cwd        string  `json:"cwd"`
	IsGitRepo  bool    `json:"is_git_repo"`
	Branch     *string `json:"branch"`
	Dirty      bool    `json:"dirty"`
	Ahead      int     `json:"ahead"`
	Behind     int     `json:"behind"`
	HeadCommit *string `json:"head_commit"`
}

// Status は runner.Command 実装。各 git 操作は function field で DI 可能にする。
type Status struct {
	getwd    func() (string, error)
	gitStat  func(ctx context.Context, dir string) (gitops.Status, error)
	revCount func(ctx context.Context, dir, upstream string) (ahead, behind int, err error)
	headSHA  func(ctx context.Context, dir string) (string, error)
}

// New は production 用の Status を返す。
func New() *Status {
	return &Status{
		getwd:    os.Getwd,
		gitStat:  gitops.StatusV2,
		revCount: gitops.RevCount,
		headSHA:  gitops.HeadSHA,
	}
}

// Name は runner.Command interface 実装。
func (s *Status) Name() string { return "status" }

// Execute は runner.Command interface 実装。
//
// フロー:
//  1. args 検証 (0 個必須)
//  2. cwd 取得
//  3. gitops.StatusV2 を試行。"not a git repo" なら IsGitRepo=false で早期 return
//  4. upstream があれば RevCount で ahead/behind をクロスチェック
//  5. HEAD SHA を取得 (失敗しても致命ではない: HeadCommit=nil で続ける)
func (s *Status) Execute(ctx context.Context, args []string, _ runner.Flags) (any, *output.Error) {
	if len(args) != 0 {
		return nil, output.NewError(output.ErrInvalidArgs,
			fmt.Sprintf("status takes no arguments, got %d", len(args)))
	}

	cwd, err := s.getwd()
	if err != nil {
		return nil, output.NewError(output.ErrExecFailed, err.Error())
	}

	st, err := s.gitStat(ctx, cwd)
	if err != nil {
		// repo 外は致命扱いせず、git 情報を空にして返す。
		if isNotARepo(err) {
			return statusData{
				Cwd:       cwd,
				IsGitRepo: false,
			}, nil
		}
		// git 自体が見つからない場合は明示エラー。
		if gitops.IsGitMissing(err) {
			return nil, output.NewError(output.ErrGitNotFound, "git CLI not found in PATH")
		}
		return nil, output.NewError(output.ErrExecFailed, err.Error())
	}

	ahead, behind := st.Ahead, st.Behind
	if st.Upstream != "" {
		if a, b, err := s.revCount(ctx, cwd, st.Upstream); err == nil {
			ahead, behind = a, b
		}
		// RevCount 失敗は致命ではない (git_switch と同方針)。
	}

	var branchPtr *string
	if st.Branch != "" {
		b := st.Branch
		branchPtr = &b
	}

	var headPtr *string
	if sha, err := s.headSHA(ctx, cwd); err == nil && sha != "" {
		headPtr = &sha
	}
	// HEAD SHA 取得失敗 (commit がまだ無い repo 等) は HeadCommit=null で続行。

	return statusData{
		Cwd:        cwd,
		IsGitRepo:  true,
		Branch:     branchPtr,
		Dirty:      st.Dirty,
		Ahead:      ahead,
		Behind:     behind,
		HeadCommit: headPtr,
	}, nil
}

// isNotARepo は gitops.StatusV2 由来の error が「git repo 外」を示すか判定する。
// gitops.IsNotARepo は (err, stderr) を受けるが、ここでは exec.ExitError から
// stderr を抜く処理を内蔵する (git_switch.classifyGitError と同パターン)。
func isNotARepo(err error) bool {
	stderr := ""
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		stderr = strings.ToLower(string(ee.Stderr))
	}
	return gitops.IsNotARepo(err, stderr)
}
