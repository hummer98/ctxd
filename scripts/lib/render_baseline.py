#!/usr/bin/env python3
"""Render baseline intermediate JSON to a static HTML report.

仕様: docs/measurement-baseline-spec.md §3.2 (Header / Tier 1 / Tier 2 / Tier 3 / Footer)
plan: .team/tasks/032-.../runs/.../plan.md §2.3

External JS forbidden (Chart.js, CDN, etc). CSS-only bars + tables only.
Raw `tool_input.command` values are NOT included (ADR 0002 Open Q3).

Exit codes (plan §2.0):
  0 success
  1 arg parse error
  2 input data JSON not found
  4 unexpected python failure
  5 schema mismatch / malformed data
"""
from __future__ import annotations

import argparse
import html
import json
import os
import sys
import traceback
from pathlib import Path
from typing import Any

ROLES: tuple[str, ...] = ("agent", "conductor", "master")
SCHEMA_VERSION = 1


def parse_args(argv: list[str] | None = None) -> argparse.Namespace:
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument("--data", required=True)
    p.add_argument("--out", required=True)
    return p.parse_args(argv)


def load_data(path: Path) -> dict[str, Any]:
    with path.open(encoding="utf-8") as f:
        data = json.load(f)
    if data.get("schema_version") != SCHEMA_VERSION:
        raise ValueError(
            f"expected schema_version={SCHEMA_VERSION} "
            f"got {data.get('schema_version')!r}")
    for key in ("source_name", "phase", "generated_at", "git_sha", "window",
                "totals", "tier1", "tier2", "tier3", "per_session_rate"):
        if key not in data:
            raise KeyError(key)
    return data


def esc(s: Any) -> str:
    return html.escape("" if s is None else str(s))


def fmt_num(v: Any, *, digits: int = 1) -> str:
    """null → '–'; floats with trailing-zero trimming."""
    if v is None:
        return "–"
    if isinstance(v, bool):
        return str(v)
    if isinstance(v, int):
        return str(v)
    if isinstance(v, float):
        s = f"{v:.{digits}f}"
        return s
    return esc(v)


def fmt_pct(v: Any) -> str:
    """per_session_rate (0..1) → '0.42 / session' style; null → '–'."""
    if v is None:
        return "–"
    return f"{v:.2f} / session"


def bar_html(value: int, max_value: int, *, color: str = "#28a745") -> str:
    """CSS-only horizontal bar. value/max_value -> width %."""
    if max_value <= 0:
        width = 0
    else:
        width = int(round(min(1.0, value / max_value) * 100))
    return (
        f'<div class="bar-track">'
        f'<div class="bar-fill" '
        f'style="width: {width}%; background: {color};"></div>'
        f'</div>'
    )


def render_header(data: dict[str, Any]) -> str:
    phase = data["phase"].upper()
    win = data["window"]
    totals = data["totals"]
    models = sorted({row["model"] for row in data["tier3"]["by_model_role"]
                     if row.get("model")})
    model_str = ", ".join(models) if models else "–"
    return f"""
<header class="report-header">
  <div class="phase-badge phase-{esc(data['phase'])}">{esc(phase)}</div>
  <div class="header-meta">
    <div class="row"><span class="label">source</span>
      <span>{esc(data['source_name'])}</span></div>
    <div class="row"><span class="label">window</span>
      <span>{esc(win['since'])} &le; t &lt; {esc(win['until'])}</span></div>
    <div class="row"><span class="label">repo</span>
      <span>{esc(data['source_name'])} repo (.team/traces/traces.db)</span></div>
    <div class="row"><span class="label">model bucket</span>
      <span>{esc(model_str)}</span></div>
  </div>
  <div class="totals-grid">
    <div><span class="big">{esc(totals['sessions'])}</span><span>sessions</span></div>
    <div><span class="big">{esc(totals['tasks'])}</span><span>tasks</span></div>
    <div><span class="big">{esc(totals['tool_calls'])}</span><span>tool_calls</span></div>
    <div><span class="big">{esc(totals['bash_calls'])}</span><span>bash_calls</span></div>
  </div>
</header>
"""


CATEGORY_LABELS = {
    "cd": "cd (incl. compound)",
    "git_switch": "git checkout / git switch",
    "env": "export / unset",
    "pwd": "pwd (standalone)",
    "git_status": "git status / branch --show-current / rev-parse --abbrev-ref",
    "env_check": "printenv / echo $... / env | ...",
}


def render_tier(data: dict[str, Any], tier_key: str, title: str,
                blurb: str, categories: list[str]) -> str:
    tier = data[tier_key]
    rates = data["per_session_rate"]
    # max for bar normalization across this tier's totals
    totals = [tier[c]["total"] for c in categories]
    max_total = max(totals) if totals else 0
    rows: list[str] = []
    for cat in categories:
        counts = tier[cat]
        rate_key = f"{tier_key}.{cat}"
        cat_rates = rates.get(rate_key, {})
        agent_rate = cat_rates.get("agent")
        conductor_rate = cat_rates.get("conductor")
        master_rate = cat_rates.get("master")
        label = CATEGORY_LABELS.get(cat, cat)
        rows.append(f"""
<tr>
  <th class="cat">{esc(label)}</th>
  <td class="num total">{esc(counts['total'])}</td>
  <td class="bar-cell">{bar_html(counts['total'], max_total)}</td>
  <td class="num agent"><strong>{esc(counts['agent'])}</strong></td>
  <td class="num">{esc(counts['conductor'])}</td>
  <td class="num">{esc(counts['master'])}</td>
  <td class="rate agent"><strong>{esc(fmt_pct(agent_rate))}</strong></td>
  <td class="rate">{esc(fmt_pct(conductor_rate))}</td>
  <td class="rate">{esc(fmt_pct(master_rate))}</td>
</tr>
""")
    return f"""
<section class="tier">
  <h2>{esc(title)}</h2>
  <p class="blurb">{esc(blurb)}</p>
  <table class="tier-table">
    <thead>
      <tr>
        <th>category</th>
        <th class="num">total</th>
        <th>distribution</th>
        <th class="num">agent</th>
        <th class="num">conductor</th>
        <th class="num">master</th>
        <th>agent / session</th>
        <th>conductor / session</th>
        <th>master / session</th>
      </tr>
    </thead>
    <tbody>
      {''.join(rows)}
    </tbody>
  </table>
</section>
"""


def render_tier1(data: dict[str, Any]) -> str:
    return render_tier(
        data, "tier1",
        "Tier 1: 直接置換指標",
        "ctxd の chdir / git-switch / env-set で直接置換できる raw bash の出現頻度。",
        ["cd", "git_switch", "env"])


def render_tier2(data: dict[str, Any]) -> str:
    return render_tier(
        data, "tier2",
        "Tier 2: 波及指標",
        "ctxd の JSON envelope で代替できる確認系コマンド。Tier 1 との重複は計上しない。",
        ["pwd", "git_status", "env_check"])


def render_tier3(data: dict[str, Any]) -> str:
    by_role = data["tier3"]["by_role"]
    by_model_role = data["tier3"]["by_model_role"]
    role_rows = []
    for r in by_role:
        role_rows.append(f"""
<tr>
  <th>{esc(r['role'])}</th>
  <td class="num">{esc(r['tasks'])}</td>
  <td class="num">{esc(fmt_num(r['avg_input']))}</td>
  <td class="num">{esc(fmt_num(r['avg_output']))}</td>
  <td class="num">{esc(fmt_num(r['avg_cache_create']))}</td>
  <td class="num">{esc(fmt_num(r['avg_cache_read']))}</td>
  <td class="num">{esc(fmt_num(r['avg_duration_ms']))}</td>
</tr>""")
    model_rows = []
    for r in by_model_role:
        model_rows.append(f"""
<tr>
  <th>{esc(r['model'])}</th>
  <td>{esc(r['role'])}</td>
  <td class="num">{esc(r['tasks'])}</td>
  <td class="num">{esc(fmt_num(r['avg_input']))}</td>
  <td class="num">{esc(fmt_num(r['avg_output']))}</td>
  <td class="num">{esc(fmt_num(r['avg_duration_ms']))}</td>
</tr>""")
    return f"""
<section class="tier">
  <h2>Tier 3: 副作用指標</h2>
  <p class="blurb">api_usage を role / model bucket 別に集計。null は '–' 表示。</p>
  <h3>by role (1 task あたり avg)</h3>
  <table class="tier-table">
    <thead>
      <tr>
        <th>role</th>
        <th class="num">tasks</th>
        <th class="num">avg_input</th>
        <th class="num">avg_output</th>
        <th class="num">avg_cache_create</th>
        <th class="num">avg_cache_read</th>
        <th class="num">avg_duration_ms</th>
      </tr>
    </thead>
    <tbody>
      {''.join(role_rows) if role_rows else '<tr><td colspan="7" class="empty">no data</td></tr>'}
    </tbody>
  </table>
  <h3>by model &times; role</h3>
  <table class="tier-table">
    <thead>
      <tr>
        <th>model</th>
        <th>role</th>
        <th class="num">tasks</th>
        <th class="num">avg_input</th>
        <th class="num">avg_output</th>
        <th class="num">avg_duration_ms</th>
      </tr>
    </thead>
    <tbody>
      {''.join(model_rows) if model_rows else '<tr><td colspan="6" class="empty">no data</td></tr>'}
    </tbody>
  </table>
</section>
"""


def render_footer(data: dict[str, Any]) -> str:
    return f"""
<footer class="report-footer">
  <span>schema_version={esc(data['schema_version'])}</span>
  <span>generated_at={esc(data['generated_at'])}</span>
  <span>git_sha={esc(data['git_sha'])}</span>
  <span>phase={esc(data['phase'])}</span>
</footer>
"""


CSS = """
:root { color-scheme: light; }
* { box-sizing: border-box; }
body { font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif;
  background: #fff; color: #222; margin: 0; padding: 24px;
  max-width: 1200px; margin-left: auto; margin-right: auto; }
h1, h2, h3 { margin-top: 0; }
h2 { margin: 32px 0 4px; font-size: 18px; border-bottom: 1px solid #ddd;
  padding-bottom: 4px; }
h3 { margin: 20px 0 6px; font-size: 14px; color: #444; }
.blurb { color: #666; font-size: 13px; margin: 4px 0 12px; }
.report-header { display: grid; grid-template-columns: auto 1fr auto;
  gap: 24px; align-items: center; padding: 16px; background: #f8f9fa;
  border: 1px solid #e1e4e8; border-radius: 6px; }
.phase-badge { font-size: 36px; font-weight: 800; padding: 8px 20px;
  border-radius: 6px; letter-spacing: 0.06em; }
.phase-badge.phase-before { background: #fff3bf; color: #5c3d00; }
.phase-badge.phase-after { background: #d4edda; color: #155724; }
.header-meta .row { font-size: 13px; margin: 2px 0; }
.header-meta .label { display: inline-block; min-width: 110px;
  color: #666; font-weight: 600; }
.totals-grid { display: grid; grid-template-columns: repeat(4, auto);
  gap: 12px 18px; }
.totals-grid > div { display: flex; flex-direction: column; align-items: center;
  padding: 4px 8px; }
.totals-grid .big { font-size: 22px; font-weight: 700; color: #0366d6; }
.totals-grid span:not(.big) { font-size: 11px; color: #666;
  text-transform: uppercase; letter-spacing: 0.05em; }
section.tier { margin-top: 28px; }
table.tier-table { border-collapse: collapse; width: 100%; font-size: 13px;
  table-layout: auto; }
table.tier-table th, table.tier-table td { border: 1px solid #e1e4e8;
  padding: 6px 8px; text-align: left; vertical-align: middle; }
table.tier-table thead th { background: #f5f5f5; font-weight: 600;
  font-size: 12px; }
table.tier-table th.cat { width: 24%; font-weight: 500; }
table.tier-table .num { text-align: right; font-variant-numeric: tabular-nums;
  font-family: ui-monospace, "SF Mono", Menlo, monospace; }
table.tier-table .rate { text-align: right; font-variant-numeric: tabular-nums;
  font-family: ui-monospace, "SF Mono", Menlo, monospace; font-size: 12px; }
table.tier-table .agent { background: #f6fbfe; }
table.tier-table .total { background: #f9f9f9; font-weight: 600; }
table.tier-table .empty { color: #888; font-style: italic; text-align: center; }
.bar-cell { width: 28%; }
.bar-track { background: #eee; border-radius: 2px; height: 16px;
  overflow: hidden; }
.bar-fill { height: 100%; }
.report-footer { margin-top: 32px; padding-top: 12px;
  border-top: 1px solid #ddd; display: flex; gap: 18px; flex-wrap: wrap;
  font-size: 11px; color: #666;
  font-family: ui-monospace, "SF Mono", Menlo, monospace; }
"""


def assemble(data: dict[str, Any]) -> str:
    return f"""<!doctype html>
<html lang="ja">
<head>
  <meta charset="utf-8">
  <title>ctxd baseline report — {esc(data['source_name'])} {esc(data['phase'])}</title>
  <style>{CSS}</style>
</head>
<body>
  <h1>ctxd baseline report</h1>
{render_header(data)}
{render_tier1(data)}
{render_tier2(data)}
{render_tier3(data)}
{render_footer(data)}
</body>
</html>
"""


def write_atomic(html_text: str, out: Path) -> None:
    tmp = out.with_suffix(out.suffix + f".tmp.{os.getpid()}")
    tmp.write_text(html_text, encoding="utf-8")
    os.replace(tmp, out)


def main(argv: list[str] | None = None) -> int:
    args = parse_args(argv)
    data_path = Path(args.data)
    if not data_path.exists():
        print(f"data JSON not found: {data_path}", file=sys.stderr)
        return 2
    out_path = Path(args.out)
    try:
        data = load_data(data_path)
    except FileNotFoundError as e:
        print(f"data JSON not found: {e}", file=sys.stderr)
        return 2
    except (json.JSONDecodeError, ValueError) as e:
        msg = str(e)
        if "schema_version" in msg:
            print(msg, file=sys.stderr)
            return 5
        print(f"failed to parse data JSON: {msg}", file=sys.stderr)
        return 4
    except KeyError as e:
        print(f"malformed data JSON: missing key {e}", file=sys.stderr)
        return 5
    except Exception:
        traceback.print_exc()
        return 4
    try:
        html_text = assemble(data)
    except KeyError as e:
        print(f"malformed data JSON: missing key {e}", file=sys.stderr)
        return 5
    except Exception:
        traceback.print_exc()
        return 4
    write_atomic(html_text, out_path)
    return 0


if __name__ == "__main__":
    sys.exit(main())
