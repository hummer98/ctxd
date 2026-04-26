#!/usr/bin/env python3
"""evals/summarize.py — JSONL の trial 群を読んで summary.md を生成する.

stdlib のみ (json, re, pathlib, argparse, sys, collections, datetime).
plan.md §6 の判定規則に従う:

- exit_status が ok 以外 → error (計測失敗)
- exit_status == ok だが tool_use 0 件 → fail (指示遵守の弱さ)
- exit_status == ok かつ tool_use あり、name + pattern が match_mode に従って一致 → pass
"""
from __future__ import annotations

import argparse
import json
import re
import sys
from collections import OrderedDict, defaultdict
from datetime import datetime, timezone
from pathlib import Path
from typing import Any, Dict, Iterable, List, Optional

ERROR_EXIT_STATUSES = {
    "timeout",
    "jsonl_missing",
    "claude_crashed",
    "ready_timeout",
    "new_workspace_parse_failed",
}

REQUIRED_SCENARIO_KEYS = ("id", "prompt", "expected_tool", "expected_args_pattern")
ID_PATTERN = re.compile(r"^[a-zA-Z0-9_-]+$")


# --------------------------- JSONL 抽出 ---------------------------

def extract_tool_uses(jsonl_path: Path | str) -> List[Dict[str, Any]]:
    """assistant の tool_use ブロックを順序保持で抽出する (plan §6.2)."""
    tool_uses: List[Dict[str, Any]] = []
    p = Path(jsonl_path)
    if not p.exists() or p.stat().st_size == 0:
        return tool_uses
    with p.open("r", encoding="utf-8") as f:
        for raw in f:
            raw = raw.strip()
            if not raw:
                continue
            try:
                obj = json.loads(raw)
            except json.JSONDecodeError:
                continue
            if not isinstance(obj, dict):
                continue
            if obj.get("type") != "assistant":
                continue
            if obj.get("isSidechain") is True:
                continue
            content = obj.get("message", {}).get("content", [])
            if not isinstance(content, list):
                continue
            for block in content:
                if isinstance(block, dict) and block.get("type") == "tool_use":
                    tool_uses.append(
                        {
                            "name": block.get("name"),
                            "input": block.get("input", {}) or {},
                        }
                    )
    return tool_uses


# --------------------------- 判定 ---------------------------

def match(
    scenario: Dict[str, Any],
    tool_uses: List[Dict[str, Any]],
    exit_status: str,
) -> str:
    """plan §6.3 の判定規則. 戻り値: "pass" | "fail" | "error"."""
    if exit_status in ERROR_EXIT_STATUSES:
        return "error"
    if exit_status != "ok":
        return "error"
    if not tool_uses:
        return "fail"
    pattern = re.compile(scenario["expected_args_pattern"])
    expected_tool = scenario["expected_tool"]
    mode = scenario.get("match_mode", "any")
    targets = tool_uses[:1] if mode == "first" else tool_uses
    for t in targets:
        if t.get("name") != expected_tool:
            continue
        inp = t.get("input") or {}
        if expected_tool == "Bash":
            cmd = inp.get("command")
        else:
            cmd = json.dumps(inp, ensure_ascii=False)
        if cmd and pattern.search(cmd):
            return "pass"
    return "fail"


# --------------------------- scenarios 読み込み ---------------------------

def load_scenarios(path: Path | str) -> List[Dict[str, Any]]:
    """scenarios.jsonl を読み、必須キー欠損 / id 重複 / regex 構文エラーで fail-fast.

    Implementer 注: bad scenario が 1 つでもあれば runner 全体を止めたいので
    ValueError を raise する (CLI でキャッチして SystemExit に変換).
    """
    p = Path(path)
    if not p.exists():
        raise ValueError(f"scenarios file not found: {path}")
    scenarios: List[Dict[str, Any]] = []
    seen_ids: Dict[str, int] = {}
    with p.open("r", encoding="utf-8") as f:
        for lineno, raw in enumerate(f, start=1):
            stripped = raw.strip()
            if not stripped or stripped.startswith("#"):
                continue
            try:
                obj = json.loads(stripped)
            except json.JSONDecodeError as e:
                raise ValueError(f"line {lineno}: invalid JSON ({e})") from e
            if not isinstance(obj, dict):
                raise ValueError(f"line {lineno}: scenario is not an object")
            for key in REQUIRED_SCENARIO_KEYS:
                if key not in obj or obj[key] in (None, ""):
                    raise ValueError(
                        f"line {lineno}: missing required key '{key}'"
                    )
            sid = obj["id"]
            if not isinstance(sid, str) or not ID_PATTERN.match(sid):
                raise ValueError(
                    f"line {lineno}: id '{sid}' does not match {ID_PATTERN.pattern}"
                )
            if sid in seen_ids:
                raise ValueError(
                    f"line {lineno}: duplicate id '{sid}' (also at line {seen_ids[sid]})"
                )
            seen_ids[sid] = lineno
            mode = obj.get("match_mode", "any")
            if mode not in ("any", "first"):
                raise ValueError(
                    f"line {lineno}: match_mode must be 'any' or 'first', got '{mode}'"
                )
            try:
                re.compile(obj["expected_args_pattern"])
            except re.error as e:
                raise ValueError(
                    f"line {lineno}: invalid expected_args_pattern regex ({e})"
                ) from e
            obj.setdefault("match_mode", "any")
            scenarios.append(obj)
    return scenarios


# --------------------------- 結果収集 ---------------------------

def load_results(results_dir: Path | str, scenarios: List[Dict[str, Any]]) -> List[Dict[str, Any]]:
    """results_dir 配下の session-*.meta.json + JSONL を 1 trial = 1 dict にまとめる."""
    rd = Path(results_dir)
    by_id = {s["id"]: s for s in scenarios}
    results: List[Dict[str, Any]] = []
    for meta_path in sorted(rd.glob("session-*.meta.json")):
        try:
            meta = json.loads(meta_path.read_text(encoding="utf-8"))
        except (OSError, json.JSONDecodeError):
            continue
        sid = meta.get("scenario_id")
        trial = meta.get("trial")
        scenario = by_id.get(sid)
        if scenario is None:
            continue
        jsonl_path = meta_path.with_suffix("")  # .meta を剥がす
        # session-*.meta.json → session-*.jsonl
        jsonl_path = jsonl_path.with_suffix(".jsonl")
        if not jsonl_path.name.endswith(".jsonl"):
            jsonl_path = Path(str(meta_path).replace(".meta.json", ".jsonl"))
        tool_uses = extract_tool_uses(jsonl_path)
        exit_status = meta.get("exit_status", "ok")
        verdict = match(scenario, tool_uses, exit_status)
        results.append(
            {
                "scenario_id": sid,
                "category": scenario.get("category", ""),
                "trial": trial,
                "exit_status": exit_status,
                "verdict": verdict,
                "tool_uses": tool_uses,
                "meta": meta,
                "jsonl_path": str(jsonl_path),
            }
        )
    return results


# --------------------------- summary.md レンダリング ---------------------------

def render_summary(
    results: List[Dict[str, Any]],
    scenarios: List[Dict[str, Any]],
    claude_version: str,
    n: int,
    timestamp: str,
    model: str = "(claude-code default)",
) -> str:
    total = len(results)
    pass_count = sum(1 for r in results if r["verdict"] == "pass")
    fail_count = sum(1 for r in results if r["verdict"] == "fail")
    error_count = sum(1 for r in results if r["verdict"] == "error")
    overall_rate = (pass_count / total * 100.0) if total else 0.0

    lines: List[str] = []
    lines.append(f"# Eval Summary {timestamp}")
    lines.append("")
    lines.append("- harness: claude-code via cmux")
    lines.append(f"- claude version: {claude_version}")
    lines.append(f"- model: {model}")
    lines.append(f"- N (trials per scenario): {n}")
    lines.append(f"- total trials: {total}")
    lines.append(
        f"- overall success rate: {pass_count} / {total} ({overall_rate:.1f}%)"
    )
    lines.append(f"- fail: {fail_count}, error: {error_count}")
    lines.append("")

    # per-scenario テーブル
    lines.append("## per-scenario")
    lines.append("")
    lines.append("| id | category | trials | pass | fail | error | success rate |")
    lines.append("|---|---|---|---|---|---|---|")
    by_scenario: "OrderedDict[str, List[Dict[str, Any]]]" = OrderedDict()
    for s in scenarios:
        by_scenario[s["id"]] = []
    for r in results:
        by_scenario.setdefault(r["scenario_id"], []).append(r)
    for sid, rs in by_scenario.items():
        scen = next((s for s in scenarios if s["id"] == sid), {})
        cat = scen.get("category", "")
        trials = len(rs)
        p = sum(1 for r in rs if r["verdict"] == "pass")
        f = sum(1 for r in rs if r["verdict"] == "fail")
        e = sum(1 for r in rs if r["verdict"] == "error")
        rate = (p / trials * 100.0) if trials else 0.0
        lines.append(
            f"| {sid} | {cat} | {trials} | {p} | {f} | {e} | {rate:.1f}% |"
        )
    lines.append("")

    # fail / error の最初の 1 例を貼る (plan §6.4)
    examples: List[str] = []
    seen: set = set()
    for r in results:
        if r["verdict"] == "pass":
            continue
        key = (r["scenario_id"], r["verdict"])
        if key in seen:
            continue
        seen.add(key)
        snippet = _format_example(r)
        examples.append(
            f"- **{r['scenario_id']}** trial {r['trial']} → {r['verdict']} "
            f"(exit_status={r['exit_status']})\n  {snippet}"
        )
    if examples:
        lines.append("## fail / error examples")
        lines.append("")
        lines.extend(examples)
        lines.append("")

    return "\n".join(lines).rstrip() + "\n"


def _format_example(result: Dict[str, Any]) -> str:
    if result["verdict"] == "error":
        meta = result.get("meta", {})
        return f"`exit_status={meta.get('exit_status')}` detail={meta.get('detail', '')}"
    tool_uses = result.get("tool_uses") or []
    if not tool_uses:
        # assistant の最終 message 冒頭 200 文字を引用
        text = _last_assistant_text(result.get("jsonl_path", ""))
        return f"no tool_use; assistant text: {text[:200]!r}"
    t = tool_uses[0]
    inp = t.get("input") or {}
    if t.get("name") == "Bash":
        return f"first tool_use: Bash command={inp.get('command')!r}"
    return f"first tool_use: {t.get('name')} input={json.dumps(inp, ensure_ascii=False)[:200]}"


def _last_assistant_text(jsonl_path: str) -> str:
    p = Path(jsonl_path)
    if not p.exists():
        return ""
    text = ""
    try:
        with p.open("r", encoding="utf-8") as f:
            for raw in f:
                raw = raw.strip()
                if not raw:
                    continue
                try:
                    obj = json.loads(raw)
                except json.JSONDecodeError:
                    continue
                if obj.get("type") != "assistant":
                    continue
                content = obj.get("message", {}).get("content", [])
                if not isinstance(content, list):
                    continue
                for block in content:
                    if isinstance(block, dict) and block.get("type") == "text":
                        text = block.get("text", "") or text
    except OSError:
        return ""
    return text


# --------------------------- CLI ---------------------------

def main(argv: Optional[List[str]] = None) -> int:
    parser = argparse.ArgumentParser(description="evals summary generator")
    parser.add_argument("--results-dir", required=True)
    parser.add_argument("--scenarios", required=True)
    parser.add_argument("--claude-version", default="(unknown)")
    parser.add_argument("--out", required=True)
    parser.add_argument("--model", default="(claude-code default)")
    parser.add_argument("--n", type=int, default=0,
                        help="trials per scenario (used in header only)")
    args = parser.parse_args(argv)

    try:
        scenarios = load_scenarios(args.scenarios)
    except ValueError as e:
        print(f"summarize: scenarios validation failed: {e}", file=sys.stderr)
        return 2

    results = load_results(args.results_dir, scenarios)
    n = args.n
    if n == 0 and scenarios:
        # scenario あたり trial 数を逆算
        per: Dict[str, int] = defaultdict(int)
        for r in results:
            per[r["scenario_id"]] += 1
        if per:
            n = max(per.values())

    timestamp = datetime.now(timezone.utc).strftime("%Y-%m-%d %H:%M:%S UTC")
    md = render_summary(
        results=results,
        scenarios=scenarios,
        claude_version=args.claude_version,
        n=n,
        timestamp=timestamp,
        model=args.model,
    )
    out = Path(args.out)
    out.parent.mkdir(parents=True, exist_ok=True)
    out.write_text(md, encoding="utf-8")
    print(f"summary written: {out}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
