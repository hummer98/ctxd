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


class TestExtractToolUsesFromHook(unittest.TestCase):
    """T015: hook 出力 (PostToolUse hook の JSONL) から tool_use を抽出する."""

    def test_reads_hook_jsonl_when_present(self):
        hook_path = FIXTURES / "sample-tools-pass.jsonl"
        # 存在しない jsonl_path を指定しても、hook_path 優先で読まれる
        tool_uses = summarize.extract_tool_uses(
            jsonl_path=FIXTURES / "does-not-exist.jsonl",
            hook_path=hook_path,
        )
        self.assertEqual(len(tool_uses), 1)
        self.assertEqual(tool_uses[0]["name"], "Bash")
        self.assertEqual(tool_uses[0]["input"].get("command"), "ctxd chdir /tmp")

    def test_falls_back_to_session_jsonl_when_hook_empty(self):
        with tempfile.TemporaryDirectory() as td:
            empty_hook = Path(td) / "tools.jsonl"
            empty_hook.write_text("", encoding="utf-8")
            tool_uses = summarize.extract_tool_uses(
                jsonl_path=FIXTURES / "sample-session-pass.jsonl",
                hook_path=empty_hook,
            )
        # session JSONL から fallback で 1 件抽出される
        self.assertEqual(len(tool_uses), 1)
        self.assertEqual(tool_uses[0]["name"], "Bash")
        self.assertEqual(tool_uses[0]["input"].get("command"), "ctxd chdir /tmp")

    def test_returns_empty_when_both_missing(self):
        tool_uses = summarize.extract_tool_uses(
            jsonl_path=FIXTURES / "does-not-exist.jsonl",
            hook_path=FIXTURES / "also-not-exist.jsonl",
        )
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
            plugin_version="0.1.0",
            git_sha="7a95621",
            git_branch="main",
        )
        self.assertIn("66.7%", md)
        self.assertIn("2.1.119 (Claude Code)", md)
        self.assertIn("chdir-01", md)
        self.assertIn("success rate", md.lower())
        # T014: meta 拡充
        self.assertIn("plugin version: 0.1.0", md)
        self.assertIn("git SHA: 7a95621", md)
        self.assertIn("git branch: main", md)

    def test_zero_total_does_not_div_by_zero(self):
        md = summarize.render_summary(
            results=[],
            scenarios=[],
            claude_version="x",
            n=0,
            timestamp="t",
        )
        self.assertIn("0", md)

    def test_meta_defaults_to_unknown(self):
        """plugin_version / git_sha / git_branch を渡さない場合は default '(unknown)'."""
        md = summarize.render_summary(
            results=[],
            scenarios=[],
            claude_version="x",
            n=0,
            timestamp="t",
        )
        self.assertIn("plugin version: (unknown)", md)
        self.assertIn("git SHA: (unknown)", md)
        self.assertIn("git branch: (unknown)", md)


class TestRenderIndexRow(unittest.TestCase):
    """T014 Step 4: render_index_row が md / csv 1 行を返す.

    plan §5.3 の例: per_scenario_rates は `<id>:<rate>` を `;` 区切り、
    rate は 0.0-1.0 の小数 (strip しない、`%.2f`)。
    """

    def test_md_and_csv_row_basic(self):
        md_row, csv_row = summarize.render_index_row(
            timestamp="20260427-120130",
            plugin_version="0.1.0",
            claude_version="2.1.119 (Claude Code)",
            git_sha="7a95621",
            overall_rate=0.733,
            per_scenario=[
                ("chdir-01", 1.0),
                ("chdir-02", 0.67),
                ("git-switch-01", 1.0),
                ("git-switch-02", 0.33),
                ("env-set-01", 0.67),
            ],
        )
        # md: pipe-separated row (no leading heading)
        self.assertIn("20260427-120130", md_row)
        self.assertIn("0.1.0", md_row)
        self.assertIn("2.1.119 (Claude Code)", md_row)
        self.assertIn("7a95621", md_row)
        self.assertIn("73.3%", md_row)
        # per_scenario_rates は `;` 区切り、`,` を含めない
        self.assertIn("chdir-01:1.0", md_row)
        self.assertIn("chdir-02:0.67", md_row)
        self.assertIn(";", md_row)
        # csv 行は `,` 区切り、フィールド内に `,` を含めない
        self.assertIn("20260427-120130", csv_row)
        self.assertIn("73.3%", csv_row)
        # csv の per_scenario_rates フィールドが `;` 区切り
        # 各 csv field は `,` で 6 列
        self.assertEqual(csv_row.count(","), 5)

    def test_per_scenario_rates_preserve_input_order(self):
        """S1: render_index_row が per_scenario の入力順を保証する."""
        md_row, _ = summarize.render_index_row(
            timestamp="t",
            plugin_version="v",
            claude_version="c",
            git_sha="s",
            overall_rate=0.5,
            per_scenario=[("z-last", 0.0), ("a-first", 1.0), ("m-mid", 0.5)],
        )
        # md_row 内の per_scenario_rates を抽出して順序確認
        # md row format: "| t | v | c | s | 50.0% | z-last:0.0;a-first:1.0;m-mid:0.5 |"
        z_pos = md_row.find("z-last")
        a_pos = md_row.find("a-first")
        m_pos = md_row.find("m-mid")
        self.assertGreater(a_pos, z_pos)
        self.assertGreater(m_pos, a_pos)

    def test_zero_and_full_rate_format(self):
        """境界 0% / 100% が ".0%" で正しく表示される."""
        md_row_zero, csv_row_zero = summarize.render_index_row(
            timestamp="t", plugin_version="v", claude_version="c", git_sha="s",
            overall_rate=0.0, per_scenario=[("only-one", 0.0)],
        )
        self.assertIn("0.0%", md_row_zero)
        self.assertIn("only-one:0.0", md_row_zero)
        self.assertIn("0.0%", csv_row_zero)

        md_row_full, _ = summarize.render_index_row(
            timestamp="t", plugin_version="v", claude_version="c", git_sha="s",
            overall_rate=1.0, per_scenario=[("only-one", 1.0)],
        )
        self.assertIn("100.0%", md_row_full)

    def test_claude_version_comma_escaped_to_semicolon(self):
        """plan §5.3 注: claude_version 内の `,` が `;` に置換される (CSV 安全化)."""
        md_row, csv_row = summarize.render_index_row(
            timestamp="t", plugin_version="v",
            claude_version="2.1.119, hot-patch",
            git_sha="s",
            overall_rate=0.5, per_scenario=[("a", 1.0)],
        )
        self.assertNotIn(",", csv_row.split(",")[2])  # 3 列目に `,` 不在
        self.assertIn(";", csv_row)  # `;` で escape
        self.assertIn("2.1.119; hot-patch", md_row)

    def test_error_only_scenario_rate_zero(self):
        """全試行が error の scenario の rate は 0.0 として表示される.

        rate 計算は呼び出し側 (_per_scenario_rates) の責務だが、render_index_row が
        0.0 を受けた時の表示が `0.0` であることを確認。
        """
        md_row, _ = summarize.render_index_row(
            timestamp="t", plugin_version="v", claude_version="c", git_sha="s",
            overall_rate=0.0,
            per_scenario=[("error-only", 0.0), ("ok", 1.0)],
        )
        self.assertIn("error-only:0.0", md_row)
        self.assertIn("ok:1.0", md_row)


class TestAppendIndex(unittest.TestCase):
    """index.{md,csv} の新規生成 / 追記の挙動 (Test Coverage 追加: append 改行)."""

    def _md_row(self, ts="t1"):
        md_row, _ = summarize.render_index_row(
            timestamp=ts, plugin_version="v", claude_version="c",
            git_sha="s", overall_rate=0.5, per_scenario=[("a", 0.5)],
        )
        return md_row

    def _csv_row(self, ts="t1"):
        _, csv_row = summarize.render_index_row(
            timestamp=ts, plugin_version="v", claude_version="c",
            git_sha="s", overall_rate=0.5, per_scenario=[("a", 0.5)],
        )
        return csv_row

    def test_md_creates_with_heading_when_missing(self):
        with tempfile.TemporaryDirectory() as td:
            p = Path(td) / "index.md"
            summarize._append_index(p, self._md_row("ts1"), kind="md")
            content = p.read_text(encoding="utf-8")
            self.assertIn("# Eval Index", content)
            self.assertIn("| timestamp |", content)  # heading 行
            self.assertIn("| ts1 |", content)

    def test_csv_creates_with_header_when_missing(self):
        with tempfile.TemporaryDirectory() as td:
            p = Path(td) / "index.csv"
            summarize._append_index(p, self._csv_row("ts1"), kind="csv")
            content = p.read_text(encoding="utf-8")
            self.assertTrue(content.startswith("timestamp,"))
            self.assertIn("ts1,", content)

    def test_md_append_does_not_break_lines(self):
        """既存 index.md があっても 2 回 append で行が崩れないこと."""
        with tempfile.TemporaryDirectory() as td:
            p = Path(td) / "index.md"
            summarize._append_index(p, self._md_row("ts1"), kind="md")
            summarize._append_index(p, self._md_row("ts2"), kind="md")
            content = p.read_text(encoding="utf-8")
            # heading 1 つ + 行 2 つ
            self.assertEqual(content.count("# Eval Index"), 1)
            self.assertIn("| ts1 |", content)
            self.assertIn("| ts2 |", content)
            # 各行は独立 (改行で区切られている)
            lines = [l for l in content.splitlines() if l.startswith("| ts")]
            self.assertEqual(len(lines), 2)

    def test_csv_append_does_not_break_lines(self):
        with tempfile.TemporaryDirectory() as td:
            p = Path(td) / "index.csv"
            summarize._append_index(p, self._csv_row("ts1"), kind="csv")
            summarize._append_index(p, self._csv_row("ts2"), kind="csv")
            content = p.read_text(encoding="utf-8")
            self.assertEqual(content.count("timestamp,"), 1)
            data_lines = [l for l in content.splitlines() if l.startswith("ts")]
            self.assertEqual(len(data_lines), 2)


class TestPerScenarioRatesAggregation(unittest.TestCase):
    """_per_scenario_rates: error-only, all-pass, mixed の rate 計算."""

    def test_error_only_scenario_rate_is_zero(self):
        scenarios = [
            {"id": "err", "expected_tool": "Bash", "expected_args_pattern": "x", "prompt": "p"},
        ]
        results = [
            {"scenario_id": "err", "trial": 1, "verdict": "error", "exit_status": "timeout"},
            {"scenario_id": "err", "trial": 2, "verdict": "error", "exit_status": "timeout"},
        ]
        rates = summarize._per_scenario_rates(results, scenarios)
        self.assertEqual(rates, [("err", 0.0)])

    def test_all_pass_scenario_rate_is_one(self):
        scenarios = [
            {"id": "ok", "expected_tool": "Bash", "expected_args_pattern": "x", "prompt": "p"},
        ]
        results = [
            {"scenario_id": "ok", "trial": 1, "verdict": "pass", "exit_status": "ok"},
            {"scenario_id": "ok", "trial": 2, "verdict": "pass", "exit_status": "ok"},
        ]
        rates = summarize._per_scenario_rates(results, scenarios)
        self.assertEqual(rates, [("ok", 1.0)])

    def test_input_order_preserved(self):
        scenarios = [
            {"id": "z", "expected_tool": "Bash", "expected_args_pattern": "x", "prompt": "p"},
            {"id": "a", "expected_tool": "Bash", "expected_args_pattern": "x", "prompt": "p"},
        ]
        results = [
            {"scenario_id": "a", "trial": 1, "verdict": "pass", "exit_status": "ok"},
            {"scenario_id": "z", "trial": 1, "verdict": "fail", "exit_status": "ok"},
        ]
        rates = summarize._per_scenario_rates(results, scenarios)
        self.assertEqual([sid for sid, _ in rates], ["z", "a"])


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
