package postcondition

import (
	"testing"

	"github.com/hummer98/ctxd/internal/output"
)

// TestNormalize は normalize() が json round-trip により map[string]any / []any /
// string / bool / float64 / nil の正規形に落とすことを検証する。
func TestNormalize(t *testing.T) {
	type sub struct {
		Branch *string `json:"branch"`
	}
	type root struct {
		Cwd     string   `json:"cwd"`
		Listing []string `json:"listing"`
		Sub     sub      `json:"sub"`
	}

	br := "main"
	in := root{
		Cwd:     "/foo",
		Listing: []string{"a", "b"},
		Sub:     sub{Branch: &br},
	}
	got := normalize(in)

	m, ok := got.(map[string]any)
	if !ok {
		t.Fatalf("normalize: expected map[string]any, got %T", got)
	}
	if m["cwd"] != "/foo" {
		t.Errorf("cwd: got %v, want /foo", m["cwd"])
	}
	listing, ok := m["listing"].([]any)
	if !ok {
		t.Fatalf("listing: expected []any, got %T", m["listing"])
	}
	if len(listing) != 2 || listing[0] != "a" || listing[1] != "b" {
		t.Errorf("listing: got %v", listing)
	}
	subMap, ok := m["sub"].(map[string]any)
	if !ok {
		t.Fatalf("sub: expected map[string]any, got %T", m["sub"])
	}
	if subMap["branch"] != "main" {
		t.Errorf("sub.branch: got %v, want main", subMap["branch"])
	}
}

// TestNormalize_NilPointer は *string nil が JSON null → Go nil として正規化されることを確認。
func TestNormalize_NilPointer(t *testing.T) {
	type root struct {
		Branch *string `json:"branch"`
	}
	got := normalize(root{Branch: nil})
	m := got.(map[string]any)
	if v, ok := m["branch"]; !ok {
		t.Errorf("branch key missing")
	} else if v != nil {
		t.Errorf("branch: got %v, want nil", v)
	}
}

// TestWalk は walk() が ドット walk と missing 判定を正しく行うことを検証する。
func TestWalk(t *testing.T) {
	root := map[string]any{
		"cwd":   "/foo",
		"set":   map[string]any{"FOO": "bar"},
		"diff":  map[string]any{"added": []any{"FOO"}},
		"empty": nil,
	}

	cases := []struct {
		name       string
		path       []string
		wantValue  any
		wantStatus walkStatus
	}{
		{"top-level string", []string{"cwd"}, "/foo", walkFound},
		{"nested map", []string{"set", "FOO"}, "bar", walkFound},
		{"nested array", []string{"diff", "added"}, []any{"FOO"}, walkFound},
		{"missing top-level", []string{"unknown"}, nil, walkMissing},
		{"missing nested", []string{"set", "BAR"}, nil, walkMissing},
		{"intermediate not map (array)", []string{"diff", "added", "0"}, nil, walkMissing},
		{"value is nil but key exists", []string{"empty"}, nil, walkFound},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, status := walk(root, tc.path)
			if status != tc.wantStatus {
				t.Errorf("status: got %v, want %v", status, tc.wantStatus)
			}
			if !deepEqualLoose(got, tc.wantValue) {
				t.Errorf("value: got %v (%T), want %v (%T)", got, got, tc.wantValue, tc.wantValue)
			}
		})
	}
}

// TestEvalExpression_Eq は Eq 演算子の挙動 (string/bool/int/null/array) を検証。
func TestEvalExpression_Eq(t *testing.T) {
	root := map[string]any{
		"cwd":        "/foo",
		"dirty":      false,
		"ahead":      float64(0),
		"behind":     float64(-1),
		"git_branch": nil,
		"listing":    []any{"a", "b"},
	}

	cases := []struct {
		name       string
		expr       Expression
		wantPassed bool
		wantActual string
	}{
		{
			name:       "string match",
			expr:       Expression{Raw: "cwd=/foo", Key: "cwd", KeyPath: []string{"cwd"}, Op: OpEq, Value: Value{Kind: KindString, Str: "/foo"}},
			wantPassed: true,
			wantActual: "/foo",
		},
		{
			name:       "string mismatch",
			expr:       Expression{Raw: "cwd=/bar", Key: "cwd", KeyPath: []string{"cwd"}, Op: OpEq, Value: Value{Kind: KindString, Str: "/bar"}},
			wantPassed: false,
			wantActual: "/foo",
		},
		{
			name:       "bool match",
			expr:       Expression{Raw: "dirty=false", Key: "dirty", KeyPath: []string{"dirty"}, Op: OpEq, Value: Value{Kind: KindBool, Bool: false}},
			wantPassed: true,
			wantActual: "false",
		},
		{
			name:       "int match (float64 ↔ int)",
			expr:       Expression{Raw: "ahead=0", Key: "ahead", KeyPath: []string{"ahead"}, Op: OpEq, Value: Value{Kind: KindInt, Int: 0}},
			wantPassed: true,
			wantActual: "0",
		},
		{
			name:       "int negative",
			expr:       Expression{Raw: "behind=-1", Key: "behind", KeyPath: []string{"behind"}, Op: OpEq, Value: Value{Kind: KindInt, Int: -1}},
			wantPassed: true,
			wantActual: "-1",
		},
		{
			name:       "null match",
			expr:       Expression{Raw: "git_branch=null", Key: "git_branch", KeyPath: []string{"git_branch"}, Op: OpEq, Value: Value{Kind: KindNull}},
			wantPassed: true,
			wantActual: "null",
		},
		{
			name:       "type mismatch (string DSL vs bool actual)",
			expr:       Expression{Raw: "dirty=yes", Key: "dirty", KeyPath: []string{"dirty"}, Op: OpEq, Value: Value{Kind: KindString, Str: "yes"}},
			wantPassed: false,
			wantActual: "<type mismatch: bool>",
		},
		{
			name:       "missing key",
			expr:       Expression{Raw: "unknown=foo", Key: "unknown", KeyPath: []string{"unknown"}, Op: OpEq, Value: Value{Kind: KindString, Str: "foo"}},
			wantPassed: false,
			wantActual: "<missing>",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := evalExpression(tc.expr, root)
			if got.Passed != tc.wantPassed {
				t.Errorf("Passed: got %v, want %v", got.Passed, tc.wantPassed)
			}
			if got.Actual != tc.wantActual {
				t.Errorf("Actual: got %q, want %q", got.Actual, tc.wantActual)
			}
			if got.Key != tc.expr.Key {
				t.Errorf("Key: got %q, want %q", got.Key, tc.expr.Key)
			}
		})
	}
}

// TestEvalExpression_Ne は Ne 演算子の挙動を検証。
// 型不一致は passed=false で actual="<type mismatch: ...>" になる (plan §1.4)。
func TestEvalExpression_Ne(t *testing.T) {
	root := map[string]any{
		"cwd":        "/foo",
		"git_branch": nil,
	}

	cases := []struct {
		name       string
		expr       Expression
		wantPassed bool
	}{
		{
			name:       "string differs",
			expr:       Expression{Raw: "cwd!=/bar", Key: "cwd", KeyPath: []string{"cwd"}, Op: OpNe, Value: Value{Kind: KindString, Str: "/bar"}},
			wantPassed: true,
		},
		{
			name:       "string equals (passed=false)",
			expr:       Expression{Raw: "cwd!=/foo", Key: "cwd", KeyPath: []string{"cwd"}, Op: OpNe, Value: Value{Kind: KindString, Str: "/foo"}},
			wantPassed: false,
		},
		{
			name: "type mismatch passed=false (string vs null)",
			// `git_branch != main` を期待したいが対象が null。型違いとして passed=false に倒す (plan §1.4)。
			expr:       Expression{Raw: "git_branch!=main", Key: "git_branch", KeyPath: []string{"git_branch"}, Op: OpNe, Value: Value{Kind: KindString, Str: "main"}},
			wantPassed: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := evalExpression(tc.expr, root)
			if got.Passed != tc.wantPassed {
				t.Errorf("Passed: got %v, want %v (Actual=%q)", got.Passed, tc.wantPassed, got.Actual)
			}
		})
	}
}

// TestEvalExpression_Contains は Contains 演算子の挙動を検証。
func TestEvalExpression_Contains(t *testing.T) {
	root := map[string]any{
		"diff": map[string]any{
			"added":   []any{"FOO", "BAR"},
			"changed": []any{},
		},
		"listing": []any{"src", "README.md"},
		"name":    "claude-code",
		"flag":    true,
	}

	cases := []struct {
		name       string
		expr       Expression
		wantPassed bool
		wantActualSubstr string
	}{
		{
			name:       "array contains element",
			expr:       Expression{Raw: "diff.added contains FOO", Key: "diff.added", KeyPath: []string{"diff", "added"}, Op: OpContains, Value: Value{Kind: KindString, Str: "FOO"}},
			wantPassed: true,
		},
		{
			name:       "array does not contain element",
			expr:       Expression{Raw: "diff.added contains BAZ", Key: "diff.added", KeyPath: []string{"diff", "added"}, Op: OpContains, Value: Value{Kind: KindString, Str: "BAZ"}},
			wantPassed: false,
		},
		{
			name:       "empty array",
			expr:       Expression{Raw: "diff.changed contains FOO", Key: "diff.changed", KeyPath: []string{"diff", "changed"}, Op: OpContains, Value: Value{Kind: KindString, Str: "FOO"}},
			wantPassed: false,
		},
		{
			name:       "string contains substring",
			expr:       Expression{Raw: "name contains code", Key: "name", KeyPath: []string{"name"}, Op: OpContains, Value: Value{Kind: KindString, Str: "code"}},
			wantPassed: true,
		},
		{
			name:       "string does not contain",
			expr:       Expression{Raw: "name contains gopher", Key: "name", KeyPath: []string{"name"}, Op: OpContains, Value: Value{Kind: KindString, Str: "gopher"}},
			wantPassed: false,
		},
		{
			name:             "type mismatch (bool)",
			expr:             Expression{Raw: "flag contains FOO", Key: "flag", KeyPath: []string{"flag"}, Op: OpContains, Value: Value{Kind: KindString, Str: "FOO"}},
			wantPassed:       false,
			wantActualSubstr: "<type mismatch:",
		},
		{
			name:       "missing key",
			expr:       Expression{Raw: "unknown contains FOO", Key: "unknown", KeyPath: []string{"unknown"}, Op: OpContains, Value: Value{Kind: KindString, Str: "FOO"}},
			wantPassed: false,
			wantActualSubstr: "<missing>",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := evalExpression(tc.expr, root)
			if got.Passed != tc.wantPassed {
				t.Errorf("Passed: got %v, want %v (Actual=%q)", got.Passed, tc.wantPassed, got.Actual)
			}
			if tc.wantActualSubstr != "" && !contains(got.Actual, tc.wantActualSubstr) {
				t.Errorf("Actual: got %q, want substring %q", got.Actual, tc.wantActualSubstr)
			}
		})
	}
}

// TestEvalExpression_ExpectedFormatting は Check.Expected の文字列化を検証する (plan §4.1)。
// quoted/bare の差を Expected で残すか は impl 段階の選択肢としていたが、本実装では
// quoted は `"true"` のように quote 付きで残す形を採用する。
func TestEvalExpression_ExpectedFormatting(t *testing.T) {
	root := map[string]any{"branch": "true"}

	cases := []struct {
		name         string
		expr         Expression
		wantExpected string
	}{
		{
			name:         "bare string",
			expr:         Expression{Raw: "branch=main", Key: "branch", KeyPath: []string{"branch"}, Op: OpEq, Value: Value{Kind: KindString, Str: "main"}},
			wantExpected: "main",
		},
		{
			name:         "quoted string keeps quotes",
			expr:         Expression{Raw: `branch="true"`, Key: "branch", KeyPath: []string{"branch"}, Op: OpEq, Value: Value{Kind: KindString, Str: "true", Quoted: true}},
			wantExpected: `"true"`,
		},
		{
			name:         "bool literal",
			expr:         Expression{Raw: "dirty=true", Key: "dirty", KeyPath: []string{"dirty"}, Op: OpEq, Value: Value{Kind: KindBool, Bool: true}},
			wantExpected: "true",
		},
		{
			name:         "null literal",
			expr:         Expression{Raw: "git_branch=null", Key: "git_branch", KeyPath: []string{"git_branch"}, Op: OpEq, Value: Value{Kind: KindNull}},
			wantExpected: "null",
		},
		{
			name:         "int literal",
			expr:         Expression{Raw: "ahead=0", Key: "ahead", KeyPath: []string{"ahead"}, Op: OpEq, Value: Value{Kind: KindInt, Int: 0}},
			wantExpected: "0",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := evalExpression(tc.expr, root)
			if got.Expected != tc.wantExpected {
				t.Errorf("Expected: got %q, want %q", got.Expected, tc.wantExpected)
			}
		})
	}
}

// TestEvalExpression_ActualForArrayMap は actual に array/map を入れたとき
// JSON 風文字列 (json.Marshal) になることを確認 (plan §4.1)。
func TestEvalExpression_ActualForArrayMap(t *testing.T) {
	root := map[string]any{
		"diff": map[string]any{
			"added": []any{"FOO", "BAR"},
		},
	}

	expr := Expression{
		Raw:     "diff.added=foo",
		Key:     "diff.added",
		KeyPath: []string{"diff", "added"},
		Op:      OpEq,
		Value:   Value{Kind: KindString, Str: "foo"},
	}
	got := evalExpression(expr, root)
	// 配列実値に対して string Eq → 型不一致で <type mismatch: array>
	if got.Passed {
		t.Errorf("expected Passed=false, got true")
	}
	if !contains(got.Actual, "<type mismatch:") {
		t.Errorf("Actual: got %q, want type mismatch marker", got.Actual)
	}
}

// 確実に output.Check 型を import しているかを compile 時に保証するためのダミー。
var _ output.Check

// contains はテストヘルパ。
func contains(s, sub string) bool {
	return len(sub) == 0 || (len(s) >= len(sub) && indexAny(s, sub) >= 0)
}

func indexAny(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

// deepEqualLoose は walk テスト用の緩い等価判定 ([]any のみ専門に扱う)。
func deepEqualLoose(a, b any) bool {
	switch av := a.(type) {
	case []any:
		bv, ok := b.([]any)
		if !ok {
			return false
		}
		if len(av) != len(bv) {
			return false
		}
		for i := range av {
			if !deepEqualLoose(av[i], bv[i]) {
				return false
			}
		}
		return true
	default:
		return a == b
	}
}
