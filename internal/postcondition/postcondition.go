// Package postcondition は --expect 指定された事後条件を検証するためのインタフェース。
//
// MVP ではダミーの NoOp 実装のみを提供する。DSL parser と各キー解決の実装は別タスクで行う。
package postcondition

import "github.com/hummer98/ctxd/internal/output"

// Verifier はコマンドの結果に対して --expect 指定された条件を検証する。
type Verifier interface {
	// Verify は raw な expect 指定（例: ["branch=main", "dirty=false"]）と
	// コマンド結果（任意の構造体 / map）を受け取り、Postcondition を返す。
	// expects が空なら nil を返してもよい（呼び出し側で「指定なし」を扱う）。
	Verify(expects []string, data any) *output.Postcondition
}

// NoOp は何も検証せず passed=true / checks=[] を返す雛形実装。
type NoOp struct{}

// Verify は expects の長さに関わらず常に passed=true を返す。
//
// TODO(task: postcondition DSL): key=value 解析と data からの値抽出を実装する。
// MVP では --expect が指定されたら受け取りはするが検証は行わない。
func (NoOp) Verify(_ []string, _ any) *output.Postcondition {
	return &output.Postcondition{
		Passed: true,
		Checks: []output.Check{},
	}
}
