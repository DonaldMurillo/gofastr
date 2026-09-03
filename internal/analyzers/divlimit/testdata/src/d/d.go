// Package d holds the divlimit spellings the 2026-09-02 adversarial
// review showed were missing or mis-scoped: struct-field divisors,
// divisions inside package-level function literals, and guards that
// do not dominate the division.
package d

import "net/http"

type ListQuery struct {
	Total  int
	Limit  int
	Offset int
}

// pagesOfQuery divides by a field of a decoded request struct: the
// standard handler spelling of a caller-supplied limit.
func pagesOfQuery(r *http.Request) int {
	q := decode(r)
	return q.Total / q.Limit // want `integer division by q.Limit`
}

func decode(r *http.Request) ListQuery { return ListQuery{} }

type pager struct{ total, Limit int }

// rem divides by the receiver's Limit field.
func (p pager) rem() int {
	return p.total % p.Limit // want `integer division by p.Limit`
}

// fieldGuarded compares the field divisor before dividing: the fix
// posture, recognized wherever it dominates.
func fieldGuarded(q ListQuery) int {
	if q.Limit == 0 {
		return 0
	}
	return q.Total / q.Limit
}

// Pages is a package-level function literal: the handler-table
// spelling, examined like any function declaration.
var Pages = func(total, limit int) int {
	return total / limit // want `integer division by limit`
}

// condGuard: the zero guard sits inside a nested conditional body of
// an earlier statement — it executes only when strict, so the division
// still panics on limit=0 with strict=false.
func condGuard(total, limit int, strict bool) int {
	if strict {
		if limit == 0 {
			return 0
		}
	}
	return total / limit // want `integer division by limit`
}

// condGuardFixed hoists the guard: now it dominates.
func condGuardFixed(total, limit int, strict bool) int {
	if limit == 0 {
		return 0
	}
	if strict {
		return total / limit
	}
	return total
}

// nestedClosureDivision: the closure is judged by its own checkFunc
// pass; the enclosing function's walk must cut at the FuncLit or the
// selector-shaped division reports twice (identical diagnostics go
// deduped by vet and analysistest — a no-dedupe driver shows the pair).
func nestedClosureDivision(r *http.Request) int {
	q := decode(r)
	apply := func(v int) int {
		return v / q.Limit // want `integer division by q.Limit`
	}
	return apply(q.Total)
}

// postGuardBreaks: the guard comparison sits in the loop's Post, which
// runs only after a NORMAL iteration — the immediate break skips it,
// and the division after the loop sees the unvetted limit.
func postGuardBreaks(total, limit int, clamps map[bool]int) int {
	for i := 0; i < 3; i, limit = i+1, clamps[limit == 0] {
		if i == 0 {
			break
		}
	}
	return total / limit // want `integer division by limit`
}

// keyGuardEmptyRange: the guard comparison sits in the range's KEY
// expression, evaluated only when the range yields an element — an
// empty range never vets the divisor.
func keyGuardEmptyRange(total, limit int, xs []int, clamps map[bool]int) int {
	for clamps[limit == 0] = range xs {
	}
	return total / limit // want `integer division by limit`
}

// conjDiverging: the failure of `limit == 0 && strict` can be strict
// alone — ¬(A && B) is ¬A ∨ ¬B, and which operand failed is unknown —
// so BOTH operands' failure must prove the divisor nonzero for the
// diverging guard to count. strict's failure proves nothing, and
// limit == 0 still reaches the division.
func conjDiverging(total, limit int, strict bool) int {
	if limit == 0 && strict {
		return 0
	}
	return total / limit // want `integer division by limit`
}

// conjHeld: the conjoined enclosing condition held means every operand
// held, so any one operand proving the divisor nonzero suffices.
func conjHeld(total, limit int, strict bool) int {
	if limit > 0 && strict {
		return total / limit
	}
	return 0
}
