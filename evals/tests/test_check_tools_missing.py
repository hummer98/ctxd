"""T021: evals/lib/check_tools_missing.py のユニットテスト.

CLI 規約 (run.sh から呼ばれる):
- exit 0 = missing  (tool_use が一件も見つからない)  → record_meta error "tools_missing"
- exit 1 = present  (tool_use が見つかる)            → record_meta ok ""
"""
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path

HERE = Path(__file__).resolve().parent
EVAL_ROOT = HERE.parent
FIXTURES = HERE / "fixtures"
SCRIPT = EVAL_ROOT / "lib" / "check_tools_missing.py"


def _run(tools_jsonl: Path, session_jsonl: Path) -> int:
    proc = subprocess.run(
        [
            sys.executable,
            str(SCRIPT),
            "--tools-jsonl", str(tools_jsonl),
            "--session-jsonl", str(session_jsonl),
        ],
        capture_output=True,
    )
    return proc.returncode


class TestCheckToolsMissing(unittest.TestCase):

    def test_returns_zero_when_both_empty(self):
        """tools.jsonl 空 + session JSONL 空 → exit 0 (= missing)."""
        with tempfile.TemporaryDirectory() as td:
            tools = Path(td) / "tools.jsonl"
            sess = Path(td) / "session.jsonl"
            tools.write_text("", encoding="utf-8")
            sess.write_text("", encoding="utf-8")
            self.assertEqual(_run(tools, sess), 0)

    def test_returns_one_when_tools_jsonl_has_record(self):
        """hook 出力 1 件 → exit 1 (= present)."""
        with tempfile.TemporaryDirectory() as td:
            sess = Path(td) / "session.jsonl"
            sess.write_text("", encoding="utf-8")
            tools = FIXTURES / "sample-tools-pass.jsonl"
            self.assertEqual(_run(tools, sess), 1)

    def test_returns_one_when_session_has_tool_use_fallback(self):
        """hook 空 + session に tool_use あり → exit 1 (= present, fallback で拾える)."""
        with tempfile.TemporaryDirectory() as td:
            tools = Path(td) / "tools.jsonl"
            tools.write_text("", encoding="utf-8")
            sess = FIXTURES / "sample-session-pass.jsonl"
            self.assertEqual(_run(tools, sess), 1)

    def test_returns_zero_when_neither_path_exists(self):
        """両 path 不在 → exit 0 (= missing)."""
        with tempfile.TemporaryDirectory() as td:
            tools = Path(td) / "no-such-tools.jsonl"
            sess = Path(td) / "no-such-session.jsonl"
            self.assertEqual(_run(tools, sess), 0)


if __name__ == "__main__":
    unittest.main()
