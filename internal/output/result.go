// Package output は ctxd のすべてのコマンドが返す統一出力フォーマットを定義する。
//
// JSON 出力のキーは snake_case で固定する（docs/seed.md の規約）。
// Go のフィールド名は CamelCase、JSON タグで snake_case を明示する。
package output

import "encoding/json"

// Result はすべてのコマンドが返す統一フォーマット。
// 成功時は Data フィールドに、失敗時は Error フィールドに値が入る（排他的）。
type Result struct {
	OK            bool           `json:"ok"`
	Cmd           string         `json:"cmd"`
	Args          []string       `json:"args"`
	Data          any            `json:"result,omitempty"`        // 成功時のコマンド固有ペイロード
	Error         *Error         `json:"error,omitempty"`         // 失敗時のみ
	Postcondition *Postcondition `json:"postcondition,omitempty"` // --expect 指定時のみ
	ElapsedMs     int64          `json:"elapsed_ms"`
}

// MarshalJSON は Result を encoding/json で marshal するためのカスタム実装。
// Args が nil でも JSON 出力では必ず空配列 [] になるよう正規化する。
// AI エージェント側で args を必ず array として unmarshal できる保証を与える。
func (r Result) MarshalJSON() ([]byte, error) {
	type alias Result
	cp := alias(r)
	if cp.Args == nil {
		cp.Args = []string{}
	}
	return json.Marshal(cp)
}

// Error は失敗時のエラー情報。
type Error struct {
	Code      ErrorCode `json:"code"`
	Message   string    `json:"message"`
	Retryable bool      `json:"retryable"`
}

// Postcondition は --expect で指定された事後条件の検証結果。
type Postcondition struct {
	Passed bool    `json:"passed"`
	Checks []Check `json:"checks"`
}

// Check は postcondition の個別チェック項目。
type Check struct {
	Key      string `json:"key"`
	Expected string `json:"expected"`
	Actual   string `json:"actual"`
	Passed   bool   `json:"passed"`
}

// MarshalJSON は Result を JSON へシリアライズする最も単純なエントリポイント。
// Result.MarshalJSON によって args nil の正規化はカスタム実装側で吸収される。
func MarshalJSON(r Result) ([]byte, error) {
	return json.Marshal(r)
}

// NewError は code とメッセージから Error を構築する。
// retryable は ErrorCode 既定値（現在はすべて false）を採用する。
func NewError(code ErrorCode, msg string) *Error {
	return &Error{
		Code:      code,
		Message:   msg,
		Retryable: code.Retryable(),
	}
}
