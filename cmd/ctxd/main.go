// ctxd のエントリポイント。
//
// cobra で root command を組み、persistent flag (--human, --expect) を宣言する。
// 個別サブコマンド (chdir / git-switch / env-set 等) の実装は別タスクで追加する。
//
// 終了コード規約 (MVP):
//   - 成功 (Result.OK == true): 0
//   - エラー (Result.OK == false): 1
//   - postcondition 違反 (OK=true だが passed=false): 1
//
// TODO(adr: 0002-exit-codes): postcondition 違反を exit 2 に分けるかは将来 ADR で議論する。
// AI エージェントが「コマンドは成功したが状態が期待と違う」を exit code レベルで
// 検出したくなる可能性があるため、保留。MVP は exit 1 で統一する。
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/hummer98/ctxd/internal/output"
	"github.com/hummer98/ctxd/internal/runner"
)

// version は build 時に -ldflags で上書きする想定。MVP の既定値はプレースホルダ。
var version = "0.0.0-dev"

func main() {
	os.Exit(run(context.Background(), os.Args[1:], os.Stdout, os.Stderr))
}

// run は main の本体を分離したもの。テストから差し替え可能。
// 戻り値は os.Exit に渡す exit code。
func run(ctx context.Context, args []string, stdout, stderr *os.File) int {
	registry := runner.NewRegistry()
	rootCmd := newRootCmd(registry, stdout)
	rootCmd.SetArgs(args)
	rootCmd.SetOut(stdout)
	rootCmd.SetErr(stderr)
	if err := rootCmd.ExecuteContext(ctx); err != nil {
		// cobra のエラー（フラグ解釈失敗など）は構造化 JSON にせず、stderr に通常のエラーとして出す。
		// サブコマンドの実行失敗は newRootCmd 内で Result に変換済みのため、ここには来ない。
		fmt.Fprintln(stderr, err)
		return 1
	}
	return 0
}

// newRootCmd は ctxd の root cobra.Command を組み立てる。
// 個別サブコマンド登録は本関数の末尾で `addSubcommands(rootCmd, registry, ...)` の形で追加する想定。
func newRootCmd(registry *runner.Registry, stdout *os.File) *cobra.Command {
	var (
		human   bool
		expects []string
	)

	rootCmd := &cobra.Command{
		Use:   "ctxd",
		Short: "Declarative CLI commands that pass structured context to AI agents.",
		Long: "ctxd wraps shell operations (cd, export, git checkout) and returns " +
			"structured JSON so AI agents can observe state changes deterministically.",
		Version:       version,
		SilenceUsage:  true,
		SilenceErrors: false,
		// 引数なしで起動された場合は help を表示する。
		// これにより cobra が Runnable と判定し、--help でも Usage / Flags が表示される。
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}

	// persistent flag は全サブコマンド共通。
	rootCmd.PersistentFlags().BoolVar(&human, "human", false, "human-readable output (pretty-printed JSON)")
	rootCmd.PersistentFlags().StringArrayVar(&expects, "expect", nil, "postcondition assertion in KEY=VALUE form (repeatable)")

	// 個別サブコマンドはこのタスクでは登録しない。
	// 別タスクで registerCommands(rootCmd, registry, &human, &expects) のように追加する。
	_ = registry
	_ = stdout

	return rootCmd
}

// dispatchAndWrite はサブコマンドの RunE から呼ぶ想定の薄いブリッジ。
// 個別サブコマンド実装が増えたタイミングで使い始める。
//
// 現時点ではサブコマンド未登録のため未使用だが、後続タスクが利用するシグネチャを
// 固めるために定義しておく。
func dispatchAndWrite(
	ctx context.Context,
	registry *runner.Registry,
	stdout *os.File,
	name string,
	args []string,
	flags runner.Flags,
) int {
	res := registry.Dispatch(ctx, name, args, flags)
	if err := output.Write(stdout, res, flags.Human); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if !res.OK {
		return 1
	}
	if res.Postcondition != nil && !res.Postcondition.Passed {
		return 1
	}
	return 0
}
