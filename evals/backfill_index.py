#!/usr/bin/env python3
"""evals/backfill_index.py — T024 Stage 1 用の 1 回限り index 移行スクリプト.

目的:
- `evals/results/index.md` / `index.csv` の列定義を T024 で 7 → 11 列に拡張する。
  既存行 (T013-T023 期に書かれた 7 列形) を 11 列形に変換し、効率指標 4 列を
  raw データから再集計する (残っていれば) か、`-` で埋める (gitignore で消えている場合).
- raw データの所在: `evals/results/<UTC>/session-*.{jsonl,meta.json,tools.jsonl}`.
  これらは `.gitignore` 対象 → 過去 commit から復元できない。手元 worktree に
  残っていない run については `-` のままで良い (T024 完了条件 §3 後段).

実行:
    python3 evals/backfill_index.py [--apply]
    --apply 無しでは diff を stdout に出すだけ (dry-run). 付けたら index.{md,csv}
    を実際に書き換える.

冪等性:
- 既に 11 列形の行はそのまま通す (再実行で上書きされない).
- raw データが残っていない過去 run は `-` のまま. raw データが追加で見つかれば
  再 run で値が埋まる.

注意 — 手動運用前提:
- T024 で 1 回走らせる. 以後 SKILL.md / harness が大幅に変わって列定義をまた
  変えたいときに参考実装として残す (`_oneoff/` に押し込まずトップに置くのは
  conductor-prompt §3 の「ドキュメント化するか議論」を後回しにしないため).
"""
from __future__ import annotations

import argparse
import json
import sys
from pathlib import Path
from typing import Any, Dict, List, Optional, Tuple

HERE = Path(__file__).resolve().parent
sys.path.insert(0, str(HERE))

import summarize  # noqa: E402

# T022 期 (= T024 直前) の列順 — backward-compat 検出に使う
LEGACY_CSV_HEADER = (
    "timestamp,model,plugin_version,claude_version,git_sha,"
    "overall_rate,per_scenario_rates"
)
LEGACY_FIELD_COUNT = 7

# T024 で追加する 4 列 — これが「足りない」行を「`-` × 4 を append」する基準
NEW_COLUMNS = ("avg_tool_uses", "avg_input_tokens", "avg_output_tokens", "avg_wall_ms")


def _load_scenarios() -> List[Dict[str, Any]]:
    """scenarios.jsonl を読み込む (raw 再集計時に必要)。"""
    return summarize.load_scenarios(str(HERE / "scenarios.jsonl"))


def _aggregate_results_dir(
    results_dir: Path, scenarios: List[Dict[str, Any]]
) -> Optional[Dict[str, int]]:
    """`evals/results/<TS>/` 配下に session-*.meta.json があれば効率指標を再集計する.

    返り値: 効率指標 dict (`tool_uses_avg` 等の summarize._efficiency_for_trials 形式).
    raw データが 1 件も無い場合 None.
    """
    metas = list(results_dir.glob("session-*.meta.json"))
    if not metas:
        return None
    results = summarize.load_results(results_dir, scenarios)
    if not results:
        return None
    return summarize._efficiency_for_trials(results)


def _migrate_csv_line(line: str, results_root: Path,
                      scenarios: List[Dict[str, Any]]) -> str:
    """既存 csv 1 行を T024 形 (11 列) に変換する。"""
    line = line.rstrip("\n")
    if not line:
        return line
    fields = line.split(",")
    if len(fields) >= LEGACY_FIELD_COUNT + 4:
        return line  # 既に 11 列以上 — 触らない
    if len(fields) != LEGACY_FIELD_COUNT:
        # 想定外の列数。安全側で何もしない (手動確認のため stderr に出す)
        print(f"backfill: WARN unexpected csv field count {len(fields)}: {line!r}",
              file=sys.stderr)
        return line
    timestamp = fields[0]
    eff = _aggregate_results_dir(results_root / timestamp, scenarios)
    if eff is None:
        # raw 不在 → `-` × 4 で埋める
        return line + ",-,-,-,-"
    return (
        f"{line},{eff['tool_uses_avg']},{eff['input_tokens_avg']},"
        f"{eff['output_tokens_avg']},{eff['wall_ms_avg']}"
    )


def _migrate_md_line(line: str, results_root: Path,
                     scenarios: List[Dict[str, Any]]) -> str:
    """既存 md 1 行を T024 形に変換する。

    md 行は `| ... | ... |` 形式. 末尾 `|` の前に 4 セルを足す.
    """
    line = line.rstrip("\n")
    if not line.startswith("|"):
        return line
    # heading 直下の separator 行 (`| --- | --- ...`) は `migrate_md_table` 側で扱う
    if "---" in line:
        return line
    # heading line 自体 (`| timestamp | model | ...`)
    if line.lstrip().startswith("| timestamp"):
        return line
    # 列数を数える: 末尾 `|` まで含めると `|` の数 = 列数 + 1
    cell_count = line.count("|") - 1
    if cell_count >= LEGACY_FIELD_COUNT + 4:
        return line  # 既に 11 列以上
    if cell_count != LEGACY_FIELD_COUNT:
        print(f"backfill: WARN unexpected md cell count {cell_count}: {line!r}",
              file=sys.stderr)
        return line
    parts = [p.strip() for p in line.strip().strip("|").split("|")]
    timestamp = parts[0]
    eff = _aggregate_results_dir(results_root / timestamp, scenarios)
    if eff is None:
        suffix = " - | - | - | - |"
    else:
        suffix = (
            f" {eff['tool_uses_avg']} | {eff['input_tokens_avg']} | "
            f"{eff['output_tokens_avg']} | {eff['wall_ms_avg']} |"
        )
    return line.rstrip("|").rstrip() + " |" + suffix


def migrate_csv(path: Path, results_root: Path,
                scenarios: List[Dict[str, Any]]) -> str:
    """csv ファイル全体を T024 形に書き換えた string を返す."""
    if not path.exists():
        return ""
    out_lines: List[str] = []
    with path.open("r", encoding="utf-8") as f:
        for i, raw in enumerate(f):
            stripped = raw.rstrip("\n")
            if i == 0:
                # header 行
                if stripped.startswith("timestamp,model"):
                    if "avg_tool_uses" in stripped:
                        out_lines.append(stripped)
                    else:
                        out_lines.append(
                            stripped + "," + ",".join(NEW_COLUMNS)
                        )
                else:
                    out_lines.append(stripped)
                continue
            out_lines.append(_migrate_csv_line(stripped, results_root, scenarios))
    return "\n".join(out_lines) + "\n"


def migrate_md(path: Path, results_root: Path,
               scenarios: List[Dict[str, Any]]) -> str:
    """md ファイル全体を T024 形に書き換えた string を返す."""
    if not path.exists():
        return ""
    out_lines: List[str] = []
    with path.open("r", encoding="utf-8") as f:
        for raw in f:
            line = raw.rstrip("\n")
            if line.lstrip().startswith("| timestamp"):
                # heading 列名: 既に拡張済みかどうか判定
                if "avg_tool_uses" in line:
                    out_lines.append(line)
                else:
                    out_lines.append(
                        line.rstrip("|").rstrip()
                        + " | avg_tool_uses | avg_input_tokens "
                        + "| avg_output_tokens | avg_wall_ms |"
                    )
                continue
            if line.lstrip().startswith("| ---"):
                # separator: 既に拡張済みかどうか判定 (`---` カウント)
                cells = [c for c in line.split("|") if c.strip().startswith("-")]
                if len(cells) >= LEGACY_FIELD_COUNT + 4:
                    out_lines.append(line)
                else:
                    out_lines.append(
                        line.rstrip("|").rstrip()
                        + " | ---: | ---: | ---: | ---: |"
                    )
                continue
            out_lines.append(_migrate_md_line(line, results_root, scenarios))
    return "\n".join(out_lines) + "\n"


def main(argv: Optional[List[str]] = None) -> int:
    parser = argparse.ArgumentParser(description="T024 index backfill (one-off)")
    parser.add_argument(
        "--apply", action="store_true",
        help="実際にファイルを書き換える (省略時は変換結果を stdout に出すだけ)",
    )
    parser.add_argument(
        "--results-root", default=str(HERE / "results"),
        help="evals/results/ の場所 (default: evals/results)",
    )
    args = parser.parse_args(argv)

    results_root = Path(args.results_root)
    md_path = results_root / "index.md"
    csv_path = results_root / "index.csv"

    scenarios = _load_scenarios()

    new_md = migrate_md(md_path, results_root, scenarios)
    new_csv = migrate_csv(csv_path, results_root, scenarios)

    if args.apply:
        if new_md:
            md_path.write_text(new_md, encoding="utf-8")
            print(f"backfill: wrote {md_path}")
        if new_csv:
            csv_path.write_text(new_csv, encoding="utf-8")
            print(f"backfill: wrote {csv_path}")
    else:
        print("=== index.md (dry-run) ===")
        print(new_md)
        print("=== index.csv (dry-run) ===")
        print(new_csv)

    return 0


if __name__ == "__main__":
    sys.exit(main())
