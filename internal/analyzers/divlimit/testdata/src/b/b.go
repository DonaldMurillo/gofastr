// Package b holds divlimit positives in code that never existed in the
// repo: different names, same shape.
package b

import (
	"fmt"
	"net/http"
	"strconv"
)

// paginateRows divides by the pageSize parameter with no guard.
func paginateRows(rows, pageSize int) int {
	return rows / pageSize // want `integer division by pageSize`
}

// shardFor computes a bucket from the count parameter with no guard.
func shardFor(id int64, count int) int {
	return int(id) % count // want `integer division by count`
}

// parseAndPage reads the divisor straight out of the query string.
func parseAndPage(r *http.Request, rows int) (int, error) {
	limit, err := strconv.Atoi(r.URL.Query().Get("limit"))
	if err != nil {
		return 0, fmt.Errorf("limit: %w", err)
	}
	return rows / limit, nil // want `integer division by limit`
}

// paginateRowsGuarded is the fix posture for paginateRows.
func paginateRowsGuarded(rows, pageSize int) int {
	if pageSize == 0 {
		return 0
	}
	return rows / pageSize
}

// shardForGuarded refuses instead of defaulting.
func shardForGuarded(id int64, count int) (int, error) {
	if count <= 0 {
		return 0, fmt.Errorf("count must be positive")
	}
	return int(id) % count, nil
}

// parseAndPageGuarded validates the parsed limit before dividing.
func parseAndPageGuarded(r *http.Request, rows int) (int, error) {
	limit, err := strconv.Atoi(r.URL.Query().Get("limit"))
	if err != nil || limit < 1 {
		return 0, fmt.Errorf("limit must be a positive integer")
	}
	return rows / limit, nil
}

// otherNames: divisors outside the pagination family are left alone —
// the rule refuses to guess what `stride` is.
func otherNames(items, stride int) int {
	return items / stride
}

// lengthDivisor: len cannot be caller-set to a hostile value.
func lengthDivisor(items []int) int {
	size := len(items)
	return 100 / size
}

// constantDivisor is a constant: no caller can make it 0.
func constantDivisor(rows int) int {
	return rows / 25
}

// floatDivide has no panic to catch.
func floatDivide(total float64, limit int) float64 {
	return total / float64(limit)
}
