"""evals/lib/read_plugin_meta.py の単体テスト (stdlib only).

T018: dot-path で plugin.json の任意キーを取り出せること、欠損 / parse error の
取扱いが read_plugin_version.py と整合することを検証。
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
from lib.read_plugin_meta import read_plugin_meta  # noqa: E402


def _write_manifest(repo_root: Path, content: str) -> Path:
    meta_dir = repo_root / ".claude-plugin"
    meta_dir.mkdir(parents=True, exist_ok=True)
    p = meta_dir / "plugin.json"
    p.write_text(content, encoding="utf-8")
    return p


class TestReadPluginMetaHappy(unittest.TestCase):
    def test_returns_top_level_string(self):
        with tempfile.TemporaryDirectory() as td:
            root = Path(td)
            _write_manifest(root, json.dumps({"name": "ctxd", "version": "0.1.3"}))
            self.assertEqual(read_plugin_meta(root, "name"), "ctxd")
            self.assertEqual(read_plugin_meta(root, "version"), "0.1.3")

    def test_returns_nested_string_via_dot_path(self):
        with tempfile.TemporaryDirectory() as td:
            root = Path(td)
            _write_manifest(
                root,
                json.dumps(
                    {
                        "author": {
                            "name": "Yuji Yamamoto",
                            "email": "yuji.yamamoto@tayorie.jp",
                        }
                    }
                ),
            )
            self.assertEqual(read_plugin_meta(root, "author.name"), "Yuji Yamamoto")
            self.assertEqual(
                read_plugin_meta(root, "author.email"), "yuji.yamamoto@tayorie.jp"
            )


class TestReadPluginMetaMissing(unittest.TestCase):
    def test_missing_top_level_key_returns_empty(self):
        with tempfile.TemporaryDirectory() as td:
            _write_manifest(Path(td), json.dumps({"name": "ctxd"}))
            self.assertEqual(read_plugin_meta(td, "author"), "")

    def test_missing_nested_key_returns_empty(self):
        with tempfile.TemporaryDirectory() as td:
            _write_manifest(Path(td), json.dumps({"author": {"name": "x"}}))
            self.assertEqual(read_plugin_meta(td, "author.email"), "")

    def test_intermediate_not_dict_returns_empty(self):
        """中間が string の場合 (e.g. author が string 形式) は空文字を返す."""
        with tempfile.TemporaryDirectory() as td:
            _write_manifest(Path(td), json.dumps({"author": "Yuji <yuji@example>"}))
            self.assertEqual(read_plugin_meta(td, "author.name"), "")


class TestReadPluginMetaErrors(unittest.TestCase):
    def test_missing_file_raises_value_error(self):
        with tempfile.TemporaryDirectory() as td:
            with self.assertRaises(ValueError) as cm:
                read_plugin_meta(td, "name")
            self.assertIn("plugin manifest not found", str(cm.exception))

    def test_invalid_json_raises_value_error(self):
        with tempfile.TemporaryDirectory() as td:
            _write_manifest(Path(td), "{not valid json")
            with self.assertRaises(ValueError) as cm:
                read_plugin_meta(td, "name")
            self.assertIn("invalid JSON", str(cm.exception))


if __name__ == "__main__":
    unittest.main()
