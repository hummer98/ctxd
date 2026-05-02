package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// runOnce は run() を *bytes.Buffer 経由で実行するテストヘルパ。
// stdout / stderr は io.Writer に広げてあるため (T010 plan §7.3 (A))、
// *bytes.Buffer を直接渡せる。
func runOnce(t *testing.T, args []string) (exitCode int, stdout, stderr string) {
	t.Helper()
	var sout, serr bytes.Buffer
	exit := run(context.Background(), args, &sout, &serr)
	return exit, sout.String(), serr.String()
}

// TestRun_Chdir_ExpectPass: chdir + --expect 通過 → exit 0、ok=true、postcondition.passed=true。
func TestRun_Chdir_ExpectPass(t *testing.T) {
	tmp := t.TempDir()
	exit, stdout, _ := runOnce(t, []string{
		"chdir", tmp,
		"--expect", "cwd=" + tmp,
	})
	if exit != 0 {
		t.Errorf("exit: got %d, want 0; stdout=%s", exit, stdout)
	}

	var m map[string]any
	if err := json.Unmarshal([]byte(stdout), &m); err != nil {
		t.Fatalf("unmarshal stdout: %v; raw=%q", err, stdout)
	}
	if got := m["ok"]; got != true {
		t.Errorf("ok: got %v, want true", got)
	}
	pc, ok := m["postcondition"].(map[string]any)
	if !ok {
		t.Fatalf("postcondition: expected object, got %T", m["postcondition"])
	}
	if pc["passed"] != true {
		t.Errorf("postcondition.passed: got %v, want true", pc["passed"])
	}
	checks, ok := pc["checks"].([]any)
	if !ok || len(checks) != 1 {
		t.Errorf("postcondition.checks: expected len=1 array, got %v", pc["checks"])
	}
}

// TestRun_Chdir_ExpectFail: chdir + 失敗する --expect → exit 1、ok=false、
// error.code=postcondition_failed、result/postcondition も共存することを確認 (plan §5.2)。
func TestRun_Chdir_ExpectFail(t *testing.T) {
	tmp := t.TempDir()
	exit, stdout, _ := runOnce(t, []string{
		"chdir", tmp,
		"--expect", "cwd=/nope",
	})
	if exit != 1 {
		t.Errorf("exit: got %d, want 1; stdout=%s", exit, stdout)
	}

	var m map[string]any
	if err := json.Unmarshal([]byte(stdout), &m); err != nil {
		t.Fatalf("unmarshal stdout: %v; raw=%q", err, stdout)
	}
	if got := m["ok"]; got != false {
		t.Errorf("ok: got %v, want false", got)
	}

	// result / error / postcondition の 3 キーが共存する (plan §5.2)。
	if _, ok := m["result"]; !ok {
		t.Errorf("result key missing (Data should be retained on postcondition_failed)")
	}
	errObj, ok := m["error"].(map[string]any)
	if !ok {
		t.Fatalf("error: expected object, got %T", m["error"])
	}
	if errObj["code"] != "postcondition_failed" {
		t.Errorf("error.code: got %v, want postcondition_failed", errObj["code"])
	}

	pc, ok := m["postcondition"].(map[string]any)
	if !ok {
		t.Fatalf("postcondition: expected object, got %T", m["postcondition"])
	}
	if pc["passed"] != false {
		t.Errorf("postcondition.passed: got %v, want false", pc["passed"])
	}
}

// TestRun_EnvSet_ExpectContainsPass: env-set + diff.added contains FOO 通過 → exit 0。
func TestRun_EnvSet_ExpectContainsPass(t *testing.T) {
	const key = "CTXD_TEST_T010_ENV_SET_FOO"
	if err := os.Unsetenv(key); err != nil {
		t.Fatalf("unsetenv: %v", err)
	}
	t.Cleanup(func() { _ = os.Unsetenv(key) })

	exit, stdout, _ := runOnce(t, []string{
		"env-set", key + "=bar",
		"--expect", "diff.added contains " + key,
		"--expect", "set." + key + "=bar",
	})
	if exit != 0 {
		t.Errorf("exit: got %d, want 0; stdout=%s", exit, stdout)
	}

	var m map[string]any
	if err := json.Unmarshal([]byte(stdout), &m); err != nil {
		t.Fatalf("unmarshal stdout: %v; raw=%q", err, stdout)
	}
	if m["ok"] != true {
		t.Errorf("ok: got %v, want true", m["ok"])
	}
	pc := m["postcondition"].(map[string]any)
	if pc["passed"] != true {
		t.Errorf("postcondition.passed: got %v, want true (checks=%v)", pc["passed"], pc["checks"])
	}
}

// TestRun_Chdir_ParseError: --expect が parse error の場合、Verifier 側で Check に
// "<parse error: ...>" として詰められ、ok=false / error.code=postcondition_failed になる
// (Q2 暫定 a)。
func TestRun_Chdir_ParseError(t *testing.T) {
	tmp := t.TempDir()
	exit, stdout, _ := runOnce(t, []string{
		"chdir", tmp,
		"--expect", "garbage_no_operator",
	})
	if exit != 1 {
		t.Errorf("exit: got %d, want 1", exit)
	}

	var m map[string]any
	if err := json.Unmarshal([]byte(stdout), &m); err != nil {
		t.Fatalf("unmarshal stdout: %v; raw=%q", err, stdout)
	}
	if m["ok"] != false {
		t.Errorf("ok: got %v, want false", m["ok"])
	}
	errObj := m["error"].(map[string]any)
	if errObj["code"] != "postcondition_failed" {
		t.Errorf("error.code: got %v, want postcondition_failed", errObj["code"])
	}
	pc := m["postcondition"].(map[string]any)
	checks := pc["checks"].([]any)
	if len(checks) != 1 {
		t.Fatalf("checks: expected len=1, got %d", len(checks))
	}
	first := checks[0].(map[string]any)
	if first["passed"] != false {
		t.Errorf("checks[0].passed: got %v, want false", first["passed"])
	}
	if got := first["actual"].(string); got[:len("<parse error:")] != "<parse error:" {
		t.Errorf("checks[0].actual: got %q, want '<parse error:' prefix", got)
	}
}

// TestRun_Chdir_NoExpect: --expect 無しでは postcondition キーが出ないことを確認。
func TestRun_Chdir_NoExpect(t *testing.T) {
	tmp := t.TempDir()
	exit, stdout, _ := runOnce(t, []string{"chdir", tmp})
	if exit != 0 {
		t.Errorf("exit: got %d, want 0; stdout=%s", exit, stdout)
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(stdout), &m); err != nil {
		t.Fatalf("unmarshal stdout: %v; raw=%q", err, stdout)
	}
	if _, ok := m["postcondition"]; ok {
		t.Errorf("postcondition key should be absent when --expect is not given, got %v", m["postcondition"])
	}
}

// TestRun_Chdir_ExpectPass_HumanMode: --human で複数行 JSON が出ても unmarshal できることを確認。
func TestRun_Chdir_ExpectPass_HumanMode(t *testing.T) {
	tmp := t.TempDir()
	exit, stdout, _ := runOnce(t, []string{
		"chdir", tmp,
		"--human",
		"--expect", "cwd=" + tmp,
	})
	if exit != 0 {
		t.Errorf("exit: got %d, want 0; stdout=%s", exit, stdout)
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(stdout), &m); err != nil {
		t.Fatalf("unmarshal stdout: %v; raw=%q", err, stdout)
	}
	if m["ok"] != true {
		t.Errorf("ok: got %v, want true", m["ok"])
	}
}

// TestRun_VersionTemplate: --version 出力が version / commit / built の 3 要素を含むことを確認 (T028)。
// goreleaser の -ldflags が空 (= デフォルト値) でも、template 経路自体が動いていることを保証する。
// commit / date は ldflags 経路が壊れたときに regression を即検出するためのガード。
func TestRun_VersionTemplate(t *testing.T) {
	exit, stdout, _ := runOnce(t, []string{"--version"})
	if exit != 0 {
		t.Errorf("exit: got %d, want 0; stdout=%s", exit, stdout)
	}
	// デフォルトでは version=0.0.0-dev / commit=unknown / date=unknown が埋まる想定。
	if !strings.Contains(stdout, "0.0.0-dev") {
		t.Errorf("--version stdout should contain version 0.0.0-dev; got %q", stdout)
	}
	if !strings.Contains(stdout, "commit ") {
		t.Errorf("--version stdout should contain 'commit '; got %q", stdout)
	}
	if !strings.Contains(stdout, "built ") {
		t.Errorf("--version stdout should contain 'built '; got %q", stdout)
	}
}

// TestRun_Chdir_AbsolutePathExpect は chdir が絶対パスに正規化することを踏まえた expect が通る確認。
// （内部実装の確認というより、e2e で path 正規化と postcondition が噛み合うかをガードする。）
func TestRun_Chdir_AbsolutePathExpect(t *testing.T) {
	tmp := t.TempDir()
	abs, err := filepath.Abs(tmp)
	if err != nil {
		t.Fatalf("Abs: %v", err)
	}
	exit, _, _ := runOnce(t, []string{
		"chdir", tmp,
		"--expect", "cwd=" + abs,
	})
	if exit != 0 {
		t.Errorf("exit: got %d, want 0", exit)
	}
}
