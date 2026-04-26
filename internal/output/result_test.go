package output

import (
	"encoding/json"
	"testing"
)

// TestResult_Success_JSON は成功時の Result が想定どおりの JSON 形状になることを検証する。
// snake_case キー存在の検証は map[string]any への unmarshal 経由で行う。
func TestResult_Success_JSON(t *testing.T) {
	r := Result{
		OK:        true,
		Cmd:       "chdir",
		Args:      []string{"/foo"},
		Data:      map[string]any{"cwd": "/foo"},
		ElapsedMs: 3,
	}

	raw, err := MarshalJSON(r)
	if err != nil {
		t.Fatalf("MarshalJSON: %v", err)
	}

	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	// 常に出るキー
	for _, key := range []string{"ok", "cmd", "args", "result", "elapsed_ms"} {
		if _, ok := m[key]; !ok {
			t.Errorf("expected key %q in JSON output, got %v", key, m)
		}
	}
	// 出てはいけないキー
	for _, key := range []string{"error", "postcondition"} {
		if _, ok := m[key]; ok {
			t.Errorf("unexpected key %q in JSON output, got %v", key, m)
		}
	}

	if got := m["ok"]; got != true {
		t.Errorf("ok: got %v, want true", got)
	}
	if got := m["cmd"]; got != "chdir" {
		t.Errorf("cmd: got %v, want chdir", got)
	}
	// args は空でなくとも []string -> []any に変換されるのでスライスのはず
	if _, ok := m["args"].([]any); !ok {
		t.Errorf("args should be a JSON array, got %T (%v)", m["args"], m["args"])
	}

	// result の中身
	res, ok := m["result"].(map[string]any)
	if !ok {
		t.Fatalf("result should be object, got %T", m["result"])
	}
	if got := res["cwd"]; got != "/foo" {
		t.Errorf("result.cwd: got %v, want /foo", got)
	}
}

// TestResult_Error_JSON はエラー時の Result が想定どおりの JSON 形状になることを検証する。
func TestResult_Error_JSON(t *testing.T) {
	r := Result{
		OK:   false,
		Cmd:  "chdir",
		Args: []string{"/nonexistent"},
		Error: &Error{
			Code:      ErrInvalidArgs,
			Message:   "invalid path",
			Retryable: false,
		},
		ElapsedMs: 1,
	}

	raw, err := MarshalJSON(r)
	if err != nil {
		t.Fatalf("MarshalJSON: %v", err)
	}

	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	for _, key := range []string{"ok", "cmd", "args", "error", "elapsed_ms"} {
		if _, ok := m[key]; !ok {
			t.Errorf("expected key %q in JSON output, got %v", key, m)
		}
	}
	for _, key := range []string{"result", "postcondition"} {
		if _, ok := m[key]; ok {
			t.Errorf("unexpected key %q in JSON output, got %v", key, m)
		}
	}

	if got := m["ok"]; got != false {
		t.Errorf("ok: got %v, want false", got)
	}

	errObj, ok := m["error"].(map[string]any)
	if !ok {
		t.Fatalf("error should be object, got %T", m["error"])
	}
	if got := errObj["code"]; got != "invalid_args" {
		t.Errorf("error.code: got %v, want invalid_args", got)
	}
	if got := errObj["retryable"]; got != false {
		t.Errorf("error.retryable: got %v, want false", got)
	}
	if got := errObj["message"]; got != "invalid path" {
		t.Errorf("error.message: got %v, want %q", got, "invalid path")
	}
}

// TestResult_Args_NilNormalized は Args が nil でも JSON 出力では空配列 [] になることを検証する。
// AI エージェントが args を必ず array として unmarshal できるようにする保証。
func TestResult_Args_NilNormalized(t *testing.T) {
	r := Result{
		OK:   true,
		Cmd:  "noop",
		Args: nil,
	}
	raw, err := MarshalJSON(r)
	if err != nil {
		t.Fatalf("MarshalJSON: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	args, ok := m["args"]
	if !ok {
		t.Fatalf("args key missing")
	}
	if args == nil {
		t.Fatalf("args should be [] not null, got nil")
	}
	if _, ok := args.([]any); !ok {
		t.Errorf("args should be JSON array, got %T (%v)", args, args)
	}
}

// TestResult_PostconditionFailed_JSON は postcondition 失敗時の Result JSON 形を規定する。
//
// T010 で導入した「Data + Error + Postcondition の 3 キー共存」を明示的に固定する。
// 既存 TestResult_Error_JSON は Data=nil 入力に対する omitempty の挙動を確認しているだけで、
// 「OK=false なら result キーが出てはいけない」というガードは敷いていないため、
// 本テストは別関数として追加し、両者を温存する (plan §5.2)。
func TestResult_PostconditionFailed_JSON(t *testing.T) {
	type chdirData struct {
		Cwd       string  `json:"cwd"`
		GitBranch *string `json:"git_branch"`
	}

	r := Result{
		OK:   false,
		Cmd:  "chdir",
		Args: []string{"/bar"},
		Data: chdirData{Cwd: "/bar", GitBranch: nil},
		Error: &Error{
			Code:      ErrPostconditionFailed,
			Message:   "postcondition failed: 1 of 1 checks did not pass",
			Retryable: false,
		},
		Postcondition: &Postcondition{
			Passed: false,
			Checks: []Check{
				{Key: "cwd", Expected: "/foo", Actual: "/bar", Passed: false},
			},
		},
		ElapsedMs: 5,
	}

	raw, err := MarshalJSON(r)
	if err != nil {
		t.Fatalf("MarshalJSON: %v", err)
	}

	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	// 全キー存在 (result + error + postcondition の 3 キー共存が本テストの主眼)。
	for _, key := range []string{"ok", "cmd", "args", "result", "error", "postcondition", "elapsed_ms"} {
		if _, ok := m[key]; !ok {
			t.Errorf("expected key %q in JSON output, got %v", key, m)
		}
	}

	if got := m["ok"]; got != false {
		t.Errorf("ok: got %v, want false", got)
	}

	res, ok := m["result"].(map[string]any)
	if !ok {
		t.Fatalf("result: expected object, got %T", m["result"])
	}
	if got := res["cwd"]; got != "/bar" {
		t.Errorf("result.cwd: got %v, want /bar", got)
	}
	if got, ok := res["git_branch"]; !ok || got != nil {
		t.Errorf("result.git_branch: should be present and null, got (%v, exists=%v)", got, ok)
	}

	errObj, ok := m["error"].(map[string]any)
	if !ok {
		t.Fatalf("error: expected object, got %T", m["error"])
	}
	if got := errObj["code"]; got != "postcondition_failed" {
		t.Errorf("error.code: got %v, want postcondition_failed", got)
	}

	pc, ok := m["postcondition"].(map[string]any)
	if !ok {
		t.Fatalf("postcondition: expected object, got %T", m["postcondition"])
	}
	if got := pc["passed"]; got != false {
		t.Errorf("postcondition.passed: got %v, want false", got)
	}
	checks, ok := pc["checks"].([]any)
	if !ok {
		t.Fatalf("postcondition.checks: expected array, got %T", pc["checks"])
	}
	if len(checks) != 1 {
		t.Fatalf("postcondition.checks: expected len=1, got %d", len(checks))
	}
	first := checks[0].(map[string]any)
	for _, key := range []string{"key", "expected", "actual", "passed"} {
		if _, ok := first[key]; !ok {
			t.Errorf("checks[0] missing key %q (got %v)", key, first)
		}
	}
}

// TestErrorCode_Retryable は ErrorCode の既定 retryable 判定が false であることを検証する。
func TestErrorCode_Retryable(t *testing.T) {
	codes := []ErrorCode{ErrInvalidArgs, ErrNotFound, ErrNotADirectory, ErrExecFailed, ErrPostconditionFailed}
	for _, c := range codes {
		if c.Retryable() {
			t.Errorf("%s should not be retryable by default", c)
		}
	}
}
