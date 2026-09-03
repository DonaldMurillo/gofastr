// Package e holds the divlimit spellings review 6 added: the guard
// must prove the divisor nonzero on the REACHING branch (a comparison
// selecting the zero values only counts when that branch diverges), a
// non-diverging guard branch constrains nothing, and selector divisors
// match by binding identity, not printed spelling.
package e

import "os"

// unsafeThenBranch divides inside the very branch that selected the
// zero divisor: the condition held means limit IS zero here.
func unsafeThenBranch(total, limit int) int {
	if limit == 0 {
		return total / limit // want `integer division by limit`
	}
	return 0
}

// safeThenBranch: the condition held on the reaching path proves the
// divisor nonzero.
func safeThenBranch(total, limit int) int {
	if limit != 0 {
		return total / limit
	}
	return 0
}

// safeLowerBound: limit >= 1 held proves nonzero.
func safeLowerBound(total, limit int) int {
	if limit >= 1 {
		return total / limit
	}
	return 0
}

// nonDivergingGuard logs the zero case and falls through: zero still
// reaches the division.
func nonDivergingGuard(total, limit int, log *[]string) int {
	if limit == 0 {
		*log = append(*log, "zero limit")
	}
	return total / limit // want `integer division by limit`
}

// wrongPolarityGuard returns on the safe values: only zero (or less)
// reaches the division.
func wrongPolarityGuard(total, limit int) int {
	if limit > 0 {
		return 0
	}
	return total / limit // want `integer division by limit`
}

// panicGuard, exitGuard, continueGuard: divergence spellings that do
// prove the continuing path nonzero.
func panicGuard(total, limit int) int {
	if limit < 1 {
		panic("limit must be positive")
	}
	return total / limit
}

func exitGuard(total, limit int) int {
	if limit == 0 {
		os.Exit(2)
	}
	return total / limit
}

func continueGuard(rows []int, limit int) int {
	n := 0
	for _, r := range rows {
		if limit == 0 {
			continue
		}
		n += r / limit
	}
	return n
}

// orGuard: on the continuing path err == nil AND limit >= 1, so the
// disjunct's failure still proves the divisor nonzero.
func orGuard(rows int, limit int, err error) int {
	if err != nil || limit < 1 {
		return 0
	}
	return rows / limit
}

// negatedGuard: !(limit == 0) held means limit != 0.
func negatedGuard(total, limit int) int {
	if !(limit == 0) {
		return total / limit
	}
	return 0
}

type req struct{ Total, Limit int }

// shadowedSelector: the guard compares the parameter q.Limit; the
// division divides by the block-shadowed q.Limit. Identical printed
// selectors, two different bindings — printed-form matching suppressed
// the finding; binding identity must not.
func shadowedSelector(q req, other req) int {
	if q.Limit == 0 {
		return 0
	}
	if other.Total > 0 {
		q := other
		return q.Total / q.Limit // want `integer division by q.Limit`
	}
	return 0
}

// shadowedSelectorGuarded: guard and division see the SAME binding, so
// the fix posture holds.
func shadowedSelectorGuarded(q req) int {
	if q.Limit == 0 {
		return 0
	}
	return q.Total / q.Limit
}
