#!/usr/bin/env python3
"""evals/lib/check_tools_missing.py — T021

run.sh から trial 末で呼ばれる薄いラッパー。hook 出力 (tool_uses_out) と
session JSONL の双方を見て、tool_use が一件も観測できなかったかを判定する。

CLI 規約:
- exit 0 = missing  (= absence is "true"): record_meta error "tools_missing"
- exit 1 = present  (= absence is "false"): record_meta ok ""

実装は summarize.extract_tool_uses をそのまま呼ぶだけ。
"""
from __future__ import annotations

import argparse
import sys
from pathlib import Path

HERE = Path(__file__).resolve().parent
EVAL_ROOT = HERE.parent
sys.path.insert(0, str(EVAL_ROOT))

import summarize  # noqa: E402


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description="check whether tool_uses are missing")
    parser.add_argument("--tools-jsonl", required=True, type=Path,
                        help="PostToolUse hook 出力 JSONL の path")
    parser.add_argument("--session-jsonl", required=True, type=Path,
                        help="claude session JSONL の path (fallback)")
    args = parser.parse_args(argv)

    tool_uses = summarize.extract_tool_uses(
        jsonl_path=args.session_jsonl,
        hook_path=args.tools_jsonl,
    )
    # exit 0 = missing (= absence is "true"), exit 1 = present
    return 0 if not tool_uses else 1


if __name__ == "__main__":
    sys.exit(main())
