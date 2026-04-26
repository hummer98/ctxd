package postcondition

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/hummer98/ctxd/internal/output"
)

// walkStatus は walk() の結果ステータス。
type walkStatus int

const (
	// walkFound はパスの解決に成功 (最終値が nil でも FOUND 扱い)。
	walkFound walkStatus = iota
	// walkMissing は途中または末端で対応するキーが存在しなかった。
	walkMissing
)

// normalize は任意の Go 値を json round-trip して
// map[string]any / []any / string / float64 / bool / nil の正規形に落とす。
//
// 採用理由 (plan §3.3):
//   - 各コマンドの data は struct tag (`json:"git_branch"`) を尊重したい
//   - *string の nil は null になり missing と区別できる
//   - reflect で struct タグを再解釈する複雑さを回避
//
// 失敗時は元の値を返す (現実的には Marshal 失敗ケースは稀)。
func normalize(data any) any {
	if data == nil {
		return nil
	}
	raw, err := json.Marshal(data)
	if err != nil {
		return data
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return data
	}
	return v
}

// walk は normalize 済みの root から path に沿ってドット walk する。
//
// 中間ノードが map[string]any でない場合 (配列や scalar) は MISSING を返す。
// 配列の index アクセスは MVP 非対応 (plan §1.5)。
func walk(root any, path []string) (any, walkStatus) {
	cur := root
	for _, seg := range path {
		m, ok := cur.(map[string]any)
		if !ok {
			return nil, walkMissing
		}
		next, exists := m[seg]
		if !exists {
			return nil, walkMissing
		}
		cur = next
	}
	return cur, walkFound
}

// evalExpression は parse 済み Expression を normalize 済み data に対して評価し、
// output.Check を返す。data は walk が動く形 (map[string]any) を想定する。
func evalExpression(expr Expression, data any) output.Check {
	check := output.Check{
		Key:      expr.Key,
		Expected: stringifyValue(expr.Value),
	}

	actual, status := walk(data, expr.KeyPath)
	if status == walkMissing {
		check.Actual = "<missing>"
		check.Passed = false
		return check
	}

	switch expr.Op {
	case OpEq:
		passed, actualStr := evalEq(expr.Value, actual)
		check.Actual = actualStr
		check.Passed = passed
	case OpNe:
		passed, actualStr := evalNe(expr.Value, actual)
		check.Actual = actualStr
		check.Passed = passed
	case OpContains:
		passed, actualStr := evalContains(expr.Value, actual)
		check.Actual = actualStr
		check.Passed = passed
	default:
		check.Actual = fmt.Sprintf("<unsupported operator: %s>", expr.Op)
		check.Passed = false
	}
	return check
}

// evalEq は厳密一致を判定する。
// 型不一致 (期待値型 vs 実値型) は passed=false / actual="<type mismatch: ...>"。
func evalEq(expected Value, actual any) (bool, string) {
	switch expected.Kind {
	case KindString:
		s, ok := actual.(string)
		if !ok {
			return false, fmt.Sprintf("<type mismatch: %s>", actualKind(actual))
		}
		return s == expected.Str, s
	case KindBool:
		b, ok := actual.(bool)
		if !ok {
			return false, fmt.Sprintf("<type mismatch: %s>", actualKind(actual))
		}
		return b == expected.Bool, strconv.FormatBool(b)
	case KindInt:
		f, ok := actual.(float64)
		if !ok {
			return false, fmt.Sprintf("<type mismatch: %s>", actualKind(actual))
		}
		// |n| <= 2^53 の範囲で int64 と一致比較する (plan §1.6 精度上限)。
		return float64(expected.Int) == f, formatNumber(f)
	case KindNull:
		if actual == nil {
			return true, "null"
		}
		return false, fmt.Sprintf("<type mismatch: %s>", actualKind(actual))
	default:
		return false, fmt.Sprintf("<unsupported value kind: %d>", expected.Kind)
	}
}

// evalNe は不一致を判定する。
// 型不一致は passed=false に倒す (plan §1.4: 「型違いのため判定不能」を明示)。
func evalNe(expected Value, actual any) (bool, string) {
	eqPassed, actualStr := evalEq(expected, actual)
	// 型不一致のとき actualStr は "<type mismatch: ...>" になっている。
	if strings.HasPrefix(actualStr, "<type mismatch:") {
		return false, actualStr
	}
	return !eqPassed, actualStr
}

// evalContains は包含を判定する。
//   - actual が []any → 要素ごとに evalEq と同じ判定で一致する要素があるか
//   - actual が string → 期待値 (string のみ) を substring 含有で判定
//   - その他 (bool/int/null/map) → 型不一致
func evalContains(expected Value, actual any) (bool, string) {
	switch v := actual.(type) {
	case []any:
		actualStr := formatActual(actual)
		for _, elem := range v {
			ok, _ := evalEq(expected, elem)
			if ok {
				return true, actualStr
			}
		}
		return false, actualStr
	case string:
		if expected.Kind != KindString {
			return false, fmt.Sprintf("<type mismatch: string vs %s>", valueKindName(expected.Kind))
		}
		return strings.Contains(v, expected.Str), v
	default:
		return false, fmt.Sprintf("<type mismatch: %s>", actualKind(actual))
	}
}

// stringifyValue は期待値を Check.Expected 用に文字列化する (plan §4.1)。
//   - quoted string は `"..."` (quote 込みで残す)
//   - bool / null / int は素朴文字列
//   - bare string はクォートなし
func stringifyValue(v Value) string {
	switch v.Kind {
	case KindString:
		if v.Quoted {
			return `"` + v.Str + `"`
		}
		return v.Str
	case KindBool:
		return strconv.FormatBool(v.Bool)
	case KindInt:
		return strconv.FormatInt(v.Int, 10)
	case KindNull:
		return "null"
	default:
		return fmt.Sprintf("<unknown kind:%d>", v.Kind)
	}
}

// actualKind は実値の Kind 名を「DSL 文脈での読みやすさ」優先で返す。
func actualKind(v any) string {
	switch v.(type) {
	case nil:
		return "null"
	case string:
		return "string"
	case bool:
		return "bool"
	case float64:
		return "number"
	case []any:
		return "array"
	case map[string]any:
		return "object"
	default:
		return fmt.Sprintf("%T", v)
	}
}

// valueKindName は ValueKind を人間可読名に変換する。
func valueKindName(k ValueKind) string {
	switch k {
	case KindString:
		return "string"
	case KindBool:
		return "bool"
	case KindInt:
		return "int"
	case KindNull:
		return "null"
	default:
		return fmt.Sprintf("kind%d", k)
	}
}

// formatActual は actual を JSON-ish に文字列化する。
// 配列 / map は json.Marshal を経由し、scalar は そのまま %v 風に。
func formatActual(v any) string {
	switch x := v.(type) {
	case nil:
		return "null"
	case string:
		return x
	case bool:
		return strconv.FormatBool(x)
	case float64:
		return formatNumber(x)
	case []any, map[string]any:
		raw, err := json.Marshal(v)
		if err != nil {
			return fmt.Sprintf("%v", v)
		}
		return string(raw)
	default:
		return fmt.Sprintf("%v", v)
	}
}

// formatNumber は float64 が integer 表現可能なら整数文字列、そうでなければ %g。
func formatNumber(f float64) string {
	if f == float64(int64(f)) {
		return strconv.FormatInt(int64(f), 10)
	}
	return strconv.FormatFloat(f, 'g', -1, 64)
}
