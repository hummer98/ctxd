package postcondition

import (
	"strings"
	"testing"
)

// TestParse_Success は正常系の table test。
// DSL を 1 expression パースして Expression 構造体に正しく分解できるかを検証する。
func TestParse_Success(t *testing.T) {
	cases := []struct {
		name      string
		input     string
		wantKey   string
		wantPath  []string
		wantOp    Op
		wantValue Value
	}{
		{
			name:      "simple bare string",
			input:     "cwd=/foo",
			wantKey:   "cwd",
			wantPath:  []string{"cwd"},
			wantOp:    OpEq,
			wantValue: Value{Kind: KindString, Str: "/foo"},
		},
		{
			name:      "bool true",
			input:     "dirty=true",
			wantKey:   "dirty",
			wantPath:  []string{"dirty"},
			wantOp:    OpEq,
			wantValue: Value{Kind: KindBool, Bool: true},
		},
		{
			name:      "bool false",
			input:     "dirty=false",
			wantKey:   "dirty",
			wantPath:  []string{"dirty"},
			wantOp:    OpEq,
			wantValue: Value{Kind: KindBool, Bool: false},
		},
		{
			name:      "int zero",
			input:     "ahead=0",
			wantKey:   "ahead",
			wantPath:  []string{"ahead"},
			wantOp:    OpEq,
			wantValue: Value{Kind: KindInt, Int: 0},
		},
		{
			name:      "int negative",
			input:     "behind=-1",
			wantKey:   "behind",
			wantPath:  []string{"behind"},
			wantOp:    OpEq,
			wantValue: Value{Kind: KindInt, Int: -1},
		},
		{
			name:      "null literal",
			input:     "git_branch=null",
			wantKey:   "git_branch",
			wantPath:  []string{"git_branch"},
			wantOp:    OpEq,
			wantValue: Value{Kind: KindNull},
		},
		{
			name:      "quoted string of literal-looking word",
			input:     `branch="true"`,
			wantKey:   "branch",
			wantPath:  []string{"branch"},
			wantOp:    OpEq,
			wantValue: Value{Kind: KindString, Str: "true", Quoted: true},
		},
		{
			name:      "ne with spaces",
			input:     "branch != main",
			wantKey:   "branch",
			wantPath:  []string{"branch"},
			wantOp:    OpNe,
			wantValue: Value{Kind: KindString, Str: "main"},
		},
		{
			name:      "ne without spaces",
			input:     "branch!=main",
			wantKey:   "branch",
			wantPath:  []string{"branch"},
			wantOp:    OpNe,
			wantValue: Value{Kind: KindString, Str: "main"},
		},
		{
			name:      "contains with nested key",
			input:     "diff.added contains FOO",
			wantKey:   "diff.added",
			wantPath:  []string{"diff", "added"},
			wantOp:    OpContains,
			wantValue: Value{Kind: KindString, Str: "FOO"},
		},
		{
			name:      "nested map key",
			input:     "set.FOO=bar",
			wantKey:   "set.FOO",
			wantPath:  []string{"set", "FOO"},
			wantOp:    OpEq,
			wantValue: Value{Kind: KindString, Str: "bar"},
		},
		{
			name:      "double-equals alias with multiple spaces",
			input:     "branch ==  main",
			wantKey:   "branch",
			wantPath:  []string{"branch"},
			wantOp:    OpEq,
			wantValue: Value{Kind: KindString, Str: "main"},
		},
		{
			name:      "value with embedded equals (URL-like)",
			input:     "set.URL=https://example.com/?a=1",
			wantKey:   "set.URL",
			wantPath:  []string{"set", "URL"},
			wantOp:    OpEq,
			wantValue: Value{Kind: KindString, Str: "https://example.com/?a=1"},
		},
		{
			name:      "trailing whitespace stripped from bare string",
			input:     "cwd=/foo   ",
			wantKey:   "cwd",
			wantPath:  []string{"cwd"},
			wantOp:    OpEq,
			wantValue: Value{Kind: KindString, Str: "/foo"},
		},
		{
			name:      "leading whitespace stripped",
			input:     "  cwd=/foo",
			wantKey:   "cwd",
			wantPath:  []string{"cwd"},
			wantOp:    OpEq,
			wantValue: Value{Kind: KindString, Str: "/foo"},
		},
		{
			name:      "underscore env-style key segment",
			input:     "set.PATH_WITH_UNDERSCORE=/usr/bin",
			wantKey:   "set.PATH_WITH_UNDERSCORE",
			wantPath:  []string{"set", "PATH_WITH_UNDERSCORE"},
			wantOp:    OpEq,
			wantValue: Value{Kind: KindString, Str: "/usr/bin"},
		},
		{
			name:      "quoted empty string",
			input:     `branch=""`,
			wantKey:   "branch",
			wantPath:  []string{"branch"},
			wantOp:    OpEq,
			wantValue: Value{Kind: KindString, Str: "", Quoted: true},
		},
		{
			name:      "quoted string with escape",
			input:     `branch="foo\"bar"`,
			wantKey:   "branch",
			wantPath:  []string{"branch"},
			wantOp:    OpEq,
			wantValue: Value{Kind: KindString, Str: `foo"bar`, Quoted: true},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Parse(tc.input)
			if err != nil {
				t.Fatalf("Parse(%q): unexpected error: %v", tc.input, err)
			}
			if got.Key != tc.wantKey {
				t.Errorf("Key: got %q, want %q", got.Key, tc.wantKey)
			}
			if !equalStrings(got.KeyPath, tc.wantPath) {
				t.Errorf("KeyPath: got %v, want %v", got.KeyPath, tc.wantPath)
			}
			if got.Op != tc.wantOp {
				t.Errorf("Op: got %q, want %q", got.Op, tc.wantOp)
			}
			if got.Value != tc.wantValue {
				t.Errorf("Value: got %+v, want %+v", got.Value, tc.wantValue)
			}
			if got.Raw != tc.input {
				t.Errorf("Raw: got %q, want %q", got.Raw, tc.input)
			}
		})
	}
}

// TestParse_Error は parse error が想定どおり error として返ることを検証する。
// メッセージ全体ではなく substring で照合し、文言調整に脆くないテストにする。
func TestParse_Error(t *testing.T) {
	cases := []struct {
		name        string
		input       string
		wantSubstr  string
	}{
		{
			name:       "no operator",
			input:      "cwd /foo",
			wantSubstr: "operator",
		},
		{
			name:       "unsupported operator",
			input:      "ahead > 0",
			wantSubstr: "operator",
		},
		{
			name:       "empty key",
			input:      "=foo",
			wantSubstr: "key",
		},
		{
			name:       "kebab key segment",
			input:      "a-b=foo",
			wantSubstr: "key",
		},
		{
			name:       "unclosed quoted string",
			input:      `branch="abc`,
			wantSubstr: "quote",
		},
		{
			name:       "integer overflow",
			input:      "count=999999999999999999999",
			wantSubstr: "overflow",
		},
		{
			name:       "empty input",
			input:      "",
			wantSubstr: "empty",
		},
		{
			name:       "whitespace-only input",
			input:      "   ",
			wantSubstr: "empty",
		},
		{
			name:       "trailing dot in key",
			input:      "diff.=foo",
			wantSubstr: "key",
		},
		{
			name:       "leading dot in key",
			input:      ".foo=bar",
			wantSubstr: "key",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Parse(tc.input)
			if err == nil {
				t.Fatalf("Parse(%q): expected error, got nil", tc.input)
			}
			if !strings.Contains(err.Error(), tc.wantSubstr) {
				t.Errorf("Parse(%q) error = %q, want substring %q", tc.input, err.Error(), tc.wantSubstr)
			}
		})
	}
}

// equalStrings は []string の等価判定 (テストヘルパ)。
func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
