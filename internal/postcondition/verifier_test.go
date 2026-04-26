package postcondition

import (
	"strings"
	"testing"
)

// TestDefault_Verify_AllPass は全 expect が通るケースで Postcondition.Passed=true を確認。
func TestDefault_Verify_AllPass(t *testing.T) {
	type chdirData struct {
		Cwd       string   `json:"cwd"`
		GitBranch *string  `json:"git_branch"`
		Listing   []string `json:"listing"`
	}

	br := "main"
	data := chdirData{
		Cwd:       "/foo",
		GitBranch: &br,
		Listing:   []string{"src", "README.md"},
	}

	pc := Default{}.Verify([]string{
		"cwd=/foo",
		"git_branch=main",
		"listing contains src",
	}, data)

	if pc == nil {
		t.Fatalf("Verify returned nil Postcondition")
	}
	if !pc.Passed {
		t.Errorf("Passed: got false, want true; checks=%+v", pc.Checks)
	}
	if got := len(pc.Checks); got != 3 {
		t.Errorf("len(Checks): got %d, want 3", got)
	}
	for i, c := range pc.Checks {
		if !c.Passed {
			t.Errorf("Checks[%d] not passed: %+v", i, c)
		}
	}
}

// TestDefault_Verify_OneFail は 1 件 fail の混在で全体 Passed=false を確認。
func TestDefault_Verify_OneFail(t *testing.T) {
	type chdirData struct {
		Cwd string `json:"cwd"`
	}

	pc := Default{}.Verify([]string{
		"cwd=/foo",
		"cwd=/bar",
	}, chdirData{Cwd: "/foo"})

	if pc.Passed {
		t.Errorf("Passed: got true, want false")
	}
	if len(pc.Checks) != 2 {
		t.Fatalf("len(Checks): got %d, want 2", len(pc.Checks))
	}
	if !pc.Checks[0].Passed {
		t.Errorf("Checks[0] should pass: %+v", pc.Checks[0])
	}
	if pc.Checks[1].Passed {
		t.Errorf("Checks[1] should fail: %+v", pc.Checks[1])
	}
}

// TestDefault_Verify_ParseError は parse error が Check に "<parse error: ...>" として
// 詰められ、全体 Passed=false になることを確認 (plan §3.2 / §3.5)。
func TestDefault_Verify_ParseError(t *testing.T) {
	type d struct {
		Cwd string `json:"cwd"`
	}

	pc := Default{}.Verify([]string{
		"cwd=/foo",
		"garbage_no_operator",
	}, d{Cwd: "/foo"})

	if pc.Passed {
		t.Errorf("Passed: got true, want false")
	}
	if len(pc.Checks) != 2 {
		t.Fatalf("len(Checks): got %d, want 2", len(pc.Checks))
	}
	bad := pc.Checks[1]
	if bad.Passed {
		t.Errorf("parse-error Check should not pass: %+v", bad)
	}
	if !strings.HasPrefix(bad.Actual, "<parse error:") {
		t.Errorf("Actual should start with <parse error:, got %q", bad.Actual)
	}
	if bad.Key != "garbage_no_operator" {
		t.Errorf("Key for parse-error: got %q, want raw input", bad.Key)
	}
	if bad.Expected != "garbage_no_operator" {
		t.Errorf("Expected for parse-error: got %q, want raw input", bad.Expected)
	}
}

// TestDefault_Verify_EmptyExpects は expects=[] のとき Checks=[]、Passed=true (defensive)。
func TestDefault_Verify_EmptyExpects(t *testing.T) {
	pc := Default{}.Verify(nil, struct{}{})
	if pc == nil {
		t.Fatalf("Verify returned nil")
	}
	if !pc.Passed {
		t.Errorf("Passed: got false, want true (no checks → vacuously true)")
	}
	if len(pc.Checks) != 0 {
		t.Errorf("len(Checks): got %d, want 0", len(pc.Checks))
	}
}

// TestDefault_Verify_NestedMapKey は env-set の set.<KEY> が解決できるかを確認 (plan §6.2)。
func TestDefault_Verify_NestedMapKey(t *testing.T) {
	type envSetData struct {
		Set map[string]string `json:"set"`
	}

	pc := Default{}.Verify([]string{
		"set.FOO=bar",
	}, envSetData{Set: map[string]string{"FOO": "bar"}})

	if !pc.Passed {
		t.Errorf("Passed: got false; checks=%+v", pc.Checks)
	}
}

// TestDefault_Verify_NullBranch は chdir で git_branch=null (git repo 外) を assertion できることを確認。
func TestDefault_Verify_NullBranch(t *testing.T) {
	type chdirData struct {
		GitBranch *string `json:"git_branch"`
	}

	pc := Default{}.Verify([]string{"git_branch=null"}, chdirData{GitBranch: nil})
	if !pc.Passed {
		t.Errorf("Passed: got false; checks=%+v", pc.Checks)
	}
}
