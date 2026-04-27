"""T015: evals/lib/hook-tool-use.py の subprocess ベース単体テスト."""
import json
import os
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path

HERE = Path(__file__).resolve().parent
EVAL_ROOT = HERE.parent
HOOK_PY = EVAL_ROOT / "lib" / "hook-tool-use.py"


def _run_hook(out_path: str, stdin_text: str) -> subprocess.CompletedProcess:
    return subprocess.run(
        [sys.executable, str(HOOK_PY), out_path],
        input=stdin_text,
        capture_output=True,
        text=True,
        timeout=10,
    )


class TestHookToolUse(unittest.TestCase):
    def test_appends_one_record_for_valid_payload(self):
        with tempfile.TemporaryDirectory() as td:
            out = os.path.join(td, "tools.jsonl")
            payload = json.dumps({
                "tool_name": "Bash",
                "tool_input": {"command": "ls"},
                "tool_use_id": "id1",
            })
            res = _run_hook(out, payload)
            self.assertEqual(res.returncode, 0, msg=f"stderr={res.stderr}")
            self.assertTrue(os.path.exists(out))
            lines = [l for l in Path(out).read_text(encoding="utf-8").splitlines() if l]
            self.assertEqual(len(lines), 1)
            rec = json.loads(lines[0])
            self.assertEqual(rec["name"], "Bash")
            self.assertEqual(rec["input"], {"command": "ls"})
            self.assertEqual(rec["tool_use_id"], "id1")
            self.assertIn("ts", rec)

    def test_empty_stdin_exits_zero(self):
        with tempfile.TemporaryDirectory() as td:
            out = os.path.join(td, "tools.jsonl")
            res = _run_hook(out, "")
            self.assertEqual(res.returncode, 0)
            # 空 stdin の場合、ファイルは作成されない (or 空) どちらでも OK
            if os.path.exists(out):
                self.assertEqual(Path(out).read_text(encoding="utf-8"), "")

    def test_invalid_json_exits_zero(self):
        with tempfile.TemporaryDirectory() as td:
            out = os.path.join(td, "tools.jsonl")
            res = _run_hook(out, "{not valid json")
            self.assertEqual(res.returncode, 0)
            # stderr に何らかの message が出るが、claude を止めない
            self.assertIn("hook-tool-use", res.stderr)

    def test_creates_missing_output_directory(self):
        with tempfile.TemporaryDirectory() as td:
            out = os.path.join(td, "nested", "deep", "tools.jsonl")
            payload = json.dumps({
                "tool_name": "Read",
                "tool_input": {"file_path": "/etc/hosts"},
                "tool_use_id": "id2",
            })
            res = _run_hook(out, payload)
            self.assertEqual(res.returncode, 0, msg=f"stderr={res.stderr}")
            self.assertTrue(os.path.exists(out))
            rec = json.loads(Path(out).read_text(encoding="utf-8").splitlines()[0])
            self.assertEqual(rec["name"], "Read")


if __name__ == "__main__":
    unittest.main()
