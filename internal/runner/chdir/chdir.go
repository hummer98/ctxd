// Package chdir は ctxd の chdir サブコマンドを実装する。
//
// 親シェルの cwd は変更しない (docs/seed.md「parent shell 問題への扱い」)。
// 引数のパスを絶対化し、ディレクトリ内容と git ブランチ (取れれば) を JSON で返すだけに徹する。
package chdir

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/hummer98/ctxd/internal/output"
	"github.com/hummer98/ctxd/internal/runner"
)

// chdirData は chdir コマンドの Result.Data に入るペイロード。
//
// GitBranch は *string 型かつ omitempty を付けない。これにより git_branch キーは
// 必ず JSON に現れ、値が nil のときは null として AI 側で「git repo 外」を区別できる。
// 型は unexported のまま。Result.Data は any で外部に出るため JSON タグだけ揃っていれば
// 型名は外部から参照されず、export して将来 field 追加で API 互換義務を負うのを避ける。
type chdirData struct {
	Cwd       string   `json:"cwd"`
	GitBranch *string  `json:"git_branch"`
	Listing   []string `json:"listing"`
}

// Chdir は runner.Command 実装。gitBranch は DI 用の function field。
type Chdir struct {
	gitBranch func(ctx context.Context, dir string) (string, error)
}

// New は production 用の Chdir を返す。
func New() *Chdir {
	return &Chdir{gitBranch: defaultGitBranch}
}

// Name は runner.Command interface 実装。
func (c *Chdir) Name() string { return "chdir" }

// Execute は runner.Command interface 実装。
func (c *Chdir) Execute(ctx context.Context, args []string, _ runner.Flags) (any, *output.Error) {
	if len(args) != 1 {
		return nil, output.NewError(output.ErrInvalidArgs,
			fmt.Sprintf("chdir requires exactly 1 argument, got %d", len(args)))
	}

	abs, err := filepath.Abs(args[0])
	if err != nil {
		return nil, output.NewError(output.ErrInvalidArgs, err.Error())
	}

	info, err := os.Stat(abs)
	if errors.Is(err, os.ErrNotExist) {
		return nil, output.NewError(output.ErrNotFound,
			fmt.Sprintf("no such file or directory: %s", abs))
	}
	if err != nil {
		return nil, output.NewError(output.ErrExecFailed, err.Error())
	}
	if !info.IsDir() {
		return nil, output.NewError(output.ErrNotADirectory,
			fmt.Sprintf("not a directory: %s", abs))
	}

	entries, err := os.ReadDir(abs)
	if err != nil {
		return nil, output.NewError(output.ErrExecFailed, err.Error())
	}
	listing := make([]string, 0, len(entries))
	for _, e := range entries {
		listing = append(listing, e.Name())
	}

	var branchPtr *string
	if br, err := c.gitBranch(ctx, abs); err == nil && br != "" {
		branchPtr = &br
	}

	return chdirData{Cwd: abs, GitBranch: branchPtr, Listing: listing}, nil
}

// defaultGitBranch は production 用の git ブランチ取得関数。
// `git symbolic-ref --short HEAD` を dir で実行し、失敗時は空文字 + error を返す。
// detached HEAD や git repo 外でも非ゼロ終了になり、呼び出し側で git_branch=null になる。
func defaultGitBranch(ctx context.Context, dir string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", "symbolic-ref", "--short", "HEAD")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}
