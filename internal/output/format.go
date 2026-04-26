package output

import (
	"encoding/json"
	"fmt"
	"io"
)

// Write は Result を w へ書き出す。
// human=false なら 1 行 JSON、human=true なら 2 スペースインデントの整形 JSON を出力する。
// 末尾に必ず改行を付ける（端末でのバッファリング対策）。
//
// TODO: human モードは将来、色付け / キーの並び替え / 1 行サマリ表示を検討する。
// 現状は MarshalIndent ベースの最小実装に留める。
func Write(w io.Writer, r Result, human bool) error {
	var (
		raw []byte
		err error
	)
	if human {
		// json.MarshalIndent は Result.MarshalJSON を経由してから整形してくれない。
		// したがって一度 MarshalJSON を通して map にしてから MarshalIndent するか、
		// または素朴に json.MarshalIndent(r, ...) を呼ぶ。後者でも Result.MarshalJSON 経由になる。
		raw, err = marshalIndentResult(r)
	} else {
		raw, err = MarshalJSON(r)
	}
	if err != nil {
		return fmt.Errorf("output marshal: %w", err)
	}
	if _, err := w.Write(raw); err != nil {
		return err
	}
	if _, err := w.Write([]byte("\n")); err != nil {
		return err
	}
	return nil
}

// marshalIndentResult は Result の human 出力用に整形 JSON を作る。
// Result.MarshalJSON が args nil 正規化を行うので、それを通したあと再 unmarshal して整形する。
func marshalIndentResult(r Result) ([]byte, error) {
	raw, err := MarshalJSON(r)
	if err != nil {
		return nil, err
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, err
	}
	return json.MarshalIndent(v, "", "  ")
}
