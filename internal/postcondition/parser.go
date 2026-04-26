package postcondition

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// Op は postcondition DSL で使う演算子の identity。
type Op string

const (
	// OpEq は厳密一致。`=` および `==` (alias) で表記される。
	OpEq Op = "="
	// OpNe は不一致。
	OpNe Op = "!="
	// OpContains は包含 (array は要素一致 / string は substring)。
	OpContains Op = "contains"
)

// ValueKind は Value の型タグ。
type ValueKind int

const (
	// KindString は文字列値 (bare / quoted のいずれも)。Quoted フィールドで区別する。
	KindString ValueKind = iota
	// KindBool は真偽値 (`true` / `false`)。
	KindBool
	// KindInt は整数値 (int64 範囲)。
	KindInt
	// KindNull は null リテラル。
	KindNull
)

// Value は parser が型推論した期待値。
//
// Quoted は Kind==KindString のときのみ意味を持ち、DSL 上で `"..."` で書かれたかを表す。
// Expected の文字列化で bare/quoted を区別する根拠として使う (plan §3.2 / §4.1)。
type Value struct {
	Kind   ValueKind
	Bool   bool
	Int    int64
	Str    string
	Quoted bool
}

// Expression は parse 済みの 1 expression。
//
// Raw は元の expect 文字列 (Check.Key 表示などのため保持)、
// Key は WS 正規化済みのドット記法 (例: "diff.added")、
// KeyPath は Key を "." で分解した segment 列。
type Expression struct {
	Raw     string
	Key     string
	KeyPath []string
	Op      Op
	Value   Value
}

// Parse は単一の DSL expression を解釈する。
//
// 戻り値 error は素のメッセージのみを保持する error 型に固定する
// (sentinel error / typed error は導入しない)。
// Verifier 側は err.Error() を "<parse error: ...>" に詰め直すだけの想定。
func Parse(raw string) (Expression, error) {
	expr := Expression{Raw: raw}

	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return expr, errors.New("empty expression")
	}

	keyEnd, op, valueStart, err := findOperator(trimmed)
	if err != nil {
		return expr, err
	}

	keyStr := strings.TrimSpace(trimmed[:keyEnd])
	if keyStr == "" {
		return expr, errors.New("missing key before operator")
	}
	keyPath, err := parseKey(keyStr)
	if err != nil {
		return expr, err
	}

	valueRaw := strings.TrimLeft(trimmed[valueStart:], " \t")
	value, err := parseValue(valueRaw)
	if err != nil {
		return expr, err
	}

	expr.Key = strings.Join(keyPath, ".")
	expr.KeyPath = keyPath
	expr.Op = op
	expr.Value = value
	return expr, nil
}

// findOperator は trimmed expression から (key 終端 index, op, value 開始 index) を返す。
//
// 演算子の探索優先度は誤マッチを避けるため:
//  1. " contains " (前後の空白必須、key の途中で contains を含む文字列にも対応)
//  2. "!=" (= より長いので先に判定)
//  3. "==" (= より長いので先に判定)
//  4. "=" (最後)
func findOperator(s string) (keyEnd int, op Op, valueStart int, err error) {
	if i := indexContains(s); i >= 0 {
		return i, OpContains, i + len(" contains "), nil
	}
	if i := strings.Index(s, "!="); i >= 0 {
		return i, OpNe, i + 2, nil
	}
	if i := strings.Index(s, "=="); i >= 0 {
		return i, OpEq, i + 2, nil
	}
	if i := strings.IndexByte(s, '='); i >= 0 {
		return i, OpEq, i + 1, nil
	}
	return 0, "", 0, fmt.Errorf("missing operator in %q (expected =, ==, !=, or contains)", s)
}

// indexContains は " contains " (前後 1 文字以上の空白) の出現位置を返す。
// "contains" がキー名の一部に含まれる悪質ケース (例: `contains_foo=bar`) を避けるため、
// 前後に空白がある形でのみマッチする。
func indexContains(s string) int {
	const kw = "contains"
	start := 0
	for {
		idx := strings.Index(s[start:], kw)
		if idx < 0 {
			return -1
		}
		abs := start + idx
		// 前後にホワイトスペースが必要。
		if abs > 0 && isWS(s[abs-1]) && abs+len(kw) < len(s) && isWS(s[abs+len(kw)]) {
			return abs - 1
		}
		start = abs + len(kw)
	}
}

func isWS(b byte) bool {
	return b == ' ' || b == '\t'
}

// parseKey は key 文字列を ドット区切りで segment 列に分解し、
// 各 segment が許容文字種 ([A-Za-z_][A-Za-z0-9_]*) に従っているかを検証する。
func parseKey(s string) ([]string, error) {
	if s == "" {
		return nil, errors.New("empty key")
	}
	if strings.HasPrefix(s, ".") {
		return nil, fmt.Errorf("invalid key %q: leading dot not allowed", s)
	}
	if strings.HasSuffix(s, ".") {
		return nil, fmt.Errorf("invalid key %q: trailing dot not allowed", s)
	}
	segs := strings.Split(s, ".")
	for _, seg := range segs {
		if !validKeySegment(seg) {
			return nil, fmt.Errorf("invalid key segment %q (allowed: [A-Za-z_][A-Za-z0-9_]*)", seg)
		}
	}
	return segs, nil
}

func validKeySegment(seg string) bool {
	if seg == "" {
		return false
	}
	for i := 0; i < len(seg); i++ {
		c := seg[i]
		switch {
		case c >= 'A' && c <= 'Z':
		case c >= 'a' && c <= 'z':
		case c == '_':
		case c >= '0' && c <= '9':
			if i == 0 {
				return false
			}
		default:
			return false
		}
	}
	return true
}

// parseValue は DSL 上の値リテラルを推論して Value に詰める。
// 優先順位: quoted_string → bool → null → int → bare_string。
func parseValue(s string) (Value, error) {
	// trailing WS は除去 (bare string の意図しない空白を抑制)。
	s = strings.TrimRight(s, " \t")
	if strings.HasPrefix(s, `"`) {
		return parseQuotedString(s)
	}
	switch s {
	case "true":
		return Value{Kind: KindBool, Bool: true}, nil
	case "false":
		return Value{Kind: KindBool, Bool: false}, nil
	case "null":
		return Value{Kind: KindNull}, nil
	}
	if isIntLiteral(s) {
		n, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			return Value{}, fmt.Errorf("integer overflow in value %q", s)
		}
		return Value{Kind: KindInt, Int: n}, nil
	}
	return Value{Kind: KindString, Str: s}, nil
}

func isIntLiteral(s string) bool {
	if s == "" {
		return false
	}
	i := 0
	if s[0] == '-' {
		if len(s) == 1 {
			return false
		}
		i = 1
	}
	for ; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

// parseQuotedString は `"..."` 形式 (escape: `\"` と `\\`) を処理する。
func parseQuotedString(s string) (Value, error) {
	if !strings.HasPrefix(s, `"`) {
		return Value{}, fmt.Errorf("expected opening quote in %q", s)
	}
	var sb strings.Builder
	i := 1
	for i < len(s) {
		c := s[i]
		switch c {
		case '"':
			// 閉じクォート発見。後続にゴミがないかは厳密にはチェックしない (trailing WS は既に除去済み)。
			if i != len(s)-1 {
				return Value{}, fmt.Errorf("unexpected trailing characters after closing quote in %q", s)
			}
			return Value{Kind: KindString, Str: sb.String(), Quoted: true}, nil
		case '\\':
			if i+1 >= len(s) {
				return Value{}, fmt.Errorf("unterminated escape sequence in %q", s)
			}
			next := s[i+1]
			switch next {
			case '"', '\\':
				sb.WriteByte(next)
				i += 2
			default:
				return Value{}, fmt.Errorf("unsupported escape sequence \\%c in %q", next, s)
			}
		default:
			sb.WriteByte(c)
			i++
		}
	}
	return Value{}, fmt.Errorf("unterminated quoted string in %q (missing closing quote)", s)
}
