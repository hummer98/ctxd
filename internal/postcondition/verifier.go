package postcondition

import (
	"github.com/hummer98/ctxd/internal/output"
)

// Default は本タスクで実装する Verifier 実装。
// DSL parser (Parse) と evaluator (evalExpression) を統合する (plan §3.2)。
type Default struct{}

// Verify は postcondition.Verifier interface 実装。
//
//   - expects のいずれかが parse 不能でも error は伝播せず、
//     Check.Passed=false / Actual="<parse error: ...>" として埋める (Q2 暫定 a)。
//   - 全ての expression を data に対して evaluate し、Postcondition を返す。
//   - すべての check が Passed=true なら Postcondition.Passed=true、そうでなければ false。
//   - expects が空なら Checks=[] / Passed=true (defensive: vacuously true)。
func (Default) Verify(expects []string, data any) *output.Postcondition {
	checks := make([]output.Check, 0, len(expects))
	if len(expects) == 0 {
		return &output.Postcondition{Passed: true, Checks: checks}
	}

	normalized := normalize(data)
	allPassed := true
	for _, raw := range expects {
		expr, err := Parse(raw)
		if err != nil {
			checks = append(checks, output.Check{
				Key:      raw,
				Expected: raw,
				Actual:   "<parse error: " + err.Error() + ">",
				Passed:   false,
			})
			allPassed = false
			continue
		}
		c := evalExpression(expr, normalized)
		if !c.Passed {
			allPassed = false
		}
		checks = append(checks, c)
	}

	return &output.Postcondition{Passed: allPassed, Checks: checks}
}
