// Package runner は ctxd のサブコマンドを登録・実行する中央ディスパッチャ。
//
// cmd/ctxd/main.go は cobra でサブコマンドを束ねた後、各サブコマンドの RunE から
// runner.Dispatch を呼び出す。サブコマンドの本体実装は output.Result を組み立てて
// 返す責任を負う。
package runner

import (
	"context"
	"fmt"
	"time"

	"github.com/hummer98/ctxd/internal/output"
	"github.com/hummer98/ctxd/internal/postcondition"
)

// Flags は cobra root の persistent flag を runner に渡すための struct。
// サブコマンド固有 flag は含めない。
type Flags struct {
	Human  bool
	Expect []string // --expect KEY=VAL の繰り返し（postcondition の生入力）
}

// Command は ctxd のサブコマンドが満たすべき interface。
//
// 戻り値の型を Go 慣習の error ではなく *output.Error に固定しているのは意図的：
// ErrorCode の取りこぼしを型で防ぎ、呼び出し側で常に code を埋めることを強制する。
// 後続タスクの Implementer はこの戻り値型を error に書き換えないこと。
// 取りこぼしを許容するくらいなら、専用ラッパーを噛ませるべき。
type Command interface {
	Name() string
	Execute(ctx context.Context, args []string, flags Flags) (data any, err *output.Error)
}

// Registry はコマンド名 → Command 実装のルックアップを担う。
type Registry struct {
	cmds     map[string]Command
	verifier postcondition.Verifier
}

// NewRegistry は空の Registry を生成する。verifier は NoOp で初期化する。
func NewRegistry() *Registry {
	return &Registry{
		cmds:     map[string]Command{},
		verifier: postcondition.NoOp{},
	}
}

// Register は Command を登録する。同名のコマンドは上書きされる。
func (r *Registry) Register(c Command) {
	r.cmds[c.Name()] = c
}

// Lookup は名前から Command を引く。
func (r *Registry) Lookup(name string) (Command, bool) {
	c, ok := r.cmds[name]
	return c, ok
}

// SetVerifier は postcondition.Verifier を差し替える（主にテスト用途）。
func (r *Registry) SetVerifier(v postcondition.Verifier) {
	r.verifier = v
}

// Dispatch はサブコマンドを引いて実行し、output.Result に詰めて返す。
// ElapsedMs は Dispatch 入口からの経過時間で計測する（JSON 出力 I/O は含めない）。
func (r *Registry) Dispatch(ctx context.Context, name string, args []string, flags Flags) output.Result {
	start := time.Now()

	// args が nil でも Result.Args は空配列で返したい。
	// Result.MarshalJSON 側でも吸収するが、Result.Args 自体も正規化しておく。
	if args == nil {
		args = []string{}
	}

	res := output.Result{
		Cmd:  name,
		Args: args,
	}

	// 防御的 recover: Command 実装の panic をエラーに変換する。
	defer func() {
		if rec := recover(); rec != nil {
			res.OK = false
			res.Data = nil
			res.Error = output.NewError(
				output.ErrExecFailed,
				fmt.Sprintf("panic in command %q: %v", name, rec),
			)
			res.ElapsedMs = time.Since(start).Milliseconds()
		}
	}()

	cmd, ok := r.Lookup(name)
	if !ok {
		res.OK = false
		res.Error = output.NewError(output.ErrNotFound, fmt.Sprintf("unknown command: %s", name))
		res.ElapsedMs = time.Since(start).Milliseconds()
		return res
	}

	data, errOut := cmd.Execute(ctx, args, flags)
	if errOut != nil {
		res.OK = false
		res.Error = errOut
		res.ElapsedMs = time.Since(start).Milliseconds()
		return res
	}

	// postcondition 検証は --expect 指定時のみ Postcondition フィールドを埋める。
	if len(flags.Expect) > 0 && r.verifier != nil {
		pc := r.verifier.Verify(flags.Expect, data)
		res.Postcondition = pc
		// passed=false でも res.OK は true のまま（exit code は main 側で判断する）。
	}

	res.OK = true
	res.Data = data
	res.ElapsedMs = time.Since(start).Milliseconds()
	return res
}
