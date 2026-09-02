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
