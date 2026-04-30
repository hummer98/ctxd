"""T024: backfill_index.py の単体テスト (one-off スクリプトだが回帰検出用)."""
import json
import os
import sys
import tempfile
import unittest
from pathlib import Path

HERE = Path(__file__).resolve().parent
EVAL_ROOT = HERE.parent

sys.path.insert(0, str(EVAL_ROOT))
import backfill_index  # noqa: E402
import summarize  # noqa: E402


SCENARIOS = [
    {"id": "chdir-01", "category": "chdir", "expected_tool": "Bash",
     "expected_args_pattern": r"ctxd\s+chdir", "prompt": "p", "match_mode": "any-match"},
]


class TestMigrateCsvLine(unittest.TestCase):
    """_migrate_csv_line: 7 列形 → 11 列形."""

    def test_legacy_seven_fields_gets_dash_filled(self):
        line = (
            "20260427-040930,claude-opus-4-7,0.1.0,2.1.119 (Claude Code),7a95621,"
            "0.0%,chdir-01:0.0"
        )
        with tempfile.TemporaryDirectory() as td:
            results_root = Path(td)
            out = backfill_index._migrate_csv_line(line, results_root, SCENARIOS)
        self.assertEqual(out.count(","), 10)
        self.assertTrue(out.endswith(",-,-,-,-"))

    def test_already_eleven_fields_passes_through(self):
        """既に 11 列の行は二重 append されない (冪等性)."""
        line = (
            "20260427-040930,claude-opus-4-7,0.1.0,2.1.119 (Claude Code),7a95621,"
            "0.0%,chdir-01:0.0,5,1234,567,8000"
        )
        with tempfile.TemporaryDirectory() as td:
            out = backfill_index._migrate_csv_line(line, Path(td), SCENARIOS)
        self.assertEqual(out, line)
        self.assertEqual(out.count(","), 10)

    def test_partial_row_with_spaces_in_overall_rate(self):
        """20260428-040044 の `96.8% (partial 31/50)` のように overall_rate に空白を含む.

        per_scenario_rates フィールドにも `;` 区切りで N/A 等が入っているが、
        これは csv 上では空白許容で 1 フィールドのまま (列数は 7 のはず).
        """
        line = (
            "20260428-040044,claude-opus-4-7,0.1.3,2.1.121 (Claude Code),bb40eb2,"
            "96.8% (partial 31/50),"
            "chdir-01:1.0;chdir-02:1.0;git-switch-01:0.9;git-switch-02:1.0(1/1);"
            "env-set-01:N/A(harness-aborted)"
        )
        # 既に 7 fields のはず
        self.assertEqual(line.count(","), 6)
        with tempfile.TemporaryDirectory() as td:
            out = backfill_index._migrate_csv_line(line, Path(td), SCENARIOS)
        # 11 fields に拡張される
        self.assertEqual(out.count(","), 10)


class TestMigrateMdLine(unittest.TestCase):
    """_migrate_md_line: md セル数 7 → 11."""

    def test_legacy_seven_cells_gets_dash_filled(self):
        line = (
            "| 20260427-040930 | claude-opus-4-7 | 0.1.0 | 2.1.119 (Claude Code) | "
            "7a95621 | 0.0% | chdir-01:0.0 |"
        )
        with tempfile.TemporaryDirectory() as td:
            out = backfill_index._migrate_md_line(line, Path(td), SCENARIOS)
        # cells = `|` 数 - 1
        self.assertEqual(out.count("|") - 1, 11)
        self.assertTrue(out.endswith("- | - | - | - |"))

    def test_idempotent_on_eleven_cells(self):
        line = (
            "| ts | m | v | c | s | 0.0% | a:0.0 | 1 | 2 | 3 | 4 |"
        )
        with tempfile.TemporaryDirectory() as td:
            out = backfill_index._migrate_md_line(line, Path(td), SCENARIOS)
        self.assertEqual(out, line)

    def test_separator_and_heading_passthrough(self):
        """`| --- | --- ...` separator と heading 行は _migrate_md_line では触らない.

        (これらは migrate_md (ファイル全体) 側で別ハンドリングされる).
        """
        sep = "| --- | --- | --- | --- | --- | --- | --- |"
        head = "| timestamp | model | plugin_version | claude_version | git_sha | overall_rate | per_scenario_rates |"
        with tempfile.TemporaryDirectory() as td:
            self.assertEqual(
                backfill_index._migrate_md_line(sep, Path(td), SCENARIOS), sep
            )
            self.assertEqual(
                backfill_index._migrate_md_line(head, Path(td), SCENARIOS), head
            )


class TestMigrateFiles(unittest.TestCase):
    """migrate_md / migrate_csv: ファイル全体の冪等性."""

    def setUp(self):
        self.tmpdir = tempfile.mkdtemp()
        self.addCleanup(self._cleanup)

    def _cleanup(self):
        import shutil
        shutil.rmtree(self.tmpdir, ignore_errors=True)

    def _write(self, name: str, content: str) -> Path:
        p = Path(self.tmpdir) / name
        p.write_text(content, encoding="utf-8")
        return p

    def test_csv_idempotent(self):
        original = (
            "timestamp,model,plugin_version,claude_version,git_sha,overall_rate,per_scenario_rates\n"
            "ts1,m,0.1.0,c,s,0.0%,a:0.0\n"
        )
        p = self._write("index.csv", original)
        first = backfill_index.migrate_csv(p, Path(self.tmpdir), SCENARIOS)
        p.write_text(first, encoding="utf-8")
        second = backfill_index.migrate_csv(p, Path(self.tmpdir), SCENARIOS)
        self.assertEqual(first, second)
        # heading に新列が混じる
        self.assertIn("avg_tool_uses", first.splitlines()[0])
        # 各行 11 fields
        for line in first.splitlines()[1:]:
            self.assertEqual(line.count(","), 10)

    def test_md_idempotent(self):
        original = (
            "# Eval Index\n"
            "\n"
            "| timestamp | model | plugin_version | claude_version | git_sha | "
            "overall_rate | per_scenario_rates |\n"
            "| --- | --- | --- | --- | --- | --- | --- |\n"
            "| ts1 | m | 0.1.0 | c | s | 0.0% | a:0.0 |\n"
        )
        p = self._write("index.md", original)
        first = backfill_index.migrate_md(p, Path(self.tmpdir), SCENARIOS)
        p.write_text(first, encoding="utf-8")
        second = backfill_index.migrate_md(p, Path(self.tmpdir), SCENARIOS)
        self.assertEqual(first, second)
        # 新 heading に avg_wall_ms 列
        self.assertIn("avg_wall_ms", first)
        # 既存 data 行は 11 cells
        for line in first.splitlines():
            if line.startswith("| ts1 "):
                self.assertEqual(line.count("|") - 1, 11)


if __name__ == "__main__":
    unittest.main()
