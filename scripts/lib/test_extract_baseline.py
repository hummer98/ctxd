"""Tests for extract_baseline.py.

Run: python3 -m unittest scripts/lib/test_extract_baseline.py

Plan §6.1 cases:
- a: DB 不在 (shell 側で test、本 file では covered)
- b: 期間内データ空 → exit 3、out ファイル生成しない
- c: Tier 1/2 SQL 集計値が手動 SQL と一致
- d: Tier 3 SQL 集計値が手動 SQL と一致
- e: role ブレイクダウン
- e2: per_session_rate divide-by-zero フォールバック (null)
- f: 冪等性 (shell 側、本 file では covered)
"""
from __future__ import annotations

import json
import os
import sqlite3
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path

SCRIPT = Path(__file__).resolve().parent / "extract_baseline.py"
SCHEMA_HOOK = """
CREATE TABLE hook_signals (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  timestamp TEXT NOT NULL,
  type TEXT NOT NULL,
  surface TEXT,
  pid INTEGER,
  reason TEXT,
  source TEXT,
  question TEXT,
  task_run_id TEXT,
  payload_json TEXT NOT NULL,
  surface_uuid TEXT,
  workspace_uuid TEXT,
  role TEXT,
  task_id TEXT,
  conductor_surface TEXT,
  agent_role TEXT,
  message TEXT,
  notification_type TEXT,
  session_id TEXT,
  tool_name TEXT
);
CREATE TABLE api_usage (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  timestamp TEXT NOT NULL,
  task_id TEXT,
  role TEXT,
  surface TEXT,
  conductor_id TEXT,
  model TEXT,
  request_id TEXT,
  status_code INTEGER,
  input_tokens INTEGER,
  output_tokens INTEGER,
  cache_creation_input_tokens INTEGER,
  cache_read_input_tokens INTEGER,
  stop_reason TEXT,
  duration_ms INTEGER
);
CREATE TABLE task_sessions (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  timestamp TEXT NOT NULL,
  task_id TEXT NOT NULL,
  task_run_id TEXT,
  session_id TEXT NOT NULL,
  role TEXT,
  surface TEXT,
  worktree_path TEXT,
  event TEXT NOT NULL,
  base_branch TEXT,
  base_sha TEXT,
  base_source TEXT
);
"""


def make_db(path: Path) -> sqlite3.Connection:
    conn = sqlite3.connect(str(path))
    conn.executescript(SCHEMA_HOOK)
    conn.commit()
    return conn


def insert_bash(conn: sqlite3.Connection, *, ts: str, role: str, session_id: str,
                command: str, task_id: str | None = None) -> None:
    payload = json.dumps({
        "type": "PRE_TOOL_USE",
        "role": role,
        "sessionId": session_id,
        "toolName": "Bash",
        "payload": {
            "session_id": session_id,
            "tool_name": "Bash",
            "tool_input": {"command": command},
        },
    })
    conn.execute(
        "INSERT INTO hook_signals "
        "(timestamp, type, payload_json, role, session_id, task_id, tool_name) "
        "VALUES (?, 'PRE_TOOL_USE', ?, ?, ?, ?, 'Bash')",
        (ts, payload, role, session_id, task_id),
    )


def insert_other_pre(conn: sqlite3.Connection, *, ts: str, role: str, session_id: str,
                     tool_name: str = "Read") -> None:
    payload = json.dumps({
        "type": "PRE_TOOL_USE",
        "role": role,
        "sessionId": session_id,
        "toolName": tool_name,
        "payload": {"session_id": session_id, "tool_name": tool_name, "tool_input": {}},
    })
    conn.execute(
        "INSERT INTO hook_signals "
        "(timestamp, type, payload_json, role, session_id, tool_name) "
        "VALUES (?, 'PRE_TOOL_USE', ?, ?, ?, ?)",
        (ts, payload, role, session_id, tool_name),
    )


def insert_api_usage(conn: sqlite3.Connection, *, ts: str, role: str, task_id: str,
                     model: str, input_tokens: int, output_tokens: int,
                     cache_creation: int | None, cache_read: int | None,
                     duration_ms: int) -> None:
    conn.execute(
        "INSERT INTO api_usage "
        "(timestamp, task_id, role, model, input_tokens, output_tokens, "
        "cache_creation_input_tokens, cache_read_input_tokens, duration_ms) "
        "VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)",
        (ts, task_id, role, model, input_tokens, output_tokens,
         cache_creation, cache_read, duration_ms),
    )


def run_extract(*, db: Path, out: Path, phase: str = "before",
                since: str | None = None, until: str | None = None,
                git_sha: str = "deadbee",
                source_name: str | None = "ctxd",
                ) -> subprocess.CompletedProcess[str]:
    cmd = [sys.executable, str(SCRIPT),
           "--db", str(db), "--phase", phase,
           "--git-sha", git_sha, "--out", str(out)]
    if source_name is not None:
        cmd += ["--source-name", source_name]
    if since:
        cmd += ["--since", since]
    if until:
        cmd += ["--until", until]
    return subprocess.run(cmd, capture_output=True, text=True)


class TestEmptyWindow(unittest.TestCase):
    """plan §6.1.b — 期間内データ空 → exit 3、out ファイル不生成"""

    def test_empty_window_exits_3_and_no_output(self) -> None:
        with tempfile.TemporaryDirectory() as td:
            db = Path(td) / "empty.db"
            conn = make_db(db)
            conn.commit()
            conn.close()
            out = Path(td) / "out.json"
            res = run_extract(db=db, out=out,
                              since="2099-01-01", until="2099-01-02")
            self.assertEqual(res.returncode, 3, msg=res.stderr)
            self.assertFalse(out.exists(),
                             msg="out file must not be created on empty window")


class TestTier1Tier2Counts(unittest.TestCase):
    """plan §6.1.c — Tier 1/2 SQL 集計値"""

    def test_tier1_and_tier2_counts(self) -> None:
        with tempfile.TemporaryDirectory() as td:
            db = Path(td) / "fix.db"
            conn = make_db(db)
            ts = "2026-04-26T00:00:00.000Z"
            # Tier 1.1 cd: 3 (agent: 2, conductor: 1)
            insert_bash(conn, ts=ts, role="agent", session_id="s1",
                        command="cd /tmp")
            insert_bash(conn, ts=ts, role="agent", session_id="s2",
                        command="cd /var && ls")  # compound -> still cd 1
            insert_bash(conn, ts=ts, role="conductor", session_id="s3",
                        command="echo a && cd /etc")  # '% && cd %'
            # Tier 1.2 git_switch: 2 (agent: 1, conductor: 1)
            insert_bash(conn, ts=ts, role="agent", session_id="s1",
                        command="git switch main")
            insert_bash(conn, ts=ts, role="conductor", session_id="s3",
                        command="git checkout -b foo")
            # Tier 1.3 env: 1 (agent: 1)
            insert_bash(conn, ts=ts, role="agent", session_id="s1",
                        command="export FOO=bar")
            # Tier 2.1 pwd: 1 (master: 1)
            insert_bash(conn, ts=ts, role="master", session_id="s4",
                        command="pwd")
            # Tier 2.2 git_status: 2 (agent: 1, master: 1)
            insert_bash(conn, ts=ts, role="agent", session_id="s1",
                        command="git status -s")
            insert_bash(conn, ts=ts, role="master", session_id="s4",
                        command="git rev-parse --abbrev-ref HEAD")
            # Tier 2.3 env_check: 1 (master: 1)
            insert_bash(conn, ts=ts, role="master", session_id="s4",
                        command="printenv PATH")
            conn.commit()
            conn.close()
            out = Path(td) / "out.json"
            res = run_extract(db=db, out=out,
                              since="2026-04-25", until="2026-04-30")
            self.assertEqual(res.returncode, 0, msg=res.stderr)
            data = json.loads(out.read_text())
            self.assertEqual(data["tier1"]["cd"]["total"], 3)
            self.assertEqual(data["tier1"]["cd"]["agent"], 2)
            self.assertEqual(data["tier1"]["cd"]["conductor"], 1)
            self.assertEqual(data["tier1"]["cd"]["master"], 0)
            self.assertEqual(data["tier1"]["git_switch"]["total"], 2)
            self.assertEqual(data["tier1"]["env"]["total"], 1)
            self.assertEqual(data["tier2"]["pwd"]["total"], 1)
            self.assertEqual(data["tier2"]["pwd"]["master"], 1)
            self.assertEqual(data["tier2"]["git_status"]["total"], 2)
            self.assertEqual(data["tier2"]["env_check"]["total"], 1)


class TestTier3Avgs(unittest.TestCase):
    """plan §6.1.d — Tier 3 avg 値"""

    def test_tier3_by_role_and_by_model_role(self) -> None:
        with tempfile.TemporaryDirectory() as td:
            db = Path(td) / "fix.db"
            conn = make_db(db)
            ts = "2026-04-26T00:00:00.000Z"
            insert_api_usage(conn, ts=ts, role="agent", task_id="t1",
                             model="claude-opus-4-7",
                             input_tokens=100, output_tokens=10,
                             cache_creation=20, cache_read=5,
                             duration_ms=1000)
            insert_api_usage(conn, ts=ts, role="agent", task_id="t1",
                             model="claude-opus-4-7",
                             input_tokens=200, output_tokens=20,
                             cache_creation=40, cache_read=15,
                             duration_ms=2000)
            insert_api_usage(conn, ts=ts, role="conductor", task_id="t2",
                             model="claude-opus-4-7",
                             input_tokens=300, output_tokens=30,
                             cache_creation=None, cache_read=None,
                             duration_ms=3000)
            conn.commit()
            conn.close()
            out = Path(td) / "out.json"
            # Need at least 1 hook signal so totals.tool_calls > 0
            conn = sqlite3.connect(str(db))
            insert_bash(conn, ts=ts, role="agent", session_id="s1",
                        command="cd /tmp")
            conn.commit()
            conn.close()
            res = run_extract(db=db, out=out,
                              since="2026-04-25", until="2026-04-30")
            self.assertEqual(res.returncode, 0, msg=res.stderr)
            data = json.loads(out.read_text())
            agent_row = next(r for r in data["tier3"]["by_role"]
                             if r["role"] == "agent")
            self.assertEqual(agent_row["tasks"], 1)
            self.assertEqual(agent_row["avg_input"], 150.0)
            self.assertEqual(agent_row["avg_output"], 15.0)
            self.assertEqual(agent_row["avg_duration_ms"], 1500.0)
            cond_row = next(r for r in data["tier3"]["by_role"]
                            if r["role"] == "conductor")
            self.assertEqual(cond_row["tasks"], 1)
            self.assertEqual(cond_row["avg_input"], 300.0)
            self.assertIsNone(cond_row["avg_cache_create"])  # all NULL
            # by_model_role
            self.assertTrue(any(
                r["model"] == "claude-opus-4-7" and r["role"] == "agent"
                for r in data["tier3"]["by_model_role"]))


class TestPerSessionRateNull(unittest.TestCase):
    """plan §6.1.e2 — divide-by-zero → null"""

    def test_master_zero_sessions_yields_null(self) -> None:
        with tempfile.TemporaryDirectory() as td:
            db = Path(td) / "fix.db"
            conn = make_db(db)
            ts = "2026-04-26T00:00:00.000Z"
            # 5 distinct agent sessions, 2 conductor, 0 master (PRE_TOOL_USE rows)
            for i in range(5):
                insert_other_pre(conn, ts=ts, role="agent",
                                 session_id=f"a{i}")
            for i in range(2):
                insert_other_pre(conn, ts=ts, role="conductor",
                                 session_id=f"c{i}")
            # cd: agent 3, conductor 1, master 0
            for i in range(3):
                insert_bash(conn, ts=ts, role="agent",
                            session_id=f"a{i}", command="cd /tmp")
            insert_bash(conn, ts=ts, role="conductor",
                        session_id="c0", command="cd /var")
            conn.commit()
            conn.close()
            out = Path(td) / "out.json"
            res = run_extract(db=db, out=out,
                              since="2026-04-25", until="2026-04-30")
            self.assertEqual(res.returncode, 0, msg=res.stderr)
            data = json.loads(out.read_text())
            self.assertEqual(data["totals"]["sessions_by_role"]["agent"], 5)
            self.assertEqual(data["totals"]["sessions_by_role"]["conductor"], 2)
            self.assertEqual(data["totals"]["sessions_by_role"]["master"], 0)
            self.assertAlmostEqual(
                data["per_session_rate"]["tier1.cd"]["agent"], 3 / 5)
            self.assertAlmostEqual(
                data["per_session_rate"]["tier1.cd"]["conductor"], 1 / 2)
            self.assertIsNone(data["per_session_rate"]["tier1.cd"]["master"])


class TestSourceName(unittest.TestCase):
    """T034 — --source-name field is recorded in JSON envelope."""

    def _make_minimal_db(self, db: Path) -> None:
        conn = make_db(db)
        ts = "2026-04-26T00:00:00.000Z"
        insert_bash(conn, ts=ts, role="agent", session_id="s1",
                    command="cd /tmp")
        conn.commit()
        conn.close()

    def test_source_name_in_json(self) -> None:
        with tempfile.TemporaryDirectory() as td:
            db = Path(td) / "fix.db"
            self._make_minimal_db(db)
            out = Path(td) / "out.json"
            res = run_extract(db=db, out=out,
                              since="2026-04-25", until="2026-04-30",
                              source_name="foobar")
            self.assertEqual(res.returncode, 0, msg=res.stderr)
            data = json.loads(out.read_text())
            self.assertEqual(data["source_name"], "foobar")

    def test_source_name_default_ctxd(self) -> None:
        with tempfile.TemporaryDirectory() as td:
            db = Path(td) / "fix.db"
            self._make_minimal_db(db)
            out = Path(td) / "out.json"
            res = run_extract(db=db, out=out,
                              since="2026-04-25", until="2026-04-30")
            self.assertEqual(res.returncode, 0, msg=res.stderr)
            data = json.loads(out.read_text())
            self.assertEqual(data["source_name"], "ctxd")

    def test_source_name_missing_argparse_error(self) -> None:
        with tempfile.TemporaryDirectory() as td:
            db = Path(td) / "fix.db"
            self._make_minimal_db(db)
            out = Path(td) / "out.json"
            res = run_extract(db=db, out=out,
                              since="2026-04-25", until="2026-04-30",
                              source_name=None)
            self.assertNotEqual(res.returncode, 0, msg=res.stderr)
            self.assertFalse(out.exists())


class TestSinceDefault(unittest.TestCase):
    """spec §1.2 — `--since` 未指定時は DB MIN(timestamp) が since になる"""

    def test_since_defaults_to_db_min(self) -> None:
        with tempfile.TemporaryDirectory() as td:
            db = Path(td) / "fix.db"
            conn = make_db(db)
            insert_bash(conn, ts="2026-04-25T10:00:00.000Z",
                        role="agent", session_id="s1", command="cd /a")
            insert_bash(conn, ts="2026-04-28T10:00:00.000Z",
                        role="agent", session_id="s2", command="cd /b")
            insert_bash(conn, ts="2026-05-02T10:00:00.000Z",
                        role="agent", session_id="s3", command="cd /c")
            conn.commit()
            conn.close()
            out = Path(td) / "out.json"
            res = run_extract(db=db, out=out)
            self.assertEqual(res.returncode, 0, msg=res.stderr)
            data = json.loads(out.read_text())
            self.assertEqual(data["window"]["since"], "2026-04-25",
                             msg="spec §1.2: --since default = DB MIN(timestamp)")
            self.assertEqual(data["window"]["until"], "2026-05-03",
                             msg="--until default = MAX(timestamp) + 1 day")
            self.assertEqual(data["totals"]["tool_calls"], 3)
            self.assertEqual(data["tier1"]["cd"]["total"], 3)

    def test_since_default_with_until_specified(self) -> None:
        """spec §1.2 — `--until` 指定 + `--since` 未指定で since は DB MIN"""
        with tempfile.TemporaryDirectory() as td:
            db = Path(td) / "fix.db"
            conn = make_db(db)
            insert_bash(conn, ts="2026-04-25T10:00:00.000Z",
                        role="agent", session_id="s1", command="cd /a")
            insert_bash(conn, ts="2026-04-28T10:00:00.000Z",
                        role="agent", session_id="s2", command="cd /b")
            insert_bash(conn, ts="2026-05-02T10:00:00.000Z",
                        role="agent", session_id="s3", command="cd /c")
            conn.commit()
            conn.close()
            out = Path(td) / "out.json"
            res = run_extract(db=db, out=out, until="2026-04-30")
            self.assertEqual(res.returncode, 0, msg=res.stderr)
            data = json.loads(out.read_text())
            self.assertEqual(data["window"]["since"], "2026-04-25")
            self.assertEqual(data["window"]["until"], "2026-05-01")
            self.assertEqual(data["totals"]["tool_calls"], 2)


class TestSchemaMismatch(unittest.TestCase):
    """plan §2.0 / §9.1 — schema mismatch → exit 5"""

    def test_missing_tool_name_column(self) -> None:
        with tempfile.TemporaryDirectory() as td:
            db = Path(td) / "old.db"
            conn = sqlite3.connect(str(db))
            conn.executescript("""
                CREATE TABLE hook_signals (
                  id INTEGER PRIMARY KEY AUTOINCREMENT,
                  timestamp TEXT NOT NULL,
                  type TEXT NOT NULL,
                  payload_json TEXT NOT NULL,
                  role TEXT
                );
                CREATE TABLE api_usage (
                  id INTEGER PRIMARY KEY AUTOINCREMENT,
                  timestamp TEXT NOT NULL,
                  role TEXT
                );
            """)
            conn.commit()
            conn.close()
            out = Path(td) / "out.json"
            res = run_extract(db=db, out=out,
                              since="2026-04-25", until="2026-04-30")
            self.assertEqual(res.returncode, 5, msg=res.stderr)


if __name__ == "__main__":
    unittest.main()
