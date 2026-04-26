"""T2-T6: summarize.py の単体テスト (stdlib only)."""
import io
import json
import os
import re
import sys
import tempfile
import unittest
from pathlib import Path

HERE = Path(__file__).resolve().parent
EVAL_ROOT = HERE.parent
FIXTURES = HERE / "fixtures"

# evals/ を import path に追加して summarize.py を読み込む
sys.path.insert(0, str(EVAL_ROOT))
import summarize  # noqa: E402


class TestExtractToolUses(unittest.TestCase):
    """T2: JSONL の assistant レコードから tool_use を抽出する."""

    def test_extracts_single_tool_use_from_assistant(self):
        path = FIXTURES / "sample-session-pass.jsonl"
        tool_uses = summarize.extract_tool_uses(path)
        self.assertEqual(len(tool_uses), 1)
        self.assertEqual(tool_uses[0]["name"], "Bash")
        self.assertEqual(tool_uses[0]["input"].get("command"), "ctxd chdir /tmp")

    def test_skips_sidechain_records(self):
        """isSidechain=True のレコードは無視されること (Task tool 内のサブエージェント発話を除外)."""
        path = FIXTURES / "sample-session-sidechain.jsonl"
        tool_uses = summarize.extract_tool_uses(path)
        names_inputs = [(t["name"], t["input"].get("command")) for t in tool_uses]
        self.assertIn(("Bash", "ctxd chdir /tmp"), names_inputs)
        self.assertNotIn(("Bash", "echo from-subagent"), names_inputs)

    def test_empty_jsonl_returns_empty(self):
        path = FIXTURES / "sample-session-empty.jsonl"
        tool_uses = summarize.extract_tool_uses(path)
        self.assertEqual(tool_uses, [])


class TestMatchPassFailError(unittest.TestCase):
    """T3: pass / fail / error 判定 (plan §6.3)."""

    def setUp(self):
        self.scenario = {
            "id": "chdir-01",
            "expected_tool": "Bash",
            "expected_args_pattern": r"ctxd\s+chdir\s+/tmp",
            "match_mode": "any",
        }

    def test_pass_when_tool_use_matches_and_exit_ok(self):
        tool_uses = [{"name": "Bash", "input": {"command": "ctxd chdir /tmp"}}]
        self.assertEqual(
            summarize.match(self.scenario, tool_uses, exit_status="ok"), "pass"
        )

    def test_fail_when_tool_use_present_but_pattern_mismatch(self):
        tool_uses = [{"name": "Bash", "input": {"command": "cd /tmp && ls"}}]
        self.assertEqual(
            summarize.match(self.scenario, tool_uses, exit_status="ok"), "fail"
        )

    def test_fail_when_no_tool_use_but_exit_ok(self):
        """m1 反映: tool_use 0 件 + exit_status=ok は fail (error ではない)."""
        self.assertEqual(
            summarize.match(self.scenario, [], exit_status="ok"), "fail"
        )

    def test_error_when_exit_status_not_ok(self):
        """exit_status が timeout 等のときは tool_use の中身に関わらず error."""
        tool_uses = [{"name": "Bash", "input": {"command": "ctxd chdir /tmp"}}]
        for status in (
            "timeout",
            "jsonl_missing",
            "claude_crashed",
            "ready_timeout",
            "new_workspace_parse_failed",
        ):
            with self.subTest(exit_status=status):
                self.assertEqual(
                    summarize.match(self.scenario, tool_uses, exit_status=status),
                    "error",
                )


class TestMatchModeFirst(unittest.TestCase):
    """T5 (m5 反映): match_mode=first は name 問わず最初の 1 件のみを評価."""

    def setUp(self):
        self.scenario = {
            "id": "first-mode",
            "expected_tool": "Bash",
            "expected_args_pattern": r"ctxd\s+chdir\s+/tmp",
            "match_mode": "first",
        }

    def test_first_fails_if_first_tool_is_wrong_name(self):
        tool_uses = [
            {"name": "Read", "input": {"file_path": "/etc/hosts"}},
            {"name": "Bash", "input": {"command": "ctxd chdir /tmp"}},
        ]
        self.assertEqual(
            summarize.match(self.scenario, tool_uses, exit_status="ok"), "fail"
        )

    def test_any_passes_with_same_inputs(self):
        scen = dict(self.scenario, match_mode="any")
        tool_uses = [
            {"name": "Read", "input": {"file_path": "/etc/hosts"}},
            {"name": "Bash", "input": {"command": "ctxd chdir /tmp"}},
        ]
        self.assertEqual(summarize.match(scen, tool_uses, exit_status="ok"), "pass")


class TestAggregate(unittest.TestCase):
    """T4: 集計結果が summary.md に必要な情報を含むこと."""

    def test_success_rate_and_claude_version_in_summary(self):
        results = [
            {"scenario_id": "chdir-01", "category": "chdir", "trial": 1, "verdict": "pass", "exit_status": "ok"},
            {"scenario_id": "chdir-01", "category": "chdir", "trial": 2, "verdict": "pass", "exit_status": "ok"},
            {"scenario_id": "chdir-01", "category": "chdir", "trial": 3, "verdict": "fail", "exit_status": "ok"},
        ]
        scenarios = [
            {"id": "chdir-01", "category": "chdir", "expected_tool": "Bash",
             "expected_args_pattern": r"ctxd\s+chdir\s+/tmp", "prompt": "p"}
        ]
        md = summarize.render_summary(
            results=results,
            scenarios=scenarios,
            claude_version="2.1.119 (Claude Code)",
            n=3,
            timestamp="2026-04-26 22:30:01",
        )
        self.assertIn("66.7%", md)
        self.assertIn("2.1.119 (Claude Code)", md)
        self.assertIn("chdir-01", md)
        self.assertIn("success rate", md.lower())

    def test_zero_total_does_not_div_by_zero(self):
        md = summarize.render_summary(
            results=[],
            scenarios=[],
            claude_version="x",
            n=0,
            timestamp="t",
        )
        self.assertIn("0", md)


class TestScenariosSchemaValidation(unittest.TestCase):
    """T6: 必須キー欠損 / id 重複 / regex 構文エラーは fail-fast."""

    def _write(self, content: str) -> str:
        f = tempfile.NamedTemporaryFile(
            mode="w", suffix=".jsonl", delete=False, encoding="utf-8"
        )
        f.write(content)
        f.close()
        self.addCleanup(os.unlink, f.name)
        return f.name

    def test_missing_required_key_raises(self):
        path = self._write(
            json.dumps({"id": "x", "prompt": "p", "expected_tool": "Bash"}) + "\n"
        )
        with self.assertRaises((SystemExit, ValueError)):
            summarize.load_scenarios(path)

    def test_duplicate_id_raises(self):
        line = json.dumps(
            {
                "id": "dup",
                "prompt": "p",
                "expected_tool": "Bash",
                "expected_args_pattern": "x",
            }
        )
        path = self._write(line + "\n" + line + "\n")
        with self.assertRaises((SystemExit, ValueError)):
            summarize.load_scenarios(path)

    def test_invalid_regex_raises(self):
        path = self._write(
            json.dumps(
                {
                    "id": "bad",
                    "prompt": "p",
                    "expected_tool": "Bash",
                    "expected_args_pattern": "(unclosed",
                }
            )
            + "\n"
        )
        with self.assertRaises((SystemExit, ValueError, re.error)):
            summarize.load_scenarios(path)

    def test_valid_scenarios_loaded(self):
        path = str(FIXTURES / "sample-scenarios.jsonl")
        scenarios = summarize.load_scenarios(path)
        self.assertEqual(len(scenarios), 1)
        self.assertEqual(scenarios[0]["id"], "chdir-01")


if __name__ == "__main__":
    unittest.main()
