#!/usr/bin/env python3
"""project root の .claude-plugin/plugin.json から dot-path で値を取り出す stdlib helper.

run.sh から `python3 evals/lib/read_plugin_meta.py "$REPO_ROOT" author.name` で stdout 取得。

`read_plugin_version.py` と並列に置く (CLAUDE.md「派生先は手動同期しない」原則に揃え、
eval-plugin manifest の動的書換で author を継承するために使う)。
"""
from __future__ import annotations

import json
import sys
from pathlib import Path
from typing import Any, Union


def read_plugin_meta(repo_root: Union[Path, str], dot_path: str) -> str:
    """`.claude-plugin/plugin.json` の dot path 値を文字列で返す.

    Args:
        repo_root: project root へのパス。
        dot_path: ピリオド区切りのパス。例: "author.name"。

    Returns:
        - 値が文字列ならその値。
        - 値が dict / list ならその JSON 表現 (`json.dumps`)。
        - キー欠損 / 中間が dict でない場合は空文字列。

    Raises:
        ValueError: ファイルが無い / JSON parse 失敗。
    """
    p = Path(repo_root) / ".claude-plugin" / "plugin.json"
    if not p.exists():
        raise ValueError(f"plugin manifest not found: {p}")
    try:
        data: Any = json.loads(p.read_text(encoding="utf-8"))
    except json.JSONDecodeError as e:
        raise ValueError(f"invalid JSON in {p}: {e}") from e
    cur: Any = data
    for part in dot_path.split("."):
        if not isinstance(cur, dict) or part not in cur:
            return ""
        cur = cur[part]
    if isinstance(cur, str):
        return cur
    return json.dumps(cur, ensure_ascii=False)


if __name__ == "__main__":
    if len(sys.argv) < 3:
        print("usage: read_plugin_meta.py <repo_root> <dot_path>", file=sys.stderr)
        sys.exit(2)
    repo_root = sys.argv[1]
    dot_path = sys.argv[2]
    try:
        print(read_plugin_meta(repo_root, dot_path))
    except ValueError as e:
        print(f"read_plugin_meta: {e}", file=sys.stderr)
        sys.exit(2)
