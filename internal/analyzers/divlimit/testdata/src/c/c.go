// Package c is a third divlimit positive with a different layout: the
// divisor arrives through a method on a table type.
package c

import "fmt"

type Pager struct {
	Rows int
}

// PagesOf divides the table by the perPage argument.
func (p Pager) PagesOf(perPage int) int {
	return p.Rows / perPage // want `integer division by perPage`
}

// PagesOfFixed guards first.
func (p Pager) PagesOfFixed(perPage int) int {
	if perPage > 0 {
		return p.Rows / perPage
	}
	return 0
}

// OffsetFor uses the raw n parameter in two divisions.
func OffsetFor(page, n int) int {
	pages := 100 / n // want `integer division by n`
	rest := 100 % n  // want `integer division by n`
	return pages + rest + page
}

// OffsetForFixed checks n against 1 once, up front.
func OffsetForFixed(page, n int) (int, error) {
	if n < 1 {
		return 0, fmt.Errorf("n must be at least 1")
	}
	return 100/n + 100%n + page, nil
}
