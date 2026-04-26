package envset

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/hummer98/ctxd/internal/output"
	"github.com/hummer98/ctxd/internal/runner"
)

// TC-parse: parseEnvArg の表駆動テスト (plan §3.1 / §4 TC table)。
func TestParseEnvArg(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		in      string
		wantKey string
		wantVal string
		wantOK  bool
	}{
		{"simple", "FOO=bar", "FOO", "bar", true},
		{"empty value is valid", "FOO=", "FOO", "", true},
		{"missing equals", "FOO", "", "", false},
		{"empty key", "=bar", "", "", false},
		{"empty string", "", "", "", false},
		{"value contains equals", "URL=http://x?a=b", "URL", "http://x?a=b", true},
		{"double equals at start of value", "FOO==bar", "FOO", "=bar", true},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			gotKey, gotVal, gotOK := parseEnvArg(tc.in)
			if gotKey != tc.wantKey || gotVal != tc.wantVal || gotOK != tc.wantOK {
				t.Errorf("parseEnvArg(%q) = (%q, %q, %v), want (%q, %q, %v)",
					tc.in, gotKey, gotVal, gotOK, tc.wantKey, tc.wantVal, tc.wantOK)
			}
		})
	}
}

// fakeEnv は environ / setenv を制御可能にする helper (plan §4.3)。
type fakeEnv struct {
	initial []string
	calls   []envCall
	failOn  string
}

type envCall struct {
	k, v string
}

func (f *fakeEnv) environ() []string { return f.initial }

func (f *fakeEnv) setenv(k, v string) error {
	f.calls = append(f.calls, envCall{k, v})
	if f.failOn != "" && k == f.failOn {
		return errors.New("simulated setenv failure")
	}
	return nil
}

func newEnvSet(initial []string) (*EnvSet, *fakeEnv) {
	fe := &fakeEnv{initial: initial}
	return &EnvSet{environ: fe.environ, setenv: fe.setenv}, fe
}

// TC1. 単一 KEY=val、snapshot に無し → added。
func TestApplySet_SingleAdded(t *testing.T) {
	t.Parallel()

	fe := &fakeEnv{initial: []string{}}
	data, errOut := applySet(fe.environ(), []string{"FOO=bar"}, fe.setenv)
	if errOut != nil {
		t.Fatalf("applySet: unexpected err: %+v", errOut)
	}

	wantSet := map[string]string{"FOO": "bar"}
	if !reflect.DeepEqual(data.Set, wantSet) {
		t.Errorf("Set: got %v, want %v", data.Set, wantSet)
	}
	if !reflect.DeepEqual(data.Diff.Added, []string{"FOO"}) {
		t.Errorf("Added: got %v, want [FOO]", data.Diff.Added)
	}
	if len(data.Diff.Changed) != 0 {
		t.Errorf("Changed: got %v, want []", data.Diff.Changed)
	}
	if len(fe.calls) != 1 || fe.calls[0] != (envCall{"FOO", "bar"}) {
		t.Errorf("setenv calls: got %v, want [(FOO,bar)]", fe.calls)
	}
}

// TC2. 複数 KEY、added と changed が分かれる。
func TestApplySet_AddedAndChanged(t *testing.T) {
	t.Parallel()

	fe := &fakeEnv{initial: []string{"FOO=old", "PATH=/usr/bin"}}
	data, errOut := applySet(fe.environ(), []string{"FOO=new", "BAZ=qux"}, fe.setenv)
	if errOut != nil {
		t.Fatalf("applySet: unexpected err: %+v", errOut)
	}

	wantSet := map[string]string{"FOO": "new", "BAZ": "qux"}
	if !reflect.DeepEqual(data.Set, wantSet) {
		t.Errorf("Set: got %v, want %v", data.Set, wantSet)
	}
	if !reflect.DeepEqual(data.Diff.Added, []string{"BAZ"}) {
		t.Errorf("Added: got %v, want [BAZ]", data.Diff.Added)
	}
	if !reflect.DeepEqual(data.Diff.Changed, []string{"FOO"}) {
		t.Errorf("Changed: got %v, want [FOO]", data.Diff.Changed)
	}
	wantCalls := []envCall{{"FOO", "new"}, {"BAZ", "qux"}}
	if !reflect.DeepEqual(fe.calls, wantCalls) {
		t.Errorf("setenv calls: got %v, want %v", fe.calls, wantCalls)
	}
}

// TC3. 値が同じ → どちらにも入らない。
func TestApplySet_SameValue_NoDiff(t *testing.T) {
	t.Parallel()

	fe := &fakeEnv{initial: []string{"FOO=bar"}}
	data, errOut := applySet(fe.environ(), []string{"FOO=bar"}, fe.setenv)
	if errOut != nil {
		t.Fatalf("applySet: unexpected err: %+v", errOut)
	}

	if !reflect.DeepEqual(data.Set, map[string]string{"FOO": "bar"}) {
		t.Errorf("Set: got %v, want {FOO:bar}", data.Set)
	}
	if len(data.Diff.Added) != 0 {
		t.Errorf("Added: got %v, want []", data.Diff.Added)
	}
	if len(data.Diff.Changed) != 0 {
		t.Errorf("Changed: got %v, want []", data.Diff.Changed)
	}
}

// TC4. 値が空文字 (KEY=) は valid。
func TestApplySet_EmptyValue(t *testing.T) {
	t.Parallel()

	fe := &fakeEnv{initial: []string{}}
	data, errOut := applySet(fe.environ(), []string{"EMPTY="}, fe.setenv)
	if errOut != nil {
		t.Fatalf("applySet: unexpected err: %+v", errOut)
	}

	if got, ok := data.Set["EMPTY"]; !ok || got != "" {
		t.Errorf("Set[EMPTY]: got (%q, %v), want (\"\", true)", got, ok)
	}
	if !reflect.DeepEqual(data.Diff.Added, []string{"EMPTY"}) {
		t.Errorf("Added: got %v, want [EMPTY]", data.Diff.Added)
	}
}

// TC5. 同一 KEY 重複 → 後勝ち、diff には 1 件。
func TestApplySet_DuplicateKey_LastWins(t *testing.T) {
	t.Parallel()

	fe := &fakeEnv{initial: []string{}}
	data, errOut := applySet(fe.environ(), []string{"FOO=a", "FOO=b"}, fe.setenv)
	if errOut != nil {
		t.Fatalf("applySet: unexpected err: %+v", errOut)
	}

	if !reflect.DeepEqual(data.Set, map[string]string{"FOO": "b"}) {
		t.Errorf("Set: got %v, want {FOO:b}", data.Set)
	}
	if !reflect.DeepEqual(data.Diff.Added, []string{"FOO"}) {
		t.Errorf("Added: got %v, want [FOO]", data.Diff.Added)
	}
	if len(data.Diff.Changed) != 0 {
		t.Errorf("Changed: got %v, want []", data.Diff.Changed)
	}
	// setenv は 2 回呼ばれる (a → b)。
	if len(fe.calls) != 2 {
		t.Errorf("setenv call count: got %d, want 2", len(fe.calls))
	}
}

// TC6. 値に '=' を含む → 最初の '=' で split。
func TestApplySet_ValueContainsEquals(t *testing.T) {
	t.Parallel()

	fe := &fakeEnv{initial: []string{}}
	args := []string{"URL=http://x?a=b&c=d"}
	data, errOut := applySet(fe.environ(), args, fe.setenv)
	if errOut != nil {
		t.Fatalf("applySet: unexpected err: %+v", errOut)
	}
	if got := data.Set["URL"]; got != "http://x?a=b&c=d" {
		t.Errorf("Set[URL]: got %q, want %q", got, "http://x?a=b&c=d")
	}
	if !reflect.DeepEqual(data.Diff.Added, []string{"URL"}) {
		t.Errorf("Added: got %v, want [URL]", data.Diff.Added)
	}
}

// TC7. '=' なし → ErrInvalidArgs。
func TestApplySet_MissingEquals(t *testing.T) {
	t.Parallel()

	fe := &fakeEnv{initial: []string{}}
	data, errOut := applySet(fe.environ(), []string{"FOO"}, fe.setenv)
	if errOut == nil {
		t.Fatalf("expected error, got nil (data=%+v)", data)
	}
	if errOut.Code != output.ErrInvalidArgs {
		t.Errorf("Code: got %q, want %q", errOut.Code, output.ErrInvalidArgs)
	}
	if len(fe.calls) != 0 {
		t.Errorf("setenv called %d times, want 0 (abort before any setenv)", len(fe.calls))
	}
}

// TC8. KEY が空 (=val) → ErrInvalidArgs。
func TestApplySet_EmptyKey(t *testing.T) {
	t.Parallel()

	fe := &fakeEnv{initial: []string{}}
	_, errOut := applySet(fe.environ(), []string{"=bar"}, fe.setenv)
	if errOut == nil {
		t.Fatalf("expected error, got nil")
	}
	if errOut.Code != output.ErrInvalidArgs {
		t.Errorf("Code: got %q, want %q", errOut.Code, output.ErrInvalidArgs)
	}
}

// TC9. 引数 0 個 → Execute が ErrInvalidArgs を返す。
func TestExecute_ZeroArgs(t *testing.T) {
	t.Parallel()

	e, _ := newEnvSet(nil)
	data, errOut := e.Execute(context.Background(), []string{}, runner.Flags{})
	if data != nil {
		t.Errorf("data: got %v, want nil", data)
	}
	if errOut == nil {
		t.Fatalf("expected error, got nil")
	}
	if errOut.Code != output.ErrInvalidArgs {
		t.Errorf("Code: got %q, want %q", errOut.Code, output.ErrInvalidArgs)
	}
	if !strings.Contains(errOut.Message, "at least 1 argument") {
		t.Errorf("Message: got %q, want substring 'at least 1 argument'", errOut.Message)
	}
}

// TC10. 空配列が null にならない (chdir TestChdir_Execute_EmptyDirectory と同流儀)。
// added / changed が空のとき JSON で [] になることを確認する。
func TestApplySet_JSON_EmptyArraysNotNull(t *testing.T) {
	t.Parallel()

	fe := &fakeEnv{initial: []string{"FOO=bar"}}
	// FOO=bar は snapshot と一致 → added/changed どちらも空。
	data, errOut := applySet(fe.environ(), []string{"FOO=bar"}, fe.setenv)
	if errOut != nil {
		t.Fatalf("applySet: unexpected err: %+v", errOut)
	}

	raw, err := json.Marshal(data)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}

	set, ok := m["set"]
	if !ok {
		t.Fatalf("set key missing in JSON: %s", raw)
	}
	if _, ok := set.(map[string]any); !ok {
		t.Errorf("set JSON: got %T (%v), want map[string]any (not null)", set, set)
	}

	diff, ok := m["diff"].(map[string]any)
	if !ok {
		t.Fatalf("diff key missing or wrong type in JSON: %s", raw)
	}
	added, ok := diff["added"]
	if !ok {
		t.Fatalf("diff.added key missing in JSON: %s", raw)
	}
	if added == nil {
		t.Errorf("diff.added: got null, want []")
	}
	if _, ok := added.([]any); !ok {
		t.Errorf("diff.added JSON: got %T (%v), want []any", added, added)
	}
	changed, ok := diff["changed"]
	if !ok {
		t.Fatalf("diff.changed key missing in JSON: %s", raw)
	}
	if changed == nil {
		t.Errorf("diff.changed: got null, want []")
	}
	if _, ok := changed.([]any); !ok {
		t.Errorf("diff.changed JSON: got %T (%v), want []any", changed, changed)
	}
}

// TC11. setenv 失敗 → ErrExecFailed。GOOD は通る、BAD で fail。
func TestApplySet_SetenvFailure(t *testing.T) {
	t.Parallel()

	fe := &fakeEnv{initial: []string{}, failOn: "BAD"}
	_, errOut := applySet(fe.environ(), []string{"GOOD=1", "BAD=2"}, fe.setenv)
	if errOut == nil {
		t.Fatalf("expected error, got nil")
	}
	if errOut.Code != output.ErrExecFailed {
		t.Errorf("Code: got %q, want %q", errOut.Code, output.ErrExecFailed)
	}
	if len(fe.calls) != 2 {
		t.Errorf("setenv call count: got %d, want 2 (GOOD then BAD)", len(fe.calls))
	}
}

// TC12. Name() が "env-set" を返す (Registry 登録キー保証)。
func TestName(t *testing.T) {
	t.Parallel()
	e := New()
	if got := e.Name(); got != "env-set" {
		t.Errorf("Name: got %q, want %q", got, "env-set")
	}
}

// 追加: Execute の正常系 e2e (fake DI 経由)。
func TestExecute_HappyPath(t *testing.T) {
	t.Parallel()

	e, fe := newEnvSet([]string{"FOO=old"})
	data, errOut := e.Execute(context.Background(), []string{"FOO=new", "BAZ=qux"}, runner.Flags{})
	if errOut != nil {
		t.Fatalf("Execute: unexpected err: %+v", errOut)
	}
	d, ok := data.(envSetData)
	if !ok {
		t.Fatalf("data type: got %T, want envSetData", data)
	}
	if !reflect.DeepEqual(d.Set, map[string]string{"FOO": "new", "BAZ": "qux"}) {
		t.Errorf("Set: got %v", d.Set)
	}
	if !reflect.DeepEqual(d.Diff.Added, []string{"BAZ"}) {
		t.Errorf("Added: got %v, want [BAZ]", d.Diff.Added)
	}
	if !reflect.DeepEqual(d.Diff.Changed, []string{"FOO"}) {
		t.Errorf("Changed: got %v, want [FOO]", d.Diff.Changed)
	}
	if len(fe.calls) != 2 {
		t.Errorf("setenv call count: got %d, want 2", len(fe.calls))
	}
}

