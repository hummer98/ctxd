package chdir

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/hummer98/ctxd/internal/output"
	"github.com/hummer98/ctxd/internal/runner"
)

// gitBranchNotRepo は git repo 外を表す fake。
func gitBranchNotRepo(_ context.Context, _ string) (string, error) {
	return "", errors.New("not a repo")
}

// TC1. 正常系: 既存ディレクトリ + git_branch=null（git repo 外）
func TestChdir_Execute_Success_NoGit(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmp, "a.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatalf("write a.txt: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(tmp, "b"), 0o755); err != nil {
		t.Fatalf("mkdir b: %v", err)
	}

	c := &Chdir{gitBranch: gitBranchNotRepo}
	data, errOut := c.Execute(context.Background(), []string{tmp}, runner.Flags{})
	if errOut != nil {
		t.Fatalf("Execute: unexpected err: %+v", errOut)
	}

	d, ok := data.(chdirData)
	if !ok {
		t.Fatalf("data type: got %T, want chdirData", data)
	}
	if d.Cwd != filepath.Clean(tmp) {
		t.Errorf("Cwd: got %q, want %q", d.Cwd, filepath.Clean(tmp))
	}
	if d.GitBranch != nil {
		t.Errorf("GitBranch: got %v, want nil", *d.GitBranch)
	}
	want := []string{"a.txt", "b"}
	if !reflect.DeepEqual(d.Listing, want) {
		t.Errorf("Listing: got %v, want %v", d.Listing, want)
	}
}

// TC2. 異常系: 存在しないパス
func TestChdir_Execute_NotFound(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	nonexistent := filepath.Join(tmp, "definitely-not-here", "xyz")

	c := &Chdir{gitBranch: gitBranchNotRepo}
	data, errOut := c.Execute(context.Background(), []string{nonexistent}, runner.Flags{})

	if data != nil {
		t.Errorf("data: got %v, want nil", data)
	}
	if errOut == nil {
		t.Fatalf("expected error, got nil")
	}
	if errOut.Code != output.ErrNotFound {
		t.Errorf("Code: got %q, want %q", errOut.Code, output.ErrNotFound)
	}
	if errOut.Retryable {
		t.Errorf("Retryable: got true, want false")
	}
}

// TC3. 異常系: ファイルを指定（ディレクトリでない）
func TestChdir_Execute_NotADirectory(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	filePath := filepath.Join(tmp, "f.txt")
	if err := os.WriteFile(filePath, []byte("x"), 0o644); err != nil {
		t.Fatalf("write f.txt: %v", err)
	}

	c := &Chdir{gitBranch: gitBranchNotRepo}
	_, errOut := c.Execute(context.Background(), []string{filePath}, runner.Flags{})

	if errOut == nil {
		t.Fatalf("expected error, got nil")
	}
	if errOut.Code != output.ErrNotADirectory {
		t.Errorf("Code: got %q, want %q", errOut.Code, output.ErrNotADirectory)
	}
	if errOut.Retryable {
		t.Errorf("Retryable: got true, want false")
	}
}

// TC4. 正常系: git_branch 取得成功（mock）
func TestChdir_Execute_GitBranchSuccess(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	c := &Chdir{
		gitBranch: func(_ context.Context, _ string) (string, error) {
			return "main", nil
		},
	}
	data, errOut := c.Execute(context.Background(), []string{tmp}, runner.Flags{})
	if errOut != nil {
		t.Fatalf("Execute: unexpected err: %+v", errOut)
	}

	d, ok := data.(chdirData)
	if !ok {
		t.Fatalf("data type: got %T, want chdirData", data)
	}
	if d.GitBranch == nil {
		t.Fatalf("GitBranch: got nil, want \"main\"")
	}
	if *d.GitBranch != "main" {
		t.Errorf("GitBranch: got %q, want %q", *d.GitBranch, "main")
	}
}

// TC5. 異常系: 引数の数が不正（0 個 / 2 個）
func TestChdir_Execute_InvalidArgs(t *testing.T) {
	t.Parallel()

	c := &Chdir{gitBranch: gitBranchNotRepo}

	cases := []struct {
		name string
		args []string
	}{
		{"zero args", []string{}},
		{"two args", []string{"/a", "/b"}},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, errOut := c.Execute(context.Background(), tc.args, runner.Flags{})
			if errOut == nil {
				t.Fatalf("expected error, got nil")
			}
			if errOut.Code != output.ErrInvalidArgs {
				t.Errorf("Code: got %q, want %q", errOut.Code, output.ErrInvalidArgs)
			}
		})
	}
}

// TC6. 正常系: 空ディレクトリ → Listing は空配列（nil でない）
func TestChdir_Execute_EmptyDirectory(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	c := &Chdir{gitBranch: gitBranchNotRepo}

	data, errOut := c.Execute(context.Background(), []string{tmp}, runner.Flags{})
	if errOut != nil {
		t.Fatalf("Execute: unexpected err: %+v", errOut)
	}

	d, ok := data.(chdirData)
	if !ok {
		t.Fatalf("data type: got %T, want chdirData", data)
	}
	if d.Listing == nil {
		t.Fatalf("Listing: got nil, want empty slice (JSON []) ")
	}
	if len(d.Listing) != 0 {
		t.Errorf("Listing: got len=%d, want 0", len(d.Listing))
	}

	// JSON marshal で "listing":[] になることを念のため確認する。
	raw, err := json.Marshal(d)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	listing, ok := m["listing"]
	if !ok {
		t.Fatalf("listing key missing in JSON: %s", raw)
	}
	if listing == nil {
		t.Errorf("listing JSON: got null, want []")
	}
	if _, ok := listing.([]any); !ok {
		t.Errorf("listing JSON: got %T, want []any", listing)
	}
	// git_branch は null でキー自体は存在することを確認 (omitempty を付けない方針の保証)。
	gb, ok := m["git_branch"]
	if !ok {
		t.Fatalf("git_branch key missing in JSON: %s", raw)
	}
	if gb != nil {
		t.Errorf("git_branch JSON: got %v, want null", gb)
	}
}
