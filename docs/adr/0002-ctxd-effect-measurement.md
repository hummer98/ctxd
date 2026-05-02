# ADR 0002: ctxd の効果測定 — cmux-team trace を母数とした Before/After 計測

## Status

Proposed (2026-05-02)

## Context

ctxd の SKILL 改修効果は `evals/run.sh` harness で計測している (詳細は `CLAUDE.md` の `## SKILL バージョンアップ運用`)。これは「SKILL.md の trigger に対して claude が ctxd を呼ぶか」という adherence の指標としては機能している。一方、以下が示せていない:

1. **実プロジェクトでの効果**: eval は ctxd 専用に作った 5 シナリオを 1 trial 単位で回す合成テスト。実際の開発作業 (cmux-team で多 task を並列に進める日常運用) で ctxd が何を改善するかは別の話。
2. **採用判断のための ROI**: 「ctxd を入れると何がどれだけ良くなるか」を数値で示せていないので、外部に対して「使うべき理由」が説明できない。SKILL の遵守率 100% は手段の達成度であって、目的の達成度ではない。
3. **絶対値の意味**: eval の `avg_tool_uses=3` 等は ctxd 専用シナリオの絶対値であり、他プロジェクトの数値と比較できない。SKILL バージョン間の相対比較にしか使えない。

cmux-team プラグインの hook 機構は、claude code の全 tool 呼び出しを `.team/traces/traces.db` の `hook_signals` テーブルに記録している (`tool_input.command` 込みで `payload_json` に保存)。このデータは ctxd の効果測定に直接利用できる。本リポジトリ (ctxd 自身を開発している cmux-team 環境) には 2026-04-25 以降の trace が蓄積済みで、ctxd 未投入の **Before データ**としてそのまま使える。

ctxd を本 repo の cmux-team 運用に投入したあと、同じ抽出を再度行えば **After データ**が取れる。両者の差分が ctxd の実プロジェクト効果になる。

## Decision

**cmux-team の `.team/traces/traces.db` を母数として、ctxd 投入前後で 3 tier 指標を計測し、Before/After を HTML レポートで可視化する。**

### 計測対象データ

- データソース: ctxd repo 内の `.team/traces/traces.db` (sqlite)
- 主テーブル: `hook_signals` (tool 呼び出し)、`api_usage` (token / duration)、`task_sessions` (session/task メタ)
- 母数の単位: **tool 呼び出し件数 / セッション件数 / タスク件数**。期間は計測時点で利用可能な全期間 (絞り込みは spec 側で `--since` フラグで対応)
- 対象 role: 全 role を集計するが、ブレイクダウン表示で `agent` / `conductor` / `master` を分ける。実装作業が集中する `agent` が ctxd 効果を最も受けるはずなので、主指標は agent ロール基準で見る

### 3 tier 指標

#### Tier 1: 直接置換指標 (核心)

ctxd の SKILL が「raw bash で書くな、ctxd 経由で書け」と言っている操作。Before で発生していたものは After でゼロに近づくはず。

- `cd` (compound `cd X && Y` 含む)
- `git checkout` / `git switch`
- `export FOO=` / `unset FOO`

成功条件: ctxd 投入後に **これらの発生件数が 80% 以上減少** する。完全ゼロは現実的でない (ctxd が CLI として捕捉しない escape hatch を残しているのと、claude が誤って raw bash を選ぶケースが残るため)。

#### Tier 2: 波及指標 (envelope による状態確認の不要化)

ctxd は実行ごとに envelope で `cwd` / `branch` / `dirty` / `ahead` / `behind` 等を返す。claude は次の操作前に状態を確認する必要が減る。よってこれら "確認系 tool call" は減るはず。

- 単独 `pwd`
- `git status` / `git branch --show-current`
- `printenv` / `echo $FOO`

成功条件: agent ロールにおいて、これらの確認系 tool call が **session あたり 30% 以上減少** する。

#### Tier 3: 二次指標 (副作用の保険)

ctxd 投入で全体が遅く / 重くならないかを確認する保険指標。劣化していたら「効果はあるが採用コストが高い」となり、SKILL 改修や ctxd 自身の改修にフィードバックが要る。

- role 別 1 task あたり avg tool_uses
- role 別 1 task あたり avg input/output/cache tokens
- role 別 1 task あたり avg duration_ms (api_usage)

成功条件: いずれの指標も Before 比 **±20% 以内**に収まる。

### 成功判定の総合ルール

- Tier 1 が 80% 以上減 **かつ** Tier 2 が 30% 以上減 **かつ** Tier 3 が ±20% 以内 → ctxd は実プロジェクトで有効
- Tier 1 だけ達成 / 他は微妙 → 「raw bash 置換は起きたが日常作業は変わらず」 → SKILL の射程を Tier 2 にも広げる議論が要る
- Tier 1 が達成できない → SKILL 改修 (eval の adherence は出ているのに traces で raw bash が残る = SKILL trigger と現場 prompt の乖離) で対応

### 出力先

- スクリプト: `scripts/build-baseline-report.sh`
- HTML: `evals/baseline/cmux-team-<phase>-<YYYYMMDD>.html` (phase = `before` | `after`)
- データソース snapshot: `evals/baseline/data-<phase>-<YYYYMMDD>.json` (将来 schema が変わっても再描画できるよう中間データを残す)
- 既存 `evals/results/report.html` (ctxd 専用 eval) と分離する。混ぜると指標の意味が混ざる

## Consequences

### Pros

- ctxd の効果を **実運用データ**で示せる。eval だけでは絶対値の意味が薄かった部分を補完する
- SKILL 改修 / ctxd 自身の改修にデータドリブンな根拠が与えられる (Tier 2 が動かない → SKILL の射程議論、Tier 3 が劣化 → ctxd CLI の overhead 削減議論、等)
- Before/After の枠組みが固定されれば、ctxd の version bump ごとに再計測する resource にもなる (`v0.3.0` After を `v0.4.0` Before として再利用、等)

### Cons / 限界

- **母数が 1 repo / 1 ユーザー**: 自分が触っている ctxd repo の cmux-team 環境のみ。サンプリングバイアスが大きい。他人 / 他プロジェクトでの効果は generalize できない
- **cmux-team の hook schema 依存**: `hook_signals.payload_json` の構造が変わると spec の SQL を直す必要がある。schema バージョン (`schema_version`) を spec に明記して追従責任を明確にする
- **claude 自身の進化と ctxd 効果が混ざる**: Before/After の間に claude code バージョンが上がった場合、Tier 3 の効率変化が ctxd 起因か claude 起因か切り分けにくい。`api_usage.model` で model 列を bucket して影響を分離する
- **session 母数が少ない期間がある**: 2026-04-26 〜 05-02 で 1468 件の hook_signals だが、Tier 1 のヒット件数は数十オーダー。統計的有意性は弱い。母数を増やしたいなら計測期間を延ばす運用に倒す (Before を 2 週間以上に伸ばすかは Open question)

### Open questions

1. **母数拡大**: cmux-team を使っている他リポジトリ (cmux-team 本体、slaido 等) の `.team/traces/traces.db` をマージするか? マージすると母数が増えるが、対象 repo 列の追加と DB ファイル収集の運用負荷が増える。**初版では ctxd repo のみで開始**。マージは 0003 ADR 以降で検討
2. **After 計測のタイミング**: ctxd 投入後すぐ計測すると "新ツール慣れ" のノイズが入る。投入から **2 週間以上**経過、かつ Bash tool 呼び出しが Before と同オーダー以上になってから取る、を運用ルールにする
3. **匿名化**: `tool_input.command` には ctxd repo 内の作業内容が生で入る。HTML レポートを公開 commit するなら top-N 例の表示でセンシティブ情報が出ないか確認が必要。**初版ではドリルダウン UI を出さず集計値のみ表示**にして、公開許容範囲を最小化
4. **claude code バージョン分離**: api_usage の model 列で bucket するだけで足りるか、claude code CLI バージョン (`claude --version`) も記録列に含めるか。spec の中で要検討
5. **schema_version=1 の追補項目**: `per_session_rate` の `null` 表現 (sessions_by_role の denominator が 0 の場合) と `SOURCE_DATE_EPOCH` による `generated_at` 冪等性 opt-in は、schema_version を bump せずに `docs/measurement-baseline-spec.md` §4.2 補足 / §4.1 shell コメントで追補済 (T033)。schema_version=2 を切る判断はこれらが破壊的変更を要するときに改めて行う

## 参考

- 計測 spec: `docs/measurement-baseline-spec.md` (実装仕様、SQL 定義、HTML 構成)
- ctxd 専用 eval: `CLAUDE.md` の `## SKILL バージョンアップ運用 (T026)` / `evals/results/index.md`
- cmux-team hook: 本 repo `.team/traces/traces.db` schema (本 ADR 起草時点で `schema_version` の明示はないため、`docs/measurement-baseline-spec.md` 側で観測した schema を固定する)
