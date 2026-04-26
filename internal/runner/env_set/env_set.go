// Package envset は ctxd の env-set サブコマンドを実装する。
//
// `os.Setenv` で子プロセス内の環境変数を 1 個以上 set し、結果の set マップと
// 呼び出し前環境との diff (added / changed) を構造化 JSON で返す。
// 親シェルの環境変数は変更しない (docs/seed.md「parent shell 問題への扱い」)。
//
// ディレクトリ名は CLI 名と整合する `env_set` だが、Go の package 名は
// snake_case 不可のため `envset` とする (plan.md §2.1、git_switch と同パターン)。
package envset

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/hummer98/ctxd/internal/output"
	"github.com/hummer98/ctxd/internal/runner"
)

// envSetData は env-set コマンドの Result.Data に入るペイロード。
//
// Set / Diff いずれも JSON 出力で空でも null にならないよう、Execute / applySet で
// make() を使って必ず non-nil で初期化する (plan.md §2.2.1)。
type envSetData struct {
	Set  map[string]string `json:"set"`
	Diff envDiff           `json:"diff"`
}

// envDiff は parse 前の os.Environ() スナップショットとの差分。
type envDiff struct {
	Added   []string `json:"added"`
	Changed []string `json:"changed"`
}

// EnvSet は runner.Command 実装。environ / setenv は test から差し替え可能な
// function field にしておく (plan.md §2.3、git_switch と同パターン)。
type EnvSet struct {
	environ func() []string
	setenv  func(key, value string) error
}

// New は production 用の EnvSet を返す。
func New() *EnvSet {
	return &EnvSet{
		environ: os.Environ,
		setenv:  os.Setenv,
	}
}

// Name は runner.Command interface 実装。
func (e *EnvSet) Name() string { return "env-set" }

// Execute は runner.Command interface 実装。ctx は本コマンドでは未使用
// (in-memory の os.Setenv のみで cancel 観測の余地がない)。
func (e *EnvSet) Execute(_ context.Context, args []string, _ runner.Flags) (any, *output.Error) {
	if len(args) == 0 {
		return nil, output.NewError(output.ErrInvalidArgs,
			"env-set requires at least 1 argument, got 0")
	}
	return applySet(e.environ(), args, e.setenv)
}

// parseEnvArg は "KEY=val" を最初の '=' で 2 分割する (plan.md §2.4)。
//   - '=' が無い → ("", "", false)
//   - '=' が先頭 (KEY が空) → ("", "", false)
//   - 値が空 ("KEY=") → ("KEY", "", true)  POSIX export と整合
//   - 値内の '=' → 最初の '=' で split (URL/DSN を保持)
func parseEnvArg(arg string) (key, val string, ok bool) {
	i := strings.IndexByte(arg, '=')
	if i < 0 {
		return "", "", false
	}
	key = arg[:i]
	val = arg[i+1:]
	if key == "" {
		return "", "", false
	}
	return key, val, true
}

// applySet は snapshot を「呼び出し前の環境」とみなして args を順に setenv し、
// set / diff を組み立てる (plan.md §3.2)。
//
// parse error が出た時点で abort し ErrInvalidArgs を返す。
// それ以前の引数は既に setenv 適用済みだが、子プロセス終了で消えるため rollback はしない
// (plan.md §5.5)。
func applySet(snapshot []string, args []string, setenv func(k, v string) error) (envSetData, *output.Error) {
	snap := make(map[string]string, len(snapshot))
	for _, kv := range snapshot {
		if i := strings.IndexByte(kv, '='); i >= 0 {
			snap[kv[:i]] = kv[i+1:]
		}
	}

	set := make(map[string]string, len(args))
	for _, arg := range args {
		k, v, ok := parseEnvArg(arg)
		if !ok {
			return envSetData{}, output.NewError(output.ErrInvalidArgs,
				fmt.Sprintf("invalid KEY=VAL argument: %q", arg))
		}
		if err := setenv(k, v); err != nil {
			return envSetData{}, output.NewError(output.ErrExecFailed, err.Error())
		}
		set[k] = v
	}

	added := make([]string, 0)
	changed := make([]string, 0)
	for k, v := range set {
		old, exists := snap[k]
		switch {
		case !exists:
			added = append(added, k)
		case old != v:
			changed = append(changed, k)
		}
	}
	sort.Strings(added)
	sort.Strings(changed)

	return envSetData{
		Set:  set,
		Diff: envDiff{Added: added, Changed: changed},
	}, nil
}
