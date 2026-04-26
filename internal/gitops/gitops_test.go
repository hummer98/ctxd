package gitops

import (
	"strings"
	"testing"
)

// nul は -z 区切りの NUL バイトを表す可読ヘルパ。
const nul = "\x00"

// TC8. 標準ケース: branch.head / branch.upstream / branch.ab を正しく取り出す。
func TestParseStatusV2_Standard(t *testing.T) {
	t.Parallel()

	in := []byte("# branch.oid abcdef" + nul +
		"# branch.head main" + nul +
		"# branch.upstream origin/main" + nul +
		"# branch.ab +0 -0" + nul)

	st, err := parseStatusV2(in)
	if err != nil {
		t.Fatalf("parseStatusV2: unexpected err: %v", err)
	}
	if st.Branch != "main" {
		t.Errorf("Branch: got %q, want %q", st.Branch, "main")
	}
	if st.Upstream != "origin/main" {
		t.Errorf("Upstream: got %q, want %q", st.Upstream, "origin/main")
	}
	if st.Ahead != 0 || st.Behind != 0 {
		t.Errorf("Ahead/Behind: got %d/%d, want 0/0", st.Ahead, st.Behind)
	}
	if st.Dirty {
		t.Errorf("Dirty: got true, want false")
	}
}

// TC9. dirty 1 行: '1 ' で始まるエントリで Dirty=true。
func TestParseStatusV2_Dirty(t *testing.T) {
	t.Parallel()

	in := []byte("# branch.head main" + nul +
		"# branch.upstream origin/main" + nul +
		"# branch.ab +1 -2" + nul +
		"1 .M N... 100644 100644 100644 abc def README.md" + nul)

	st, err := parseStatusV2(in)
	if err != nil {
		t.Fatalf("parseStatusV2: unexpected err: %v", err)
	}
	if !st.Dirty {
		t.Errorf("Dirty: got false, want true")
	}
	if st.Ahead != 1 || st.Behind != 2 {
		t.Errorf("Ahead/Behind: got %d/%d, want 1/2 (absolute)", st.Ahead, st.Behind)
	}
}

// TC10. detached HEAD: branch.head が "(detached)" → Branch="".
func TestParseStatusV2_Detached(t *testing.T) {
	t.Parallel()

	in := []byte("# branch.head (detached)" + nul)

	st, err := parseStatusV2(in)
	if err != nil {
		t.Fatalf("parseStatusV2: unexpected err: %v", err)
	}
	if st.Branch != "" {
		t.Errorf("Branch: got %q, want empty (detached)", st.Branch)
	}
}

// TC11. upstream 未設定: branch.upstream / branch.ab 行なし → Upstream="", Ahead=0, Behind=0.
func TestParseStatusV2_NoUpstream(t *testing.T) {
	t.Parallel()

	in := []byte("# branch.oid abc" + nul +
		"# branch.head feature" + nul)

	st, err := parseStatusV2(in)
	if err != nil {
		t.Fatalf("parseStatusV2: unexpected err: %v", err)
	}
	if st.Branch != "feature" {
		t.Errorf("Branch: got %q, want %q", st.Branch, "feature")
	}
	if st.Upstream != "" {
		t.Errorf("Upstream: got %q, want empty", st.Upstream)
	}
	if st.Ahead != 0 || st.Behind != 0 {
		t.Errorf("Ahead/Behind: got %d/%d, want 0/0", st.Ahead, st.Behind)
	}
}

// TC12. rename エントリ ('2 ...'): pathname が NUL 2 個で来るので、続く NUL 区切りの
// orig_path レコードを skip して dirty=true を返す。
func TestParseStatusV2_RenameEntry(t *testing.T) {
	t.Parallel()

	// "2 R.. N... ... new_name" + NUL + "old_name" + NUL  →  rename 1 件。
	in := []byte("# branch.head main" + nul +
		"2 R.. N... 100644 100644 100644 abc def R100 new_name.md" + nul +
		"old_name.md" + nul +
		"? extra.txt" + nul)

	st, err := parseStatusV2(in)
	if err != nil {
		t.Fatalf("parseStatusV2: unexpected err: %v", err)
	}
	if !st.Dirty {
		t.Errorf("Dirty: got false, want true")
	}
	// rename の後続レコード "old_name.md" は entry として誤認されないこと（dirty フラグの
	// 二重カウントはないが、parser が orig_path を別エントリと混同しないことを確認）。
	if st.Branch != "main" {
		t.Errorf("Branch: got %q, want %q (rename should not corrupt parser state)", st.Branch, "main")
	}
}

// 追加: 異常系。branch.ab の数値が壊れていれば error を返す。
func TestParseStatusV2_MalformedAb(t *testing.T) {
	t.Parallel()

	in := []byte("# branch.head main" + nul +
		"# branch.ab +x -1" + nul)

	_, err := parseStatusV2(in)
	if err == nil {
		t.Fatalf("expected parse error for malformed branch.ab")
	}
	if !strings.Contains(err.Error(), "branch.ab") {
		t.Errorf("error message: got %q, want contains 'branch.ab'", err.Error())
	}
}

// IsNotARepo / IsBranchNotFound / IsDirtyTree / IsGitMissing 各 helper の文字列マッチを軽く確認。
func TestErrorClassifiers(t *testing.T) {
	t.Parallel()

	if !IsNotARepo(nil, "fatal: not a git repository (or any of the parent directories): .git") {
		t.Error("IsNotARepo: want true for canonical fatal message")
	}
	if !IsBranchNotFound("fatal: invalid reference: foo") {
		t.Error("IsBranchNotFound: want true for invalid reference")
	}
	if !IsBranchNotFound("error: pathspec 'foo' did not match any file(s) known to git") {
		t.Error("IsBranchNotFound: want true for pathspec did not match")
	}
	if !IsDirtyTree("error: Your local changes to the following files would be overwritten by checkout: README.md") {
		t.Error("IsDirtyTree: want true for would be overwritten")
	}
}
