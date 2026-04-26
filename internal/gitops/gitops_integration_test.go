package gitops

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// requireGit は git CLI が PATH に無ければ skip する安全弁。
// plan.md §7.1: testing.Short() でのスキップは行わない。git が無い CI のみ skip する。
func requireGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skipf("git not in PATH: %v", err)
	}
}

// runGit は fixture セットアップ用の helper。失敗時は t.Fatal する。
// identity が無い CI でも commit 可能なよう -c で user.name / user.email を毎回渡す。
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
	cmd.Env = forcedEnv()
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// newFixtureRepo は plan.md §7.2 の fixture を作る。
//   - main ブランチに commit "init" (README.md)
//   - feature ブランチを main から分岐 (HEAD=main のまま戻す)
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

// Switch + StatusV2: clean な main → feature へ切り替えて branch=feature, dirty=false を観測。
func TestSwitch_AndStatusV2_Clean(t *testing.T) {
	requireGit(t)
	dir := newFixtureRepo(t)

	if err := Switch(context.Background(), dir, "feature"); err != nil {
		t.Fatalf("Switch: unexpected err: %v", err)
	}
	st, err := StatusV2(context.Background(), dir)
	if err != nil {
		t.Fatalf("StatusV2: unexpected err: %v", err)
	}
	if st.Branch != "feature" {
		t.Errorf("Branch: got %q, want %q", st.Branch, "feature")
	}
	if st.Dirty {
		t.Errorf("Dirty: got true, want false on fresh switch")
	}
	if st.Upstream != "" {
		t.Errorf("Upstream: got %q, want empty (no remote configured)", st.Upstream)
	}
}

// Switch: 存在しないブランチ → ExitError + stderr に pathspec / invalid reference。
func TestSwitch_BranchNotFound(t *testing.T) {
	requireGit(t)
	dir := newFixtureRepo(t)

	err := Switch(context.Background(), dir, "definitely-not-a-branch")
	if err == nil {
		t.Fatalf("Switch: expected error, got nil")
	}
	var ee *exec.ExitError
	if !errors.As(err, &ee) {
		t.Fatalf("Switch: expected *exec.ExitError, got %T: %v", err, err)
	}
	stderr := string(ee.Stderr)
	if !IsBranchNotFound(stderr) {
		t.Errorf("IsBranchNotFound: want true for stderr=%q", stderr)
	}
}

// Switch: dirty な working tree で衝突するブランチへ → dirty で失敗。
func TestSwitch_DirtyTree(t *testing.T) {
	requireGit(t)
	dir := newFixtureRepo(t)

	// feature ブランチで README.md を別内容にして commit する。
	runGit(t, dir, "switch", "feature")
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("feature-content\n"), 0o644); err != nil {
		t.Fatalf("write README.md (feature): %v", err)
	}
	runGit(t, dir, "add", "README.md")
	runGit(t, dir, "commit", "-m", "feature change")

	// main に戻り、README.md を未コミット改変する → feature への switch は dirty で失敗するはず。
	runGit(t, dir, "switch", "main")
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("main-uncommitted\n"), 0o644); err != nil {
		t.Fatalf("write README.md (main, uncommitted): %v", err)
	}

	err := Switch(context.Background(), dir, "feature")
	if err == nil {
		t.Fatalf("Switch: expected dirty error, got nil")
	}
	var ee *exec.ExitError
	if !errors.As(err, &ee) {
		t.Fatalf("Switch: expected *exec.ExitError, got %T: %v", err, err)
	}
	stderr := string(ee.Stderr)
	if !IsDirtyTree(stderr) {
		t.Errorf("IsDirtyTree: want true for stderr=%q", stderr)
	}
}

// StatusV2: git repo 外で呼ぶ → ExitError + stderr に "not a git repository"。
func TestStatusV2_NotARepo(t *testing.T) {
	requireGit(t)
	dir := t.TempDir() // git init していない素の tmp dir。

	_, err := StatusV2(context.Background(), dir)
	if err == nil {
		t.Fatalf("StatusV2: expected error, got nil")
	}
	var ee *exec.ExitError
	if !errors.As(err, &ee) {
		t.Fatalf("StatusV2: expected *exec.ExitError, got %T: %v", err, err)
	}
	if !IsNotARepo(err, string(ee.Stderr)) {
		t.Errorf("IsNotARepo: want true for stderr=%q", string(ee.Stderr))
	}
}

// RevCount: upstream が空文字なら (0, 0, nil)。
func TestRevCount_NoUpstream(t *testing.T) {
	requireGit(t)
	dir := newFixtureRepo(t)

	a, b, err := RevCount(context.Background(), dir, "")
	if err != nil {
		t.Fatalf("RevCount: unexpected err: %v", err)
	}
	if a != 0 || b != 0 {
		t.Errorf("RevCount: got (%d, %d), want (0, 0)", a, b)
	}
}
