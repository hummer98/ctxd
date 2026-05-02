// ctxd のエントリポイント。
//
// cobra で root command を組み、persistent flag (--human, --expect) を宣言する。
// 個別サブコマンド (chdir / git-switch / env-set 等) の実装は別タスクで追加する。
//
// 終了コード規約 (MVP):
//   - 成功 (Result.OK == true): 0
//   - エラー (Result.OK == false): 1
//   - postcondition 違反は OK=false に統合 (error.code=postcondition_failed) → 1
//
// TODO(adr: 0002-exit-codes): postcondition 違反を exit 2 に分けるかは将来 ADR で議論する。
// AI エージェントが「コマンドは成功したが状態が期待と違う」を exit code レベルで
// 検出したくなる可能性があるため、保留。MVP は exit 1 で統一する。
package main

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"

	"github.com/hummer98/ctxd/internal/output"
	"github.com/hummer98/ctxd/internal/runner"
	"github.com/hummer98/ctxd/internal/runner/chdir"
	envset "github.com/hummer98/ctxd/internal/runner/env_set"
	gitswitch "github.com/hummer98/ctxd/internal/runner/git_switch"
)

// version / commit / date は build 時に -ldflags で上書きする想定。
// goreleaser から `-X main.version=... -X main.commit=... -X main.date=...` で埋める (T028)。
// 既定値はローカル `go build` 用のプレースホルダ。
var (
	version = "0.0.0-dev"
	commit  = "unknown"
	date    = "unknown"
)

func main() {
	os.Exit(run(context.Background(), os.Args[1:], os.Stdout, os.Stderr))
}

// run は main の本体を分離したもの。テストから差し替え可能。
// 戻り値は os.Exit に渡す exit code。
//
// サブコマンドの exit code は subcmdExit に集約する (D7)。
// RunE 内で os.Exit を直呼びしないことで run() の戻り値で観察可能にし、
// dispatchAndWrite の signature 変更を避けつつ exit code 規約を単一経路で守る。
//
// signature: stdout / stderr は io.Writer に広げる (T010)。
// e2e テストから *bytes.Buffer を直接渡せるようにし、test の可読性を上げるため。
// production 呼び出し (main()) からは os.Stdout / os.Stderr を渡す。
func run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	var subcmdExit int
	registry := runner.NewRegistry()
	rootCmd := newRootCmd(registry, stdout, &subcmdExit)
	rootCmd.SetArgs(args)
	rootCmd.SetOut(stdout)
	rootCmd.SetErr(stderr)
	if err := rootCmd.ExecuteContext(ctx); err != nil {
		// cobra のエラー（フラグ解釈失敗など）は構造化 JSON にせず、stderr に通常のエラーとして出す。
		// サブコマンドの実行失敗は newRootCmd 内で Result に変換済みのため、ここには来ない。
		fmt.Fprintln(stderr, err)
		return 1
	}
	return subcmdExit
}

// newRootCmd は ctxd の root cobra.Command を組み立てる。
// exitCode は RunE から書き戻すサブコマンド exit code の格納先 (D7)。
//
// chdir 単独の現時点では subcommand を本関数内に直書きする。次タスクで git-switch を
// 追加するタイミングで registerCommands helper への切り出しを検討する (Q2)。
func newRootCmd(registry *runner.Registry, stdout io.Writer, exitCode *int) *cobra.Command {
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
	// `--version` 出力に commit / built 日付を含める (T028)。
	// cobra の SetVersionTemplate は cobra.Command の field しか参照できないので、
	// commit / date は fmt.Sprintf で事前に組み立てた文字列に埋め込む。
	rootCmd.SetVersionTemplate(fmt.Sprintf("ctxd version %s (commit %s, built %s)\n", version, commit, date))

	// persistent flag は全サブコマンド共通。
	rootCmd.PersistentFlags().BoolVar(&human, "human", false, "human-readable output (pretty-printed JSON)")
	rootCmd.PersistentFlags().StringArrayVar(&expects, "expect", nil, "postcondition assertion in KEY=VALUE form (repeatable)")

	// chdir サブコマンド登録。
	registry.Register(chdir.New())
	chdirCmd := &cobra.Command{
		Use:   "chdir <path>",
		Short: "Change directory and report cwd / git_branch / listing as JSON.",
		Long: "Resolve the given path, list its contents, and report the git branch (if any) " +
			"as structured JSON. The parent shell's cwd is not modified.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			flags := runner.Flags{Human: human, Expect: expects}
			*exitCode = dispatchAndWrite(cmd.Context(), registry, stdout, "chdir", args, flags)
			// JSON は dispatchAndWrite で出力済み。cobra の error path に乗せず nil を返す。
			return nil
		},
	}
	rootCmd.AddCommand(chdirCmd)

	// git-switch サブコマンド登録。
	// TODO: 4 つ目以降のサブコマンド追加時に registerCommands ヘルパへ抽出する。
	registry.Register(gitswitch.New())
	gitSwitchCmd := &cobra.Command{
		Use:   "git-switch <branch>",
		Short: "Switch git branch and report branch / dirty / ahead / behind as JSON.",
		Long: "Run `git switch <branch>` and report the resulting working tree state " +
			"(branch, dirty, ahead, behind) as structured JSON.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			flags := runner.Flags{Human: human, Expect: expects}
			*exitCode = dispatchAndWrite(cmd.Context(), registry, stdout, "git-switch", args, flags)
			return nil
		},
	}
	rootCmd.AddCommand(gitSwitchCmd)

	// env-set サブコマンド登録。
	registry.Register(envset.New())
	envSetCmd := &cobra.Command{
		Use:   "env-set <KEY=VAL>...",
		Short: "Set environment variables and report set / diff as JSON.",
		Long: "Set one or more environment variables in this child process and report " +
			"the resulting `set` map and `diff` (added / changed) as structured JSON. " +
			"The parent shell's environment is not modified.",
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			flags := runner.Flags{Human: human, Expect: expects}
			*exitCode = dispatchAndWrite(cmd.Context(), registry, stdout, "env-set", args, flags)
			return nil
		},
	}
	rootCmd.AddCommand(envSetCmd)

	return rootCmd
}

// dispatchAndWrite はサブコマンドの RunE から呼ぶ想定の薄いブリッジ。
//
// exit code 判定は !res.OK の単一経路に集約する (T010)。
// postcondition 違反は runner.Dispatch 側で OK=false / error.code=postcondition_failed
// に統合済みのため、ここで Postcondition.Passed を別ルートで見る必要はない。
func dispatchAndWrite(
	ctx context.Context,
	registry *runner.Registry,
	stdout io.Writer,
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
	return 0
}
