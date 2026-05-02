#!/usr/bin/env python3
"""Extract baseline metrics from .team/traces/traces.db into intermediate JSON.

仕様: docs/measurement-baseline-spec.md §1〜§4
plan: .team/tasks/032-.../runs/.../plan.md §2.2

Exit codes (plan §2.0):
  0 success
  1 arg parse error (argparse handles this)
  2 input file not found (DB missing)
  3 empty window (no tool_calls in [since, until))
  4 unexpected python / sql exec failure
  5 schema mismatch / malformed input data
"""
from __future__ import annotations

import argparse
import json
import os
import sqlite3
import sys
import traceback
from pathlib import Path
from typing import Any

SCHEMA_VERSION = 1
ROLES: tuple[str, ...] = ("agent", "conductor", "master")

JSON_CMD = "json_extract(payload_json, '$.payload.tool_input.command')"


def parse_args(argv: list[str] | None = None) -> argparse.Namespace:
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument("--db", required=True)
    p.add_argument("--phase", required=True, choices=("before", "after"))
    p.add_argument("--since", default=None,
                   help="YYYY-MM-DD inclusive (default: DB MIN(timestamp) date)")
    p.add_argument("--until", default=None,
                   help="YYYY-MM-DD; SQL uses next-day exclusive bound")
    p.add_argument("--git-sha", required=True)
    p.add_argument("--out", required=True)
    return p.parse_args(argv)


def shift_to_next_day(date_str: str) -> str:
    """`2026-05-02` → `2026-05-03`. Cheap arithmetic without datetime imports."""
    from datetime import date, timedelta
    d = date.fromisoformat(date_str)
    return (d + timedelta(days=1)).isoformat()


def compute_window(conn: sqlite3.Connection, since: str | None,
                   until: str | None) -> dict[str, str]:
    """Resolve window bounds. SQL uses [since_iso, until_iso) (until exclusive)."""
    if since is None:
        cur = conn.execute("SELECT MIN(timestamp) FROM hook_signals")
        row = cur.fetchone()
        if not row or not row[0]:
            since_iso = ""
        else:
            since_iso = row[0][:10]
    else:
        since_iso = since

    if until is None:
        cur = conn.execute("SELECT MAX(timestamp) FROM hook_signals")
        row = cur.fetchone()
        if not row or not row[0]:
            until_iso = ""
        else:
            until_iso = shift_to_next_day(row[0][:10])
    else:
        until_iso = shift_to_next_day(until)

    return {"since": since_iso, "until": until_iso}


def query_totals(conn: sqlite3.Connection, since: str, until: str,
                 sessions_by_role: dict[str, int]) -> dict[str, Any]:
    cur = conn.execute(
        "SELECT COUNT(DISTINCT session_id) FROM hook_signals "
        "WHERE type='PRE_TOOL_USE' AND timestamp >= ? AND timestamp < ?",
        (since, until),
    )
    sessions = cur.fetchone()[0] or 0
    cur = conn.execute(
        "SELECT COUNT(DISTINCT task_id) FROM hook_signals "
        "WHERE type='PRE_TOOL_USE' AND task_id IS NOT NULL "
        "AND timestamp >= ? AND timestamp < ?",
        (since, until),
    )
    tasks = cur.fetchone()[0] or 0
    cur = conn.execute(
        "SELECT COUNT(*) FROM hook_signals "
        "WHERE type='PRE_TOOL_USE' AND timestamp >= ? AND timestamp < ?",
        (since, until),
    )
    tool_calls = cur.fetchone()[0] or 0
    cur = conn.execute(
        "SELECT COUNT(*) FROM hook_signals "
        "WHERE type='PRE_TOOL_USE' AND tool_name='Bash' "
        "AND timestamp >= ? AND timestamp < ?",
        (since, until),
    )
    bash_calls = cur.fetchone()[0] or 0
    return {
        "sessions": int(sessions),
        "tasks": int(tasks),
        "tool_calls": int(tool_calls),
        "bash_calls": int(bash_calls),
        "sessions_by_role": dict(sessions_by_role),
    }


def query_sessions_by_role(conn: sqlite3.Connection, since: str,
                           until: str) -> dict[str, int]:
    """spec §2.4 第 2 クエリ. denominator for per_session_rate."""
    cur = conn.execute(
        "SELECT role, COUNT(DISTINCT session_id) FROM hook_signals "
        "WHERE type='PRE_TOOL_USE' AND timestamp >= ? AND timestamp < ? "
        "GROUP BY role",
        (since, until),
    )
    out = {r: 0 for r in ROLES}
    for role, count in cur.fetchall():
        if role in out:
            out[role] = int(count)
        # roles outside (agent/conductor/master) are dropped silently
    return out


def _tier_by_role(conn: sqlite3.Connection, since: str, until: str,
                  where_sql: str) -> dict[str, int]:
    sql = (
        "SELECT role, COUNT(*) FROM hook_signals "
        "WHERE type='PRE_TOOL_USE' AND tool_name='Bash' "
        "AND timestamp >= ? AND timestamp < ? "
        f"AND ({where_sql}) "
        "GROUP BY role"
    )
    cur = conn.execute(sql, (since, until))
    out: dict[str, int] = {r: 0 for r in ROLES}
    total = 0
    for role, count in cur.fetchall():
        c = int(count)
        total += c
        if role in out:
            out[role] = c
    out["total"] = total
    return out


# spec §2.2 / §2.3 — LIKE patterns copied verbatim
TIER1_CD_WHERE = (
    f"{JSON_CMD} LIKE 'cd %' "
    f"OR {JSON_CMD} LIKE 'cd ' "
    f"OR {JSON_CMD} LIKE '% && cd %' "
    f"OR {JSON_CMD} LIKE '% ; cd %' "
    f"OR {JSON_CMD} LIKE 'cd ' || char(10) || '%'"
)
TIER1_GIT_SWITCH_WHERE = (
    f"{JSON_CMD} LIKE 'git checkout%' "
    f"OR {JSON_CMD} LIKE 'git switch%' "
    f"OR {JSON_CMD} LIKE '% && git checkout%' "
    f"OR {JSON_CMD} LIKE '% && git switch%'"
)
TIER1_ENV_WHERE = (
    f"{JSON_CMD} LIKE 'export %' "
    f"OR {JSON_CMD} LIKE 'unset %' "
    f"OR {JSON_CMD} LIKE '% && export %' "
    f"OR {JSON_CMD} LIKE '% && unset %'"
)
TIER2_PWD_WHERE = f"TRIM({JSON_CMD}) = 'pwd'"
TIER2_GIT_STATUS_WHERE = (
    f"{JSON_CMD} LIKE 'git status%' "
    f"OR {JSON_CMD} LIKE 'git branch --show-current%' "
    f"OR {JSON_CMD} LIKE 'git rev-parse --abbrev-ref%'"
)
TIER2_ENV_CHECK_WHERE = (
    f"{JSON_CMD} LIKE 'printenv%' "
    f"OR {JSON_CMD} LIKE 'echo $%' "
    f"OR {JSON_CMD} LIKE 'env | %'"
)


def query_tier1(conn: sqlite3.Connection, since: str,
                until: str) -> dict[str, dict[str, int]]:
    return {
        "cd": _tier_by_role(conn, since, until, TIER1_CD_WHERE),
        "git_switch": _tier_by_role(conn, since, until, TIER1_GIT_SWITCH_WHERE),
        "env": _tier_by_role(conn, since, until, TIER1_ENV_WHERE),
    }


def query_tier2(conn: sqlite3.Connection, since: str,
                until: str) -> dict[str, dict[str, int]]:
    return {
        "pwd": _tier_by_role(conn, since, until, TIER2_PWD_WHERE),
        "git_status": _tier_by_role(conn, since, until, TIER2_GIT_STATUS_WHERE),
        "env_check": _tier_by_role(conn, since, until, TIER2_ENV_CHECK_WHERE),
    }


def query_tier3(conn: sqlite3.Connection, since: str,
                until: str) -> dict[str, list[dict[str, Any]]]:
    cur = conn.execute(
        "SELECT role, COUNT(DISTINCT task_id) AS tasks, "
        "AVG(input_tokens), AVG(output_tokens), "
        "AVG(cache_creation_input_tokens), AVG(cache_read_input_tokens), "
        "AVG(duration_ms) "
        "FROM api_usage "
        "WHERE timestamp >= ? AND timestamp < ? AND task_id IS NOT NULL "
        "GROUP BY role ORDER BY role",
        (since, until),
    )
    by_role: list[dict[str, Any]] = []
    for role, tasks, ai, ao, ac, ar, ad in cur.fetchall():
        by_role.append({
            "role": role,
            "tasks": int(tasks or 0),
            "avg_input": ai,
            "avg_output": ao,
            "avg_cache_create": ac,
            "avg_cache_read": ar,
            "avg_duration_ms": ad,
        })
    cur = conn.execute(
        "SELECT model, role, COUNT(DISTINCT task_id) AS tasks, "
        "AVG(input_tokens), AVG(output_tokens), AVG(duration_ms) "
        "FROM api_usage "
        "WHERE timestamp >= ? AND timestamp < ? AND task_id IS NOT NULL "
        "GROUP BY model, role ORDER BY model, role",
        (since, until),
    )
    by_model_role: list[dict[str, Any]] = []
    for model, role, tasks, ai, ao, ad in cur.fetchall():
        by_model_role.append({
            "model": model,
            "role": role,
            "tasks": int(tasks or 0),
            "avg_input": ai,
            "avg_output": ao,
            "avg_duration_ms": ad,
        })
    return {"by_role": by_role, "by_model_role": by_model_role}


def compute_per_session_rate(tier_counts: dict[str, dict[str, int]],
                             sessions_by_role: dict[str, int],
                             ) -> dict[str, dict[str, float | None]]:
    """plan §2.2 / §9.9 — null fallback when denominator == 0."""
    result: dict[str, dict[str, float | None]] = {}
    for tier_key, role_counts in tier_counts.items():
        per_role: dict[str, float | None] = {}
        for role in ROLES:
            denom = sessions_by_role.get(role, 0)
            if denom == 0:
                per_role[role] = None
            else:
                per_role[role] = role_counts.get(role, 0) / denom
        result[tier_key] = per_role
    return result


def utc_now_iso() -> str:
    """generated_at の生成. SOURCE_DATE_EPOCH を尊重して冪等性を担保。"""
    sde = os.environ.get("SOURCE_DATE_EPOCH")
    from datetime import datetime, timezone
    if sde:
        try:
            ts = int(sde)
            return datetime.fromtimestamp(ts, tz=timezone.utc).strftime(
                "%Y-%m-%dT%H:%M:%SZ")
        except ValueError:
            pass
    return datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")


def build_envelope(args: argparse.Namespace, conn: sqlite3.Connection,
                   ) -> dict[str, Any]:
    window = compute_window(conn, args.since, args.until)
    if not window["since"] or not window["until"]:
        raise EmptyWindowError("DB has no timestamps; cannot resolve window")
    sessions_by_role = query_sessions_by_role(conn, window["since"],
                                              window["until"])
    totals = query_totals(conn, window["since"], window["until"],
                          sessions_by_role)
    if totals["tool_calls"] == 0:
        raise EmptyWindowError(
            f"no tool_calls in window since={window['since']} "
            f"until={window['until']}")
    tier1 = query_tier1(conn, window["since"], window["until"])
    tier2 = query_tier2(conn, window["since"], window["until"])
    tier3 = query_tier3(conn, window["since"], window["until"])
    tier_counts: dict[str, dict[str, int]] = {}
    for k, v in tier1.items():
        tier_counts[f"tier1.{k}"] = v
    for k, v in tier2.items():
        tier_counts[f"tier2.{k}"] = v
    per_session_rate = compute_per_session_rate(tier_counts, sessions_by_role)
    return {
        "schema_version": SCHEMA_VERSION,
        "phase": args.phase,
        "generated_at": utc_now_iso(),
        "git_sha": args.git_sha,
        "window": window,
        "totals": totals,
        "tier1": tier1,
        "tier2": tier2,
        "tier3": tier3,
        "per_session_rate": per_session_rate,
    }


def dump_json(envelope: dict[str, Any], out_path: Path) -> None:
    """Atomic write. sort_keys for byte-stable output."""
    tmp = out_path.with_suffix(out_path.suffix + f".tmp.{os.getpid()}")
    with tmp.open("w", encoding="utf-8") as f:
        json.dump(envelope, f, indent=2, sort_keys=True, ensure_ascii=False)
        f.write("\n")
    os.replace(tmp, out_path)


class EmptyWindowError(Exception):
    pass


class SchemaMismatchError(Exception):
    pass


def main(argv: list[str] | None = None) -> int:
    args = parse_args(argv)
    db_path = Path(args.db)
    if not db_path.exists():
        print(f"DB not found: {db_path}", file=sys.stderr)
        return 2
    out_path = Path(args.out)
    try:
        conn = sqlite3.connect(str(db_path))
        conn.row_factory = sqlite3.Row
        try:
            envelope = build_envelope(args, conn)
        finally:
            conn.close()
    except EmptyWindowError as e:
        print(str(e), file=sys.stderr)
        return 3
    except ValueError as e:
        # invalid date string from shift_to_next_day or similar arg-shape errors
        print(f"invalid argument: {e}", file=sys.stderr)
        return 1
    except sqlite3.OperationalError as e:
        msg = str(e)
        if "no such table" in msg or "no such column" in msg:
            print(
                f"schema mismatch detected — see "
                f"docs/measurement-baseline-spec.md §1.1: {msg}",
                file=sys.stderr)
            return 5
        print(f"sqlite3 error: {msg}", file=sys.stderr)
        return 4
    except sqlite3.Error as e:
        print(f"sqlite3 error: {e}", file=sys.stderr)
        return 4
    except KeyError as e:
        print(f"malformed data: missing key {e}", file=sys.stderr)
        return 5
    except Exception:
        traceback.print_exc()
        return 4
    dump_json(envelope, out_path)
    return 0


if __name__ == "__main__":
    sys.exit(main())
