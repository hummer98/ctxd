#!/usr/bin/env python3
"""evals/lib/hook-tool-use.py — PostToolUse hook (T015).

claude-code が PostToolUse 時に stdin に流す JSON payload を読み、
指定された path に 1 行 JSONL を append する.

設計方針 (failure-tolerant):
- どんなエラーでも常に exit 0 で抜ける. claude のフローを止めない.
- 失敗時は stderr に短いメッセージのみ出す.
- hook が壊れた場合でも fallback の claude session JSONL から集計できる前提.

注: 本 hook は per tool_use ごとに python3 が spawn される.
    十数 trial × 数十 tool_use のスケールでは無視できるが、
    将来的に重い scenarios を入れる場合は C / sh wrapper への置換を検討.
"""
from __future__ import annotations

import json
import os
import sys
from datetime import datetime, timezone


def main(argv):
    if len(argv) < 2:
        print("hook-tool-use: missing output path", file=sys.stderr)
        return 0

    out_path = argv[1]
    try:
        payload_raw = sys.stdin.read()
    except Exception as e:
        print(f"hook-tool-use: stdin read failed: {e}", file=sys.stderr)
        return 0

    if not payload_raw.strip():
        return 0

    try:
        payload = json.loads(payload_raw)
    except Exception as e:
        print(f"hook-tool-use: stdin parse failed: {e}", file=sys.stderr)
        return 0

    if not isinstance(payload, dict):
        print("hook-tool-use: payload not a JSON object", file=sys.stderr)
        return 0

    record = {
        "name": payload.get("tool_name") or payload.get("name"),
        "input": payload.get("tool_input") or payload.get("input") or {},
        "tool_use_id": payload.get("tool_use_id"),
        "ts": datetime.now(timezone.utc).isoformat().replace("+00:00", "Z"),
    }

    try:
        parent = os.path.dirname(out_path)
        if parent:
            os.makedirs(parent, exist_ok=True)
        with open(out_path, "a", encoding="utf-8") as f:
            f.write(json.dumps(record, ensure_ascii=False) + "\n")
    except Exception as e:
        print(f"hook-tool-use: write failed: {e}", file=sys.stderr)
        return 0

    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv))
