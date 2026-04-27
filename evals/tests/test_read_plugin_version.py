"""evals/lib/read_plugin_version.py の単体テスト (stdlib only).

plan §3 Step 2.1 / §7 のテスト戦略:
- 正常系: 一時 dir に valid plugin.json を置いて version を返す
- 異常系: ファイルなし / JSON parse error / version キー欠損 → ValueError
"""
from __future__ import annotations

import json
import sys
import tempfile
import unittest
from pathlib import Path

HERE = Path(__file__).resolve().parent
EVAL_ROOT = HERE.parent

sys.path.insert(0, str(EVAL_ROOT))
from lib.read_plugin_version import read_plugin_version  # noqa: E402


def _write_manifest(repo_root: Path, content: str) -> Path:
    meta_dir = repo_root / ".claude-plugin"
    meta_dir.mkdir(parents=True, exist_ok=True)
    p = meta_dir / "plugin.json"
    p.write_text(content, encoding="utf-8")
    return p


class TestReadPluginVersionHappy(unittest.TestCase):
    def test_returns_version_string(self):
        with tempfile.TemporaryDirectory() as td:
            root = Path(td)
            _write_manifest(root, json.dumps({"name": "ctxd", "version": "0.1.0"}))
            self.assertEqual(read_plugin_version(root), "0.1.0")

    def test_accepts_str_path(self):
        with tempfile.TemporaryDirectory() as td:
            _write_manifest(Path(td), json.dumps({"name": "x", "version": "1.2.3"}))
            self.assertEqual(read_plugin_version(td), "1.2.3")


class TestReadPluginVersionErrors(unittest.TestCase):
    def test_missing_file_raises_value_error(self):
        with tempfile.TemporaryDirectory() as td:
            with self.assertRaises(ValueError) as cm:
                read_plugin_version(td)
            self.assertIn("plugin manifest not found", str(cm.exception))

    def test_invalid_json_raises_value_error(self):
        with tempfile.TemporaryDirectory() as td:
            _write_manifest(Path(td), "{not valid json")
            with self.assertRaises(ValueError) as cm:
                read_plugin_version(td)
            self.assertIn("invalid JSON", str(cm.exception))

    def test_missing_version_key_raises_value_error(self):
        with tempfile.TemporaryDirectory() as td:
            _write_manifest(Path(td), json.dumps({"name": "ctxd"}))
            with self.assertRaises(ValueError) as cm:
                read_plugin_version(td)
            self.assertIn("missing or empty 'version'", str(cm.exception))

    def test_empty_version_string_raises_value_error(self):
        with tempfile.TemporaryDirectory() as td:
            _write_manifest(Path(td), json.dumps({"version": ""}))
            with self.assertRaises(ValueError):
                read_plugin_version(td)

    def test_non_string_version_raises_value_error(self):
        with tempfile.TemporaryDirectory() as td:
            _write_manifest(Path(td), json.dumps({"version": 123}))
            with self.assertRaises(ValueError):
                read_plugin_version(td)


if __name__ == "__main__":
    unittest.main()
