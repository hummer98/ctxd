# encode_cwd: cwd を ~/.claude/projects/ の encoded 名に変換する。
#
# Claude Code は session JSONL を `~/.claude/projects/<encoded-cwd>/<session-id>.jsonl`
# に書き出す。encoded-cwd の規則は実機検証で「`/` と `.` を `-` に置換」で全例成立 (plan §2.5)。
#
# Usage: encode_cwd "/Users/foo/git/repo/.worktrees/x"
#        → "-Users-foo-git-repo--worktrees-x"
encode_cwd() {
  printf '%s' "$1" | tr '/.' '--'
}
