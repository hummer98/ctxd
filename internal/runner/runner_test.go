package runner

import (
	"context"
	"testing"

	"github.com/hummer98/ctxd/internal/output"
	"github.com/hummer98/ctxd/internal/postcondition"
)

// fakeCmd は Command interface を満たす test 用ダミー。
// Execute の戻り値は data / err を test 側から差し替え可能にする。
type fakeCmd struct {
	name string
	data any
	err  *output.Error
}

func (f *fakeCmd) Name() string { return f.name }

func (f *fakeCmd) Execute(_ context.Context, _ []string, _ Flags) (any, *output.Error) {
	return f.data, f.err
}

// TestDispatch_NoExpect は flags.Expect=nil のとき Postcondition は nil で Data は data どおりであることを確認。
func TestDispatch_NoExpect(t *testing.T) {
	r := NewRegistry()
	r.Register(&fakeCmd{name: "fake", data: map[string]any{"cwd": "/foo"}})

	res := r.Dispatch(context.Background(), "fake", nil, Flags{})

	if !res.OK {
		t.Errorf("OK: got false, want true; error=%+v", res.Error)
	}
	if res.Postcondition != nil {
		t.Errorf("Postcondition: got %+v, want nil", res.Postcondition)
	}
	if res.Data == nil {
		t.Errorf("Data: got nil, want non-nil")
	}
}

// TestDispatch_ExpectPass は --expect が全 pass のとき OK=true / Postcondition.Passed=true を確認。
func TestDispatch_ExpectPass(t *testing.T) {
	type d struct {
		Cwd string `json:"cwd"`
	}
	r := NewRegistry()
	r.Register(&fakeCmd{name: "fake", data: d{Cwd: "/foo"}})

	res := r.Dispatch(context.Background(), "fake", nil, Flags{
		Expect: []string{"cwd=/foo"},
	})

	if !res.OK {
		t.Errorf("OK: got false, want true; error=%+v postcondition=%+v", res.Error, res.Postcondition)
	}
	if res.Postcondition == nil {
		t.Fatalf("Postcondition: got nil, want non-nil")
	}
	if !res.Postcondition.Passed {
		t.Errorf("Postcondition.Passed: got false, want true; checks=%+v", res.Postcondition.Checks)
	}
	if len(res.Postcondition.Checks) != 1 {
		t.Errorf("len(Checks): got %d, want 1", len(res.Postcondition.Checks))
	}
}

// TestDispatch_ExpectFail は --expect が落ちたとき OK=false / Error.Code=postcondition_failed /
// Postcondition.Passed=false / Data 保持を確認 (plan §5.1 / §5.2)。
func TestDispatch_ExpectFail(t *testing.T) {
	type d struct {
		Cwd string `json:"cwd"`
	}
	dataIn := d{Cwd: "/foo"}
	r := NewRegistry()
	r.Register(&fakeCmd{name: "fake", data: dataIn})

	res := r.Dispatch(context.Background(), "fake", nil, Flags{
		Expect: []string{"cwd=/bar"},
	})

	if res.OK {
		t.Errorf("OK: got true, want false")
	}
	if res.Error == nil {
		t.Fatalf("Error: got nil, want non-nil")
	}
	if res.Error.Code != output.ErrPostconditionFailed {
		t.Errorf("Error.Code: got %q, want %q", res.Error.Code, output.ErrPostconditionFailed)
	}
	if res.Postcondition == nil || res.Postcondition.Passed {
		t.Errorf("Postcondition.Passed: should be false; got %+v", res.Postcondition)
	}
	// Data は保持される (plan §5.2: AI が「実観測値」を見られるようにする)。
	if res.Data == nil {
		t.Errorf("Data: should be retained even when postcondition failed; got nil")
	}
}

// TestDispatch_ExpectFail_MessageSummary は Error.Message が「N of M」サマリ形式 (plan §5.5) であることを確認。
func TestDispatch_ExpectFail_MessageSummary(t *testing.T) {
	type d struct {
		Cwd   string `json:"cwd"`
		Dirty bool   `json:"dirty"`
	}
	r := NewRegistry()
	r.Register(&fakeCmd{name: "fake", data: d{Cwd: "/foo", Dirty: false}})

	res := r.Dispatch(context.Background(), "fake", nil, Flags{
		Expect: []string{"cwd=/foo", "dirty=true"}, // 1 件 fail
	})

	if res.OK {
		t.Errorf("OK: should be false")
	}
	if res.Error == nil {
		t.Fatalf("Error: should be non-nil")
	}
	// "1 of 2" を含む
	if !contains(res.Error.Message, "1 of 2") {
		t.Errorf("Error.Message should contain '1 of 2', got %q", res.Error.Message)
	}
}

// TestDispatch_VerifierDefaultIsRealImpl は NewRegistry が Default Verifier を採用していることを確認。
// (NoOp のままだったら expect=fail でも OK=true になってしまうため、ここで gate する。)
func TestDispatch_VerifierDefaultIsRealImpl(t *testing.T) {
	type d struct {
		Cwd string `json:"cwd"`
	}
	r := NewRegistry()
	r.Register(&fakeCmd{name: "fake", data: d{Cwd: "/foo"}})

	res := r.Dispatch(context.Background(), "fake", nil, Flags{
		Expect: []string{"cwd=/bar"},
	})
	if res.OK {
		t.Fatalf("default verifier appears to be NoOp: expected OK=false on mismatch")
	}
}

// TestDispatch_NoOpVerifierStillWorks は SetVerifier(NoOp{}) で既存の DI 経路が生きていることを確認。
func TestDispatch_NoOpVerifierStillWorks(t *testing.T) {
	type d struct {
		Cwd string `json:"cwd"`
	}
	r := NewRegistry()
	r.SetVerifier(postcondition.NoOp{})
	r.Register(&fakeCmd{name: "fake", data: d{Cwd: "/foo"}})

	res := r.Dispatch(context.Background(), "fake", nil, Flags{
		Expect: []string{"cwd=/bar"},
	})
	// NoOp は常に passed=true を返すので OK は true のまま。
	if !res.OK {
		t.Errorf("OK: with NoOp verifier should remain true, got false; error=%+v", res.Error)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
