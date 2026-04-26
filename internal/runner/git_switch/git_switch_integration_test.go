package gitswitch

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/hummer98/ctxd/internal/gitops"
	"github.com/hummer98/ctxd/internal/output"
	"github.com/hummer98/ctxd/internal/runner"
)

// requireGit は git CLI が PATH に無ければ skip する安全弁。
// plan.md §7.1: testing.Short() でのスキップは行わない。git が無い CI のみ skip する。
func requireGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skipf("git not in PATH: %v", err)
	}
}

// runGit は fixture セットアップ用の helper。
func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	all := append([]string{
		"-c", "user.name=test",
		"-c", "user.email=test@example.com",
		"-c", "init.defaultBranch=main",
		"-c", "commit.gpgsign=false",
	}, args...)
	cmd := exec.Command("git", all...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "LANG=C", "LC_ALL=C")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// newFixtureRepo は plan.md §7.2 の fixture を作る。
func newFixtureRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	runGit(t, dir, "init", "-b", "main")
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("hello\n"), 0o644); err != nil {
		t.Fatalf("write README.md: %v", err)
	}
	runGit(t, dir, "add", "README.md")
	runGit(t, dir, "commit", "-m", "init")
	runGit(t, dir, "branch", "feature")
	return dir
}

// withFixtureGetwd は production GitSwitch を返すが、cwd だけ fixture dir に固定する。
// 親シェルの cwd を変更したくないので os.Chdir は使わず getwd を差し替える。
func withFixtureGetwd(dir string) *GitSwitch {
	g := New()
	g.getwd = func() (string, error) { return dir, nil }
	return g
}

// TC1. clean switch 成功:
//
//	fixture HEAD=main → Execute(["feature"]) → branch=feature, dirty=false, ahead=0, behind=0.
func TestIntegration_CleanSwitch(t *testing.T) {
	requireGit(t)
	dir := newFixtureRepo(t)

	g := withFixtureGetwd(dir)
	data, errOut := g.Execute(context.Background(), []string{"feature"}, runner.Flags{})
	if errOut != nil {
		t.Fatalf("Execute: unexpected err: %+v", errOut)
	}
	d, ok := data.(gitSwitchData)
	if !ok {
		t.Fatalf("data type: got %T, want gitSwitchData", data)
	}
	if d.Branch == nil || *d.Branch != "feature" {
		got := "<nil>"
		if d.Branch != nil {
			got = *d.Branch
		}
		t.Errorf("Branch: got %q, want %q", got, "feature")
	}
	if d.Dirty {
		t.Error("Dirty: got true, want false on fresh switch")
	}
	if d.Ahead != 0 || d.Behind != 0 {
		t.Errorf("Ahead/Behind: got %d/%d, want 0/0", d.Ahead, d.Behind)
	}
}

// TC2. dirty 状態で switch 失敗:
//
//	main で README.md を改変 → feature(別内容で commit 済) への switch は dirty で失敗。
func TestIntegration_DirtyTreeBlocksSwitch(t *testing.T) {
	requireGit(t)
	dir := newFixtureRepo(t)

	// feature ブランチで README.md を別内容にして commit する。
	runGit(t, dir, "switch", "feature")
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("feature-content\n"), 0o644); err != nil {
		t.Fatalf("write README.md (feature): %v", err)
	}
	runGit(t, dir, "add", "README.md")
	runGit(t, dir, "commit", "-m", "feature change")

	// main に戻り、README.md を未コミット改変する。
	runGit(t, dir, "switch", "main")
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("main-uncommitted\n"), 0o644); err != nil {
		t.Fatalf("write README.md (main, uncommitted): %v", err)
	}

	g := withFixtureGetwd(dir)
	_, errOut := g.Execute(context.Background(), []string{"feature"}, runner.Flags{})
	if errOut == nil {
		t.Fatalf("expected dirty_tree error, got nil")
	}
	if errOut.Code != output.ErrDirtyTree {
		t.Errorf("Code: got %q, want %q (message=%q)", errOut.Code, output.ErrDirtyTree, errOut.Message)
	}
}

// TC3. branch 不在 → ErrBranchNotFound。
func TestIntegration_BranchNotFound(t *testing.T) {
	requireGit(t)
	dir := newFixtureRepo(t)

	g := withFixtureGetwd(dir)
	_, errOut := g.Execute(context.Background(), []string{"definitely-not-a-branch"}, runner.Flags{})
	if errOut == nil {
		t.Fatalf("expected branch_not_found error, got nil")
	}
	if errOut.Code != output.ErrBranchNotFound {
		t.Errorf("Code: got %q, want %q (message=%q)", errOut.Code, output.ErrBranchNotFound, errOut.Message)
	}
}

// 追加: git repo 外で実行 → ErrNotAGitRepo。
func TestIntegration_NotAGitRepo(t *testing.T) {
	requireGit(t)
	dir := t.TempDir() // git init していない素の tmp dir。

	g := withFixtureGetwd(dir)
	_, errOut := g.Execute(context.Background(), []string{"main"}, runner.Flags{})
	if errOut == nil {
		t.Fatalf("expected not_a_git_repo error, got nil")
	}
	if errOut.Code != output.ErrNotAGitRepo {
		t.Errorf("Code: got %q, want %q (message=%q)", errOut.Code, output.ErrNotAGitRepo, errOut.Message)
	}
}

// 追加: gitops package を直接通せていることの sanity check (プロダクション関数を使う統合テスト)。
func TestIntegration_StatusV2Direct(t *testing.T) {
	requireGit(t)
	dir := newFixtureRepo(t)

	st, err := gitops.StatusV2(context.Background(), dir)
	if err != nil {
		t.Fatalf("StatusV2: unexpected err: %v", err)
	}
	if st.Branch != "main" {
		t.Errorf("Branch: got %q, want %q", st.Branch, "main")
	}
}
