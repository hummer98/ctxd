#!/usr/bin/env python3
"""project root の .claude-plugin/plugin.json から version を読み出す stdlib helper.

run.sh から `python3 evals/lib/read_plugin_version.py "$REPO_ROOT"` で stdout 取得。
"""
from __future__ import annotations

import json
import sys
from pathlib import Path
from typing import Union


def read_plugin_version(repo_root: Union[Path, str]) -> str:
    """`.claude-plugin/plugin.json` の `version` フィールドを返す.

    Args:
        repo_root: project root へのパス（`.claude-plugin/plugin.json` の親の親）。

    Returns:
        semver 文字列（例: "0.1.0"）。

    Raises:
        ValueError: ファイルが無い / JSON parse 失敗 / `version` キー欠損のいずれか。
    """
    p = Path(repo_root) / ".claude-plugin" / "plugin.json"
    if not p.exists():
        raise ValueError(f"plugin manifest not found: {p}")
    try:
        data = json.loads(p.read_text(encoding="utf-8"))
    except json.JSONDecodeError as e:
        raise ValueError(f"invalid JSON in {p}: {e}") from e
    version = data.get("version")
    if not isinstance(version, str) or not version:
        raise ValueError(f"missing or empty 'version' in {p}")
    return version


if __name__ == "__main__":
    repo_root = sys.argv[1] if len(sys.argv) > 1 else "."
    try:
        print(read_plugin_version(repo_root))
    except ValueError as e:
        print(f"read_plugin_version: {e}", file=sys.stderr)
        sys.exit(2)
