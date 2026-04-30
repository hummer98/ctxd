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
            "tools_missing",
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


class TestMatchModeAnyMatch(unittest.TestCase):
    """T018: match_mode=any-match は Bash 直接 ctxd OR Skill ctxd-eval:ctxd で pass.

    plan §1.1 採用案セマンティクス:
    - Bash 経路: name == "Bash" かつ input.command が pattern hit
    - Skill 経路: name == "Skill" かつ input.skill == "ctxd-eval:ctxd"
      (args の中身は問わない — Skill loader が ctxd-eval を選んだ時点で
       shell mutation を ctxd 経路に乗せる責務を負ったとみなす)
    """

    def setUp(self):
        self.scenario = {
            "id": "chdir-01",
            "expected_tool": "Bash",
            "expected_args_pattern": r"ctxd\s+chdir\s+/tmp",
            "match_mode": "any-match",
        }

    def test_pass_when_bash_with_ctxd(self):
        """既存 any と同等の Bash 経路 pass."""
        tool_uses = [{"name": "Bash", "input": {"command": "ctxd chdir /tmp"}}]
        self.assertEqual(
            summarize.match(self.scenario, tool_uses, exit_status="ok"), "pass"
        )

    def test_pass_when_skill_ctxd_eval_only(self):
        """Skill 単独 (Bash で ctxd 未到達) でも pass."""
        tool_uses = [
            {"name": "Skill", "input": {"skill": "ctxd-eval:ctxd", "args": "cd /tmp"}},
            {"name": "Bash", "input": {"command": "ls /tmp"}},
        ]
        self.assertEqual(
            summarize.match(self.scenario, tool_uses, exit_status="ok"), "pass"
        )

    def test_fail_when_skill_other_than_ctxd_eval(self):
        """Skill skill='update-config' は pass にしない."""
        tool_uses = [
            {"name": "Skill", "input": {"skill": "update-config", "args": "..."}},
            {"name": "Bash", "input": {"command": "echo hi"}},
        ]
        self.assertEqual(
            summarize.match(self.scenario, tool_uses, exit_status="ok"), "fail"
        )

    def test_fail_when_no_ctxd_anywhere(self):
        """Bash も Skill も ctxd 経由でない場合 fail."""
        tool_uses = [
            {"name": "Read", "input": {"file_path": "/etc/hosts"}},
            {"name": "Bash", "input": {"command": "cd /tmp && ls"}},
        ]
        self.assertEqual(
            summarize.match(self.scenario, tool_uses, exit_status="ok"), "fail"
        )

    def test_pass_when_bash_after_ctxd_invoke(self):
        """ctxd 呼出の後に補助 Bash (例: ls /tmp) が来ても pass (補助呼出許容)."""
        tool_uses = [
            {"name": "Bash", "input": {"command": "ctxd chdir /tmp"}},
            {"name": "Bash", "input": {"command": "ls /tmp"}},
        ]
        self.assertEqual(
            summarize.match(self.scenario, tool_uses, exit_status="ok"), "pass"
        )

    def test_pass_via_skill_chain_fixture(self):
        """T017 で観察された Skill→Bash 連鎖 trial 形の fixture で pass 判定."""
        path = FIXTURES / "sample-tools-skill-chain.jsonl"
        tool_uses = summarize.extract_tool_uses(
            jsonl_path=FIXTURES / "does-not-exist.jsonl",
            hook_path=path,
        )
        self.assertEqual(
            summarize.match(self.scenario, tool_uses, exit_status="ok"), "pass"
        )

    def test_existing_any_unchanged_for_skill_only(self):
        """既存 'any' mode は Skill 単独では pass にならない (後方互換性確認)."""
        scen = dict(self.scenario, match_mode="any")
        tool_uses = [
            {"name": "Skill", "input": {"skill": "ctxd-eval:ctxd", "args": "cd /tmp"}},
        ]
        # any mode は expected_tool=Bash 限定走査なので Skill 単独では fail
        self.assertEqual(summarize.match(scen, tool_uses, exit_status="ok"), "fail")


class TestLoadScenariosSetupField(unittest.TestCase):
    """T018: scenarios の任意 setup フィールド (str / 不在 / 非 string で fail)."""

    def _write(self, content: str) -> str:
        f = tempfile.NamedTemporaryFile(
            mode="w", suffix=".jsonl", delete=False, encoding="utf-8"
        )
        f.write(content)
        f.close()
        self.addCleanup(os.unlink, f.name)
        return f.name

    def test_setup_string_accepted(self):
        path = self._write(
            json.dumps(
                {
                    "id": "x",
                    "prompt": "p",
                    "expected_tool": "Bash",
                    "expected_args_pattern": r"ctxd",
                    "setup": "git branch feature-eval origin/main 2>/dev/null || true",
                }
            )
            + "\n"
        )
        scenarios = summarize.load_scenarios(path)
        self.assertEqual(len(scenarios), 1)
        self.assertIn("git branch feature-eval", scenarios[0].get("setup", ""))

    def test_setup_absent_accepted(self):
        """setup 不在は default 動作 (実行しない) — load 自体は成功する."""
        path = self._write(
            json.dumps(
                {
                    "id": "x",
                    "prompt": "p",
                    "expected_tool": "Bash",
                    "expected_args_pattern": r"ctxd",
                }
            )
            + "\n"
        )
        scenarios = summarize.load_scenarios(path)
        self.assertEqual(len(scenarios), 1)
        # setup が空の場合は key 自体が無い or 空文字
        self.assertFalse(scenarios[0].get("setup"))

    def test_setup_non_string_rejected(self):
        path = self._write(
            json.dumps(
                {
                    "id": "x",
                    "prompt": "p",
                    "expected_tool": "Bash",
                    "expected_args_pattern": r"ctxd",
                    "setup": 123,
                }
            )
            + "\n"
        )
        with self.assertRaises((SystemExit, ValueError)):
            summarize.load_scenarios(path)


class TestLoadScenariosTeardownField(unittest.TestCase):
    """T021: scenarios の任意 teardown フィールド (str / 不在 / 非 string で fail)."""

    def _write(self, content: str) -> str:
        f = tempfile.NamedTemporaryFile(
            mode="w", suffix=".jsonl", delete=False, encoding="utf-8"
        )
        f.write(content)
        f.close()
        self.addCleanup(os.unlink, f.name)
        return f.name

    def test_teardown_string_accepted(self):
        path = self._write(
            json.dumps(
                {
                    "id": "x",
                    "prompt": "p",
                    "expected_tool": "Bash",
                    "expected_args_pattern": r"ctxd",
                    "teardown": "git branch -D feature-eval 2>/dev/null || true",
                }
            )
            + "\n"
        )
        scenarios = summarize.load_scenarios(path)
        self.assertEqual(len(scenarios), 1)
        self.assertIn("git branch -D feature-eval", scenarios[0].get("teardown", ""))

    def test_teardown_absent_accepted(self):
        """teardown 不在は default 動作 (実行しない) — load 自体は成功する."""
        path = self._write(
            json.dumps(
                {
                    "id": "x",
                    "prompt": "p",
                    "expected_tool": "Bash",
                    "expected_args_pattern": r"ctxd",
                }
            )
            + "\n"
        )
        scenarios = summarize.load_scenarios(path)
        self.assertEqual(len(scenarios), 1)
        self.assertFalse(scenarios[0].get("teardown"))

    def test_teardown_non_string_rejected(self):
        path = self._write(
            json.dumps(
                {
                    "id": "x",
                    "prompt": "p",
                    "expected_tool": "Bash",
                    "expected_args_pattern": r"ctxd",
                    "teardown": 123,
                }
            )
            + "\n"
        )
        with self.assertRaises((SystemExit, ValueError)):
            summarize.load_scenarios(path)


class TestLoadScenariosAnyMatchAccepted(unittest.TestCase):
    """T018: load_scenarios が match_mode='any-match' を受理する."""

    def _write(self, content: str) -> str:
        f = tempfile.NamedTemporaryFile(
            mode="w", suffix=".jsonl", delete=False, encoding="utf-8"
        )
        f.write(content)
        f.close()
        self.addCleanup(os.unlink, f.name)
        return f.name

    def test_any_match_accepted(self):
        path = self._write(
            json.dumps(
                {
                    "id": "x",
                    "prompt": "p",
                    "expected_tool": "Bash",
                    "expected_args_pattern": r"ctxd\s+chdir",
                    "match_mode": "any-match",
                }
            )
            + "\n"
        )
        scenarios = summarize.load_scenarios(path)
        self.assertEqual(len(scenarios), 1)
        self.assertEqual(scenarios[0]["match_mode"], "any-match")

    def test_unknown_match_mode_still_rejected(self):
        path = self._write(
            json.dumps(
                {
                    "id": "x",
                    "prompt": "p",
                    "expected_tool": "Bash",
                    "expected_args_pattern": r"ctxd\s+chdir",
                    "match_mode": "bogus",
                }
            )
            + "\n"
        )
        with self.assertRaises((SystemExit, ValueError)):
            summarize.load_scenarios(path)


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
            model="claude-opus-4-7",
        )
        # md: pipe-separated row (no leading heading)
        self.assertIn("20260427-120130", md_row)
        self.assertIn("0.1.0", md_row)
        self.assertIn("2.1.119 (Claude Code)", md_row)
        self.assertIn("7a95621", md_row)
        self.assertIn("73.3%", md_row)
        # T022: model 列が 2 番目 (timestamp の直後) に入る
        self.assertIn("claude-opus-4-7", md_row)
        self.assertLess(md_row.find("claude-opus-4-7"), md_row.find("0.1.0"))
        # per_scenario_rates は `;` 区切り、`,` を含めない
        self.assertIn("chdir-01:1.0", md_row)
        self.assertIn("chdir-02:0.67", md_row)
        self.assertIn(";", md_row)
        # csv 行は `,` 区切り、フィールド内に `,` を含めない
        self.assertIn("20260427-120130", csv_row)
        self.assertIn("73.3%", csv_row)
        self.assertIn("claude-opus-4-7", csv_row)
        # T024: csv は 11 列 (timestamp,model,plugin_version,claude_version,git_sha,
        # overall_rate,per_scenario_rates,avg_tool_uses,avg_input_tokens,
        # avg_output_tokens,avg_wall_ms) なので `,` は 10 個
        self.assertEqual(csv_row.count(","), 10)

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


class TestRenderIndexRowModel(unittest.TestCase):
    """T022: render_index_row が model 列を出力する.

    列順は `timestamp | model | plugin_version | claude_version | git_sha | overall_rate
    | per_scenario_rates` で固定 (migration 済みの既存 index.{md,csv} と同じ並び).
    """

    def test_model_column_appears_in_md_after_timestamp(self):
        md_row, _ = summarize.render_index_row(
            timestamp="20260428-200000",
            plugin_version="0.1.3",
            claude_version="2.1.121 (Claude Code)",
            git_sha="abc1234",
            overall_rate=1.0,
            per_scenario=[("chdir-01", 1.0)],
            model="claude-sonnet-4-5",
        )
        ts_pos = md_row.find("20260428-200000")
        model_pos = md_row.find("claude-sonnet-4-5")
        plugin_pos = md_row.find("0.1.3")
        # model は timestamp の後、plugin_version の前
        self.assertGreater(model_pos, ts_pos)
        self.assertLess(model_pos, plugin_pos)

    def test_model_column_appears_in_csv_after_timestamp(self):
        _, csv_row = summarize.render_index_row(
            timestamp="20260428-200000",
            plugin_version="0.1.3",
            claude_version="2.1.121 (Claude Code)",
            git_sha="abc1234",
            overall_rate=1.0,
            per_scenario=[("chdir-01", 1.0)],
            model="claude-sonnet-4-5",
        )
        # csv field 順: 0=timestamp, 1=model, 2=plugin_version, ...
        fields = csv_row.split(",")
        self.assertEqual(fields[0], "20260428-200000")
        self.assertEqual(fields[1], "claude-sonnet-4-5")
        self.assertEqual(fields[2], "0.1.3")

    def test_model_default_is_unknown_placeholder(self):
        """model 引数を渡さないと '(unknown)' が入る (テストしやすさのため default あり)."""
        _, csv_row = summarize.render_index_row(
            timestamp="t", plugin_version="v", claude_version="c", git_sha="s",
            overall_rate=0.5, per_scenario=[("a", 1.0)],
        )
        fields = csv_row.split(",")
        self.assertEqual(fields[1], "(unknown)")

    def test_model_with_comma_escaped_to_semicolon(self):
        """model に `,` が含まれた場合 csv 安全化 (claude_version と同じルール)."""
        _, csv_row = summarize.render_index_row(
            timestamp="t", plugin_version="v", claude_version="c", git_sha="s",
            overall_rate=0.5, per_scenario=[("a", 1.0)],
            model="claude,opus",
        )
        # csv 列数は 11 のまま (model 内の `,` が `;` に置換されているはず) — T024
        self.assertEqual(csv_row.count(","), 10)
        self.assertIn("claude;opus", csv_row)


class TestIndexHeadingHasModelColumn(unittest.TestCase):
    """T022: 新規 index.{md,csv} を作るとき heading に model 列が含まれる."""

    def _row(self):
        md_row, csv_row = summarize.render_index_row(
            timestamp="ts1", plugin_version="0.1.3", claude_version="c",
            git_sha="s", overall_rate=1.0, per_scenario=[("a", 1.0)],
            model="claude-opus-4-7",
        )
        return md_row, csv_row

    def test_md_heading_includes_model(self):
        with tempfile.TemporaryDirectory() as td:
            p = Path(td) / "index.md"
            md_row, _ = self._row()
            summarize._append_index(p, md_row, kind="md")
            content = p.read_text(encoding="utf-8")
            heading_line = next(
                (l for l in content.splitlines() if l.startswith("| timestamp")),
                "",
            )
            self.assertIn("model", heading_line)
            # 列順: timestamp が最左、model が 2 列目
            ts_pos = heading_line.find("timestamp")
            model_pos = heading_line.find("model")
            plugin_pos = heading_line.find("plugin_version")
            self.assertGreater(model_pos, ts_pos)
            self.assertLess(model_pos, plugin_pos)

    def test_csv_heading_includes_model(self):
        with tempfile.TemporaryDirectory() as td:
            p = Path(td) / "index.csv"
            _, csv_row = self._row()
            summarize._append_index(p, csv_row, kind="csv")
            content = p.read_text(encoding="utf-8")
            heading = content.splitlines()[0]
            fields = heading.split(",")
            # T024: 11 列。index 1 が model、末尾 4 列が efficiency 指標
            self.assertEqual(len(fields), 11)
            self.assertEqual(fields[0], "timestamp")
            self.assertEqual(fields[1], "model")
            self.assertEqual(fields[2], "plugin_version")
            self.assertEqual(fields[-4], "avg_tool_uses")
            self.assertEqual(fields[-3], "avg_input_tokens")
            self.assertEqual(fields[-2], "avg_output_tokens")
            self.assertEqual(fields[-1], "avg_wall_ms")

    def test_csv_data_row_has_eleven_fields(self):
        """index.csv の各行が必ず 11 フィールドを持つことの保証 (T024).

        (model / efficiency 列が抜けると csv が壊れる回帰検出用 — DoD §5)。
        """
        with tempfile.TemporaryDirectory() as td:
            p = Path(td) / "index.csv"
            _, csv_row = self._row()
            summarize._append_index(p, csv_row, kind="csv")
            summarize._append_index(p, csv_row, kind="csv")
            for line in p.read_text(encoding="utf-8").splitlines():
                self.assertEqual(line.count(","), 10, msg=f"row: {line!r}")


class TestRenderSummaryModel(unittest.TestCase):
    """T022: render_summary がヘッダに `model:` 行を出力する."""

    def test_model_appears_in_header_when_provided(self):
        md = summarize.render_summary(
            results=[], scenarios=[], claude_version="x", n=0, timestamp="t",
            model="claude-opus-4-7",
        )
        self.assertIn("- model: claude-opus-4-7", md)

    def test_model_default_is_unknown_not_placeholder(self):
        """default は明示的なプレースホルダー文字列ではなく '(unknown)'.

        DoD §2: summary.md ヘッダの model 行はプレースホルダー禁止 — run.sh 経由
        では必ず実値が入る。直接 render_summary() を呼ぶ unit test では default
        '(unknown)' が許容される (他の meta フィールドと挙動を揃える)。
        """
        md = summarize.render_summary(
            results=[], scenarios=[], claude_version="x", n=0, timestamp="t",
        )
        self.assertIn("- model: (unknown)", md)
        # 旧 placeholder が完全に除去されていること
        self.assertNotIn("(claude-code default)", md)


class TestSummarizeCliRequiresModel(unittest.TestCase):
    """T022: CLI の `--model` が required (DoD §5: 必須化 or default の挙動)."""

    def test_argparse_errors_when_model_missing(self):
        """`--model` を渡さないと argparse が SystemExit を投げる."""
        # argparse は SystemExit(2) を投げる
        with self.assertRaises(SystemExit):
            summarize.main([
                "--results-dir", "/nonexistent",
                "--scenarios", "/nonexistent",
                "--out", "/tmp/x.md",
            ])


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


class TestExtractTokenUsage(unittest.TestCase):
    """T024: assistant メッセージの usage を合算する."""

    def _write_jsonl(self, lines: List[str]) -> str:
        f = tempfile.NamedTemporaryFile(
            mode="w", suffix=".jsonl", delete=False, encoding="utf-8"
        )
        for line in lines:
            f.write(line + "\n")
        f.close()
        self.addCleanup(os.unlink, f.name)
        return f.name

    def test_sums_input_and_output_tokens_across_assistants(self):
        path = self._write_jsonl([
            json.dumps({
                "type": "assistant",
                "message": {"role": "assistant", "usage": {
                    "input_tokens": 10, "output_tokens": 5,
                    "cache_creation_input_tokens": 0, "cache_read_input_tokens": 0,
                }},
            }),
            json.dumps({
                "type": "user",
                "message": {"role": "user", "content": "hi"},
            }),
            json.dumps({
                "type": "assistant",
                "message": {"role": "assistant", "usage": {
                    "input_tokens": 3, "output_tokens": 7,
                    "cache_creation_input_tokens": 100, "cache_read_input_tokens": 200,
                }},
            }),
        ])
        usage = summarize.extract_token_usage(path)
        self.assertEqual(usage["input_tokens"], 13)
        self.assertEqual(usage["output_tokens"], 12)
        self.assertEqual(usage["cache_creation_input_tokens"], 100)
        self.assertEqual(usage["cache_read_input_tokens"], 200)

    def test_returns_zero_when_file_missing(self):
        usage = summarize.extract_token_usage("/nonexistent/path/x.jsonl")
        self.assertEqual(usage, {
            "input_tokens": 0,
            "cache_creation_input_tokens": 0,
            "cache_read_input_tokens": 0,
            "output_tokens": 0,
        })

    def test_skips_non_assistant_records(self):
        path = self._write_jsonl([
            json.dumps({"type": "user", "message": {"usage": {"input_tokens": 999}}}),
            json.dumps({"type": "system", "message": {"usage": {"input_tokens": 999}}}),
            json.dumps({
                "type": "assistant",
                "message": {"usage": {"input_tokens": 1, "output_tokens": 2}},
            }),
        ])
        usage = summarize.extract_token_usage(path)
        # user / system の usage は無視される
        self.assertEqual(usage["input_tokens"], 1)
        self.assertEqual(usage["output_tokens"], 2)

    def test_handles_assistant_without_usage(self):
        """usage キーが無い assistant 行は無視される (drift 抑制)."""
        path = self._write_jsonl([
            json.dumps({"type": "assistant", "message": {"content": []}}),
            json.dumps({
                "type": "assistant",
                "message": {"usage": {"input_tokens": 5, "output_tokens": 1}},
            }),
        ])
        usage = summarize.extract_token_usage(path)
        self.assertEqual(usage["input_tokens"], 5)
        self.assertEqual(usage["output_tokens"], 1)

    def test_includes_sidechain_subagent_usage(self):
        """isSidechain=True の usage も課金対象なので合算する.

        (tool_use 抽出側は sidechain を除外するが、tokens は別扱い: T024 設計判断).
        """
        path = self._write_jsonl([
            json.dumps({
                "type": "assistant", "isSidechain": True,
                "message": {"usage": {"input_tokens": 50, "output_tokens": 10}},
            }),
            json.dumps({
                "type": "assistant", "isSidechain": False,
                "message": {"usage": {"input_tokens": 5, "output_tokens": 1}},
            }),
        ])
        usage = summarize.extract_token_usage(path)
        self.assertEqual(usage["input_tokens"], 55)
        self.assertEqual(usage["output_tokens"], 11)


class TestEfficiencyForTrials(unittest.TestCase):
    """T024: _efficiency_for_trials の avg / median 計算."""

    def test_empty_returns_zeros(self):
        eff = summarize._efficiency_for_trials([])
        self.assertEqual(eff["count"], 0)
        self.assertEqual(eff["tool_uses_avg"], 0)
        self.assertEqual(eff["wall_ms_avg"], 0)

    def test_avg_is_round_half_up(self):
        trials = [
            {"tool_uses_count": 3, "wall_seconds": 1,
             "usage": {"input_tokens": 100, "cache_creation_input_tokens": 0,
                       "cache_read_input_tokens": 0, "output_tokens": 50}},
            {"tool_uses_count": 4, "wall_seconds": 2,
             "usage": {"input_tokens": 110, "cache_creation_input_tokens": 0,
                       "cache_read_input_tokens": 0, "output_tokens": 50}},
        ]
        eff = summarize._efficiency_for_trials(trials)
        self.assertEqual(eff["count"], 2)
        # tool_uses_avg = (3+4)/2 = 3.5 → 4 (round-half-up)
        self.assertEqual(eff["tool_uses_avg"], 4)
        self.assertEqual(eff["input_tokens_avg"], 105)
        self.assertEqual(eff["output_tokens_avg"], 50)
        # wall_ms = wall_seconds * 1000
        self.assertEqual(eff["wall_ms_avg"], 1500)

    def test_median_with_odd_count(self):
        trials = [
            {"tool_uses_count": 1, "wall_seconds": 5,
             "usage": {"input_tokens": 0, "cache_creation_input_tokens": 0,
                       "cache_read_input_tokens": 0, "output_tokens": 0}},
            {"tool_uses_count": 2, "wall_seconds": 10,
             "usage": {"input_tokens": 0, "cache_creation_input_tokens": 0,
                       "cache_read_input_tokens": 0, "output_tokens": 0}},
            {"tool_uses_count": 3, "wall_seconds": 100,
             "usage": {"input_tokens": 0, "cache_creation_input_tokens": 0,
                       "cache_read_input_tokens": 0, "output_tokens": 0}},
        ]
        eff = summarize._efficiency_for_trials(trials)
        # median(5,10,100) = 10 → 10000 ms
        self.assertEqual(eff["wall_ms_median"], 10000.0)

    def test_handles_missing_usage(self):
        """usage キーが無い trial 群でも 0 で集計できる (raw データ欠損経路)."""
        trials = [{"tool_uses_count": 2, "wall_seconds": 3}]
        eff = summarize._efficiency_for_trials(trials)
        self.assertEqual(eff["input_tokens_avg"], 0)
        self.assertEqual(eff["tool_uses_avg"], 2)
        self.assertEqual(eff["wall_ms_avg"], 3000)


class TestRenderIndexRowEfficiency(unittest.TestCase):
    """T024: render_index_row が効率指標 4 列を末尾に追加する."""

    def test_efficiency_columns_appended_to_md_row(self):
        md_row, _ = summarize.render_index_row(
            timestamp="ts1", plugin_version="0.2.0",
            claude_version="2.1.122 (Claude Code)", git_sha="abc1234",
            overall_rate=1.0, per_scenario=[("chdir-01", 1.0)],
            model="claude-opus-4-7",
            avg_tool_uses=5,
            avg_input_tokens=12345,
            avg_output_tokens=678,
            avg_wall_ms=12000,
        )
        self.assertIn("| 5 |", md_row)
        self.assertIn("| 12345 |", md_row)
        self.assertIn("| 678 |", md_row)
        self.assertIn("| 12000 |", md_row)

    def test_efficiency_columns_appended_to_csv_row(self):
        _, csv_row = summarize.render_index_row(
            timestamp="ts1", plugin_version="0.2.0",
            claude_version="c", git_sha="s",
            overall_rate=1.0, per_scenario=[("chdir-01", 1.0)],
            model="m",
            avg_tool_uses=5,
            avg_input_tokens=12345,
            avg_output_tokens=678,
            avg_wall_ms=12000,
        )
        fields = csv_row.split(",")
        self.assertEqual(fields[-4], "5")
        self.assertEqual(fields[-3], "12345")
        self.assertEqual(fields[-2], "678")
        self.assertEqual(fields[-1], "12000")

    def test_dash_default_for_missing_efficiency(self):
        """効率指標を渡さないと `-` で埋める (過去 run の遡及で raw データ欠損経路)."""
        md_row, csv_row = summarize.render_index_row(
            timestamp="ts1", plugin_version="0.1.0", claude_version="c", git_sha="s",
            overall_rate=0.0, per_scenario=[("a", 0.0)],
        )
        self.assertIn("| - | - | - | - |", md_row)
        fields = csv_row.split(",")
        self.assertEqual(fields[-4:], ["-", "-", "-", "-"])

    def test_float_efficiency_rounded_to_int(self):
        """avg_input_tokens 等が float (不浮動 avg) でも int に丸めて表示."""
        _, csv_row = summarize.render_index_row(
            timestamp="t", plugin_version="v", claude_version="c", git_sha="s",
            overall_rate=0.5, per_scenario=[("a", 1.0)],
            model="m",
            avg_tool_uses=3.49,
            avg_input_tokens=12345.6,
            avg_output_tokens=0.0,
            avg_wall_ms=999.5,
        )
        fields = csv_row.split(",")
        # 3.49 → 3 (round-half-up)
        self.assertEqual(fields[-4], "3")
        self.assertEqual(fields[-3], "12346")
        self.assertEqual(fields[-2], "0")
        # 999.5 → 1000 (banker's rounding might give 1000)
        self.assertEqual(fields[-1], "1000")


class TestRenderSummaryEfficiencySection(unittest.TestCase):
    """T024: render_summary が efficiency セクションを出力する."""

    def _trial(self, sid: str, trial: int, tokens: int, wall: int, tool_uses: int = 1):
        return {
            "scenario_id": sid,
            "category": "x",
            "trial": trial,
            "verdict": "pass",
            "exit_status": "ok",
            "tool_uses": [{"name": "Bash", "input": {"command": f"ctxd chdir /t/{sid}"}}] * tool_uses,
            "tool_uses_count": tool_uses,
            "usage": {
                "input_tokens": tokens,
                "cache_creation_input_tokens": 0,
                "cache_read_input_tokens": 0,
                "output_tokens": tokens // 2,
            },
            "wall_seconds": wall,
        }

    def test_efficiency_table_appears_when_data_present(self):
        results = [
            self._trial("chdir-01", 1, 1000, 5),
            self._trial("chdir-01", 2, 1100, 6),
        ]
        scenarios = [
            {"id": "chdir-01", "category": "x", "expected_tool": "Bash",
             "expected_args_pattern": "ctxd", "prompt": "p"},
        ]
        md = summarize.render_summary(
            results=results, scenarios=scenarios,
            claude_version="x", n=2, timestamp="t",
            plugin_version="0.2.0", git_sha="s", git_branch="b",
            model="claude-opus-4-7",
        )
        self.assertIn("## efficiency", md)
        self.assertIn("avg_tool_uses", md)
        self.assertIn("avg_input_tokens", md)
        self.assertIn("avg_wall_ms", md)
        # all-trial 集計行が **(all)** で出る
        self.assertIn("(all)", md)

    def test_efficiency_table_omitted_for_legacy_results_without_usage(self):
        """効率指標を持たない results (古い load 経路 / unit test 専用) では表が出ない."""
        results = [
            {"scenario_id": "x", "category": "x", "trial": 1,
             "verdict": "pass", "exit_status": "ok"},
        ]
        scenarios = [
            {"id": "x", "category": "x", "expected_tool": "Bash",
             "expected_args_pattern": "ctxd", "prompt": "p"},
        ]
        md = summarize.render_summary(
            results=results, scenarios=scenarios,
            claude_version="x", n=1, timestamp="t",
        )
        # legacy 経路 (usage / wall_seconds / tool_uses_count いずれも持たない) では
        # efficiency 章が出ない (T024: 後方互換性).
        self.assertNotIn("## efficiency", md)


class TestIndexHeadingHasEfficiencyColumns(unittest.TestCase):
    """T024: 新規 index.{md,csv} を作るとき heading に効率指標 4 列が含まれる."""

    def test_md_heading_includes_efficiency(self):
        with tempfile.TemporaryDirectory() as td:
            p = Path(td) / "index.md"
            md_row, _ = summarize.render_index_row(
                timestamp="t", plugin_version="v", claude_version="c", git_sha="s",
                overall_rate=1.0, per_scenario=[("a", 1.0)],
                model="m",
                avg_tool_uses=1, avg_input_tokens=2,
                avg_output_tokens=3, avg_wall_ms=4,
            )
            summarize._append_index(p, md_row, kind="md")
            content = p.read_text(encoding="utf-8")
            heading_line = next(
                (l for l in content.splitlines() if l.startswith("| timestamp")),
                "",
            )
            self.assertIn("avg_tool_uses", heading_line)
            self.assertIn("avg_input_tokens", heading_line)
            self.assertIn("avg_output_tokens", heading_line)
            self.assertIn("avg_wall_ms", heading_line)


if __name__ == "__main__":
    unittest.main()
