// Package gitops は git CLI を shell-out して構造化情報を取り出すための薄いラッパー。
//
// 設計方針 (plan.md §1):
//   - interface 経由 DI ではなく package-level 関数で公開する。
//   - 失敗時の error 分類は呼び出し側 (internal/runner/git_switch) で行う。本 package では
//     文字列マッチを共通化した判定 helper のみ公開する。
//   - 子プロセスには `LANG=C` / `LC_ALL=C` を必ず注入する。git の stderr が和訳されると
//     呼び出し側の grep が外れるため (plan.md §12)。
package gitops

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

// Status は `git status --porcelain=v2 -b -z` の構造化結果。
type Status struct {
	Branch   string // # branch.head の値。detached なら "" を返す。
	Upstream string // # branch.upstream の値。未設定なら ""。
	Ahead    int    // # branch.ab の +N。upstream 未設定時 0。
	Behind   int    // # branch.ab の -N（絶対値）。upstream 未設定時 0。
	Dirty    bool   // 1/2/u/? いずれかのエントリ行が 1 行でもあれば true。
}

// Switch は `git switch <branch>` を dir で実行する。
// 失敗時の error は `*exec.ExitError` を含む生のものを返す。エラー分類は呼び出し側で行う。
func Switch(ctx context.Context, dir, branch string) error {
	cmd := exec.CommandContext(ctx, "git", "switch", branch)
	cmd.Dir = dir
	cmd.Env = forcedEnv()
	_, err := cmd.Output()
	return err
}

// StatusV2 は `git status --porcelain=v2 -b -z` を実行し parse して返す。
func StatusV2(ctx context.Context, dir string) (Status, error) {
	cmd := exec.CommandContext(ctx, "git", "status", "--porcelain=v2", "-b", "-z")
	cmd.Dir = dir
	cmd.Env = forcedEnv()
	out, err := cmd.Output()
	if err != nil {
		return Status{}, err
	}
	return parseStatusV2(out)
}

// RevCount は `git rev-list --count --left-right HEAD...<upstream>` を実行し
// (ahead, behind) を返す。upstream が "" の場合は呼ばずに (0, 0, nil) を返す。
func RevCount(ctx context.Context, dir, upstream string) (ahead, behind int, err error) {
	if upstream == "" {
		return 0, 0, nil
	}
	spec := "HEAD..." + upstream
	cmd := exec.CommandContext(ctx, "git", "rev-list", "--count", "--left-right", spec)
	cmd.Dir = dir
	cmd.Env = forcedEnv()
	out, execErr := cmd.Output()
	if execErr != nil {
		return 0, 0, execErr
	}
	fields := strings.Fields(string(out))
	if len(fields) != 2 {
		return 0, 0, fmt.Errorf("RevCount: unexpected output %q", string(out))
	}
	a, errA := strconv.Atoi(fields[0])
	b, errB := strconv.Atoi(fields[1])
	if errA != nil || errB != nil {
		return 0, 0, fmt.Errorf("RevCount: parse %q: %v / %v", string(out), errA, errB)
	}
	return a, b, nil
}

// parseStatusV2 は --porcelain=v2 -b -z の bytes を Status に変換する。
// `-z` 区切りで NUL 分割し、ヘッダ行 (# で始まる) とエントリ行を順に走査する。
// rename/copy エントリ (`2 ...`) は pathname が NUL 2 個で区切られているため、
// 次のレコードを 1 つ skip する。
func parseStatusV2(b []byte) (Status, error) {
	var st Status
	if len(b) == 0 {
		return st, nil
	}
	records := bytes.Split(b, []byte{0})
	// 末尾の NUL で空文字レコードが 1 つ余分に来るのが普通。空はスキップする。
	for i := 0; i < len(records); i++ {
		rec := records[i]
		if len(rec) == 0 {
			continue
		}
		s := string(rec)
		switch {
		case strings.HasPrefix(s, "# branch.head "):
			head := strings.TrimPrefix(s, "# branch.head ")
			if head == "(detached)" {
				st.Branch = ""
			} else {
				st.Branch = head
			}
		case strings.HasPrefix(s, "# branch.upstream "):
			st.Upstream = strings.TrimPrefix(s, "# branch.upstream ")
		case strings.HasPrefix(s, "# branch.ab "):
			rest := strings.TrimPrefix(s, "# branch.ab ")
			parts := strings.Fields(rest)
			if len(parts) != 2 {
				return Status{}, fmt.Errorf("parseStatusV2: malformed branch.ab line %q", s)
			}
			a, errA := strconv.Atoi(parts[0])
			bv, errB := strconv.Atoi(parts[1])
			if errA != nil || errB != nil {
				return Status{}, fmt.Errorf("parseStatusV2: parse branch.ab %q: %v / %v", s, errA, errB)
			}
			st.Ahead = absInt(a)
			st.Behind = absInt(bv)
		case strings.HasPrefix(s, "# "):
			// 既知だが本 parser で使わないヘッダ (branch.oid 等) はスキップ。
		case strings.HasPrefix(s, "1 "):
			st.Dirty = true
		case strings.HasPrefix(s, "2 "):
			st.Dirty = true
			// rename/copy: 次のレコードが orig_path。skip する。
			if i+1 < len(records) {
				i++
			}
		case strings.HasPrefix(s, "u "):
			st.Dirty = true
		case strings.HasPrefix(s, "? "):
			st.Dirty = true
		case strings.HasPrefix(s, "! "):
			// ignored: dirty 扱いしない。--porcelain=v2 では通常出ない。
		default:
			// 未知のレコードは無視する。互換のため fail させない。
		}
	}
	return st, nil
}

func absInt(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

// forcedEnv は子プロセス用の env を作る。LANG=C / LC_ALL=C を必ず注入する。
// 親 env から既存の LANG / LC_ALL は除外して上書きする (plan.md §12)。
func forcedEnv() []string {
	parent := os.Environ()
	out := make([]string, 0, len(parent)+2)
	for _, kv := range parent {
		if strings.HasPrefix(kv, "LANG=") || strings.HasPrefix(kv, "LC_ALL=") {
			continue
		}
		out = append(out, kv)
	}
	out = append(out, "LANG=C", "LC_ALL=C")
	return out
}

// IsNotARepo は err（または stderr）が「git repo の外」を示すかを判定する。
func IsNotARepo(err error, stderr string) bool {
	if stderr != "" && strings.Contains(strings.ToLower(stderr), "not a git repository") {
		return true
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		if strings.Contains(strings.ToLower(string(ee.Stderr)), "not a git repository") {
			return true
		}
	}
	return false
}

// IsBranchNotFound は switch <branch> が「ブランチ不在」で失敗したかを判定する。
func IsBranchNotFound(stderr string) bool {
	low := strings.ToLower(stderr)
	if strings.Contains(low, "pathspec") && strings.Contains(low, "did not match") {
		return true
	}
	if strings.Contains(low, "invalid reference") {
		return true
	}
	if strings.Contains(low, "unknown revision") {
		return true
	}
	if strings.Contains(low, "did not match any") {
		return true
	}
	return false
}

// IsDirtyTree は switch <branch> が dirty で失敗したかを判定する。
func IsDirtyTree(stderr string) bool {
	low := strings.ToLower(stderr)
	if strings.Contains(low, "would be overwritten") {
		return true
	}
	if strings.Contains(low, "your local changes") {
		return true
	}
	if strings.Contains(low, "please commit your changes or stash them") {
		return true
	}
	return false
}

// IsGitMissing は git CLI 自体が見つからないかを判定する。
func IsGitMissing(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, exec.ErrNotFound) {
		return true
	}
	var execErr *exec.Error
	if errors.As(err, &execErr) {
		if errors.Is(execErr.Err, exec.ErrNotFound) {
			return true
		}
	}
	return false
}
