package gitswitch

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

// fakeOK は production 関数の代替で、各 step が常に成功する fake 群を返す。
// テスト本体は必要に応じて個別 field のみ上書きする。
func fakeOK(branch string, dirty bool, ahead, behind int, upstream string) *GitSwitch {
	st := gitops.Status{
		Branch:   branch,
		Upstream: upstream,
		Ahead:    ahead,
		Behind:   behind,
		Dirty:    dirty,
	}
	return &GitSwitch{
		getwd: func() (string, error) { return "/tmp/fake", nil },
		switch_: func(_ context.Context, _, _ string) error {
			return nil
		},
		status: func(_ context.Context, _ string) (gitops.Status, error) {
			return st, nil
		},
		revCount: func(_ context.Context, _, _ string) (int, int, error) {
			return st.Ahead, st.Behind, nil
		},
	}
}

// TC4. invalid args (0 / 2 個) → ErrInvalidArgs。
func TestExecute_InvalidArgs(t *testing.T) {
	t.Parallel()

	g := fakeOK("main", false, 0, 0, "")
	cases := []struct {
		name string
		args []string
	}{
		{"zero args", []string{}},
		{"two args", []string{"main", "feature"}},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			data, errOut := g.Execute(context.Background(), tc.args, runner.Flags{})
			if data != nil {
				t.Errorf("data: got %v, want nil", data)
			}
			if errOut == nil {
				t.Fatalf("expected error, got nil")
			}
			if errOut.Code != output.ErrInvalidArgs {
				t.Errorf("Code: got %q, want %q", errOut.Code, output.ErrInvalidArgs)
			}
		})
	}
}

// TC5. detached HEAD で観測 → fake StatusV2 が Branch="" を返す → JSON で branch: null。
func TestExecute_DetachedHEAD(t *testing.T) {
	t.Parallel()

	g := fakeOK("", false, 0, 0, "")
	data, errOut := g.Execute(context.Background(), []string{"abc1234"}, runner.Flags{})
	if errOut != nil {
		t.Fatalf("Execute: unexpected err: %+v", errOut)
	}
	d, ok := data.(gitSwitchData)
	if !ok {
		t.Fatalf("data type: got %T, want gitSwitchData", data)
	}
	if d.Branch != nil {
		t.Errorf("Branch: got %v, want nil (detached)", *d.Branch)
	}

	// JSON marshal で branch: null になることを確認する。
	raw, err := json.Marshal(d)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if br, ok := m["branch"]; !ok {
		t.Fatalf("branch key missing in JSON: %s", raw)
	} else if br != nil {
		t.Errorf("branch JSON: got %v, want null", br)
	}
}

// TC6. upstream 未設定 → RevCount は呼ばれないこと (call counter で確認)。
func TestExecute_NoUpstream_DoesNotCallRevCount(t *testing.T) {
	t.Parallel()

	called := 0
	g := fakeOK("main", false, 0, 0, "")
	g.revCount = func(_ context.Context, _, _ string) (int, int, error) {
		called++
		return 0, 0, nil
	}

	data, errOut := g.Execute(context.Background(), []string{"main"}, runner.Flags{})
	if errOut != nil {
		t.Fatalf("Execute: unexpected err: %+v", errOut)
	}
	if called != 0 {
		t.Errorf("revCount call count: got %d, want 0 (upstream is empty)", called)
	}
	d := data.(gitSwitchData)
	if d.Ahead != 0 || d.Behind != 0 {
		t.Errorf("Ahead/Behind: got %d/%d, want 0/0", d.Ahead, d.Behind)
	}
}

// TC7. RevCount 失敗時のフォールバック → StatusV2 の ab で Result を返す。
func TestExecute_RevCountFailure_FallsBackToStatusV2(t *testing.T) {
	t.Parallel()

	g := fakeOK("main", false, 3, 2, "origin/main")
	g.revCount = func(_ context.Context, _, _ string) (int, int, error) {
		return 0, 0, errors.New("rev-list boom")
	}

	data, errOut := g.Execute(context.Background(), []string{"main"}, runner.Flags{})
	if errOut != nil {
		t.Fatalf("Execute: unexpected err: %+v", errOut)
	}
	d := data.(gitSwitchData)
	if d.Ahead != 3 || d.Behind != 2 {
		t.Errorf("Ahead/Behind: got %d/%d, want 3/2 (StatusV2 fallback)", d.Ahead, d.Behind)
	}
}

// 追加: switch が「git CLI 不在」相当のエラーを返す → ErrGitNotFound にマップ。
func TestExecute_GitMissing(t *testing.T) {
	t.Parallel()

	g := fakeOK("main", false, 0, 0, "")
	g.switch_ = func(_ context.Context, _, _ string) error {
		return &exec.Error{Name: "git", Err: exec.ErrNotFound}
	}

	_, errOut := g.Execute(context.Background(), []string{"main"}, runner.Flags{})
	if errOut == nil {
		t.Fatalf("expected error, got nil")
	}
	if errOut.Code != output.ErrGitNotFound {
		t.Errorf("Code: got %q, want %q", errOut.Code, output.ErrGitNotFound)
	}
}

// Name() が "git-switch" を返すこと (Registry 登録キー)。
func TestName(t *testing.T) {
	t.Parallel()
	g := New()
	if got := g.Name(); got != "git-switch" {
		t.Errorf("Name: got %q, want %q", got, "git-switch")
	}
}
