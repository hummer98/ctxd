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

// TestErrorCode_Retryable は ErrorCode の既定 retryable 判定が false であることを検証する。
func TestErrorCode_Retryable(t *testing.T) {
	codes := []ErrorCode{ErrInvalidArgs, ErrNotFound, ErrNotADirectory, ErrExecFailed, ErrPostconditionFailed}
	for _, c := range codes {
		if c.Retryable() {
			t.Errorf("%s should not be retryable by default", c)
		}
	}
}
