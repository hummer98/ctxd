package status

import (
	"context"
	"encoding/json"
	"errors"
	"os/exec"
	"testing"

	"github.com/hummer98/ctxd/internal/gitops"
	"github.com/hummer98/ctxd/internal/output"
	"github.com/hummer98/ctxd/internal/runner"
)

// fakeNotARepoErr は exec.ExitError を模した「not a git repository」エラー。
type fakeNotARepoErr struct{}

func (e *fakeNotARepoErr) Error() string { return "fatal: not a git repository" }

// notARepoExitErr は gitops.IsNotARepo が拾える形式の exec.ExitError を作る。
func notARepoExitErr() error {
	cmd := exec.Command("/bin/sh", "-c", "echo 'fatal: not a git repository' >&2; exit 128")
	out, err := cmd.CombinedOutput()
	if err == nil {
		// 想定外: テスト helper の組み立てミス。fallback で plain error を返す。
		return errors.New("fatal: not a git repository")
	}
	// CombinedOutput は stderr を out に混ぜる。ExitError の Stderr field を埋めるため
	// 直接 cmd.Output() を経由する版に切り替える。
	cmd2 := exec.Command("/bin/sh", "-c", "echo 'fatal: not a git repository' >&2; exit 128")
	_, err2 := cmd2.Output()
	_ = out
	return err2
}

// TC1. 正常系: cwd 取得失敗を擬する getwd で error 経路を確認。
func TestStatus_Execute_GetwdFails(t *testing.T) {
	t.Parallel()

	s := &Status{
		getwd: func() (string, error) { return "", errors.New("getwd failed") },
	}
	_, errOut := s.Execute(context.Background(), nil, runner.Flags{})
	if errOut == nil {
		t.Fatalf("expected error, got nil")
	}
	if errOut.Code != output.ErrExecFailed {
		t.Errorf("Code: got %q, want %q", errOut.Code, output.ErrExecFailed)
	}
}

// TC2. 異常系: 引数が 1 個以上は invalid_args。
func TestStatus_Execute_InvalidArgs(t *testing.T) {
	t.Parallel()

	s := &Status{}
	_, errOut := s.Execute(context.Background(), []string{"unexpected"}, runner.Flags{})
	if errOut == nil {
		t.Fatalf("expected error, got nil")
	}
	if errOut.Code != output.ErrInvalidArgs {
		t.Errorf("Code: got %q, want %q", errOut.Code, output.ErrInvalidArgs)
	}
}

// TC3. 正常系: git repo 外 → IsGitRepo=false、Branch / HeadCommit は null。
func TestStatus_Execute_NotAGitRepo(t *testing.T) {
	t.Parallel()

	s := &Status{
		getwd: func() (string, error) { return "/tmp/no-repo", nil },
		gitStat: func(_ context.Context, _ string) (gitops.Status, error) {
			return gitops.Status{}, notARepoExitErr()
		},
	}
	data, errOut := s.Execute(context.Background(), nil, runner.Flags{})
	if errOut != nil {
		t.Fatalf("Execute: unexpected err: %+v", errOut)
	}

	d, ok := data.(statusData)
	if !ok {
		t.Fatalf("data type: got %T, want statusData", data)
	}
	if d.Cwd != "/tmp/no-repo" {
		t.Errorf("Cwd: got %q, want %q", d.Cwd, "/tmp/no-repo")
	}
	if d.IsGitRepo {
		t.Errorf("IsGitRepo: got true, want false")
	}
	if d.Branch != nil {
		t.Errorf("Branch: got %v, want nil", *d.Branch)
	}
	if d.HeadCommit != nil {
		t.Errorf("HeadCommit: got %v, want nil", *d.HeadCommit)
	}
	if d.Dirty {
		t.Errorf("Dirty: got true, want false (no repo)")
	}
}

// TC4. 正常系: 通常 branch + upstream あり、HEAD SHA 取得成功。
func TestStatus_Execute_OnBranchWithUpstream(t *testing.T) {
	t.Parallel()

	s := &Status{
		getwd: func() (string, error) { return "/repo", nil },
		gitStat: func(_ context.Context, _ string) (gitops.Status, error) {
			return gitops.Status{
				Branch:   "main",
				Upstream: "origin/main",
				Ahead:    2,
				Behind:   0,
				Dirty:    true,
			}, nil
		},
		revCount: func(_ context.Context, _, _ string) (int, int, error) {
			return 3, 1, nil // RevCount が StatusV2 を上書きする
		},
		headSHA: func(_ context.Context, _ string) (string, error) {
			return "abc1234", nil
		},
	}
	data, errOut := s.Execute(context.Background(), nil, runner.Flags{})
	if errOut != nil {
		t.Fatalf("Execute: unexpected err: %+v", errOut)
	}

	d := data.(statusData)
	if !d.IsGitRepo {
		t.Errorf("IsGitRepo: got false, want true")
	}
	if d.Branch == nil || *d.Branch != "main" {
		t.Errorf("Branch: got %v, want main", d.Branch)
	}
	if d.Ahead != 3 || d.Behind != 1 {
		t.Errorf("Ahead/Behind: got %d/%d, want 3/1", d.Ahead, d.Behind)
	}
	if !d.Dirty {
		t.Errorf("Dirty: got false, want true")
	}
	if d.HeadCommit == nil || *d.HeadCommit != "abc1234" {
		t.Errorf("HeadCommit: got %v, want abc1234", d.HeadCommit)
	}
}

// TC5. 正常系: detached HEAD → Branch=nil、IsGitRepo=true。
func TestStatus_Execute_DetachedHEAD(t *testing.T) {
	t.Parallel()

	s := &Status{
		getwd: func() (string, error) { return "/repo", nil },
		gitStat: func(_ context.Context, _ string) (gitops.Status, error) {
			return gitops.Status{Branch: ""}, nil // detached HEAD
		},
		revCount: func(_ context.Context, _, _ string) (int, int, error) { return 0, 0, nil },
		headSHA: func(_ context.Context, _ string) (string, error) {
			return "deadbee", nil
		},
	}
	data, errOut := s.Execute(context.Background(), nil, runner.Flags{})
	if errOut != nil {
		t.Fatalf("Execute: unexpected err: %+v", errOut)
	}

	d := data.(statusData)
	if !d.IsGitRepo {
		t.Errorf("IsGitRepo: got false, want true")
	}
	if d.Branch != nil {
		t.Errorf("Branch: got %v, want nil (detached)", *d.Branch)
	}
	if d.HeadCommit == nil || *d.HeadCommit != "deadbee" {
		t.Errorf("HeadCommit: got %v, want deadbee", d.HeadCommit)
	}
}

// TC6. 正常系: HEAD SHA 取得失敗 → HeadCommit=null、それ以外は正常。
func TestStatus_Execute_HeadSHAFails(t *testing.T) {
	t.Parallel()

	s := &Status{
		getwd: func() (string, error) { return "/repo", nil },
		gitStat: func(_ context.Context, _ string) (gitops.Status, error) {
			return gitops.Status{Branch: "main"}, nil
		},
		revCount: func(_ context.Context, _, _ string) (int, int, error) { return 0, 0, nil },
		headSHA: func(_ context.Context, _ string) (string, error) {
			return "", errors.New("no commits yet")
		},
	}
	data, errOut := s.Execute(context.Background(), nil, runner.Flags{})
	if errOut != nil {
		t.Fatalf("Execute: unexpected err: %+v", errOut)
	}

	d := data.(statusData)
	if d.HeadCommit != nil {
		t.Errorf("HeadCommit: got %v, want nil", *d.HeadCommit)
	}
	if d.Branch == nil || *d.Branch != "main" {
		t.Errorf("Branch: got %v, want main", d.Branch)
	}
}

// TC7. JSON 形状: 必須キーが repo 外でも全部出ていること。
func TestStatus_Execute_JSONShape_NoRepo(t *testing.T) {
	t.Parallel()

	s := &Status{
		getwd: func() (string, error) { return "/tmp/no-repo", nil },
		gitStat: func(_ context.Context, _ string) (gitops.Status, error) {
			return gitops.Status{}, notARepoExitErr()
		},
	}
	data, _ := s.Execute(context.Background(), nil, runner.Flags{})

	raw, err := json.Marshal(data)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	for _, k := range []string{"cwd", "is_git_repo", "branch", "dirty", "ahead", "behind", "head_commit"} {
		if _, ok := m[k]; !ok {
			t.Errorf("key %q missing in JSON: %s", k, raw)
		}
	}
	if m["branch"] != nil {
		t.Errorf("branch JSON: got %v, want null", m["branch"])
	}
	if m["head_commit"] != nil {
		t.Errorf("head_commit JSON: got %v, want null", m["head_commit"])
	}
}
