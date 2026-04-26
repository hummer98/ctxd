package output

// ErrorCode は構造化エラー出力で使う識別子（snake_case 固定）。
// 値は JSON 出力にそのまま現れるため、AI エージェント側で安定的に分岐できるようにする。
type ErrorCode string

const (
	// ErrInvalidArgs は引数が不正な場合に返す。リトライ不可。
	ErrInvalidArgs ErrorCode = "invalid_args"
	// ErrNotFound はパスやリソースが存在しない場合に返す。リトライ不可。
	ErrNotFound ErrorCode = "not_found"
	// ErrNotADirectory はパスは存在するがディレクトリではない（ファイル等）場合に返す。リトライ不可。
	ErrNotADirectory ErrorCode = "not_a_directory"
	// ErrExecFailed は子プロセスやシステムコールが失敗した場合に返す。
	// transient かどうか判別困難なため MVP では保守的に retryable=false で扱う。
	ErrExecFailed ErrorCode = "exec_failed"
	// ErrPostconditionFailed は --expect の検証が失敗した場合に返す。
	// 「リトライではなく診断」の意図で retryable=false。
	ErrPostconditionFailed ErrorCode = "postcondition_failed"
)

// Retryable は ErrorCode の既定 retryable 判定を返す。
// MVP ではすべて false。将来 network_error 等を追加する際に true ケースを設ける。
//
// TODO: ネットワーク系コマンド導入時に network_error を追加し retryable=true ケースを設ける。
// net.Error.Temporary() 判定の取り込みも検討する。
func (c ErrorCode) Retryable() bool {
	return false
}
