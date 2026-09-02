// Package a holds the divlimit fixture reduced from the real bug site:
// framework/crud crud_stream.go ServeStreamingList's page math as it
// was before fix a24928c1 (probe TestStreamingListZeroLimitNoPanic).
package a

// ServeStreamingList, reduced to its page math. An in-process caller
// passing limit=0 panicked with "integer divide by zero" before the
// first row was streamed.
func ServeStreamingList(total int, page, limit int) int {
	totalPages := total / limit // want `integer division by limit`
	if total%limit != 0 {       // want `integer division by limit`
		totalPages++
	}
	return totalPages
}

// ServeStreamingListFixed is the fix posture: guard the division.
func ServeStreamingListFixed(total int, page, limit int) int {
	totalPages := 0
	if limit > 0 {
		totalPages = total / limit
		if total%limit != 0 {
			totalPages++
		}
	}
	return totalPages
}

// ServeStreamingListRefuse is the other fix spelling: refuse the bad
// limit up front instead of defaulting.
func ServeStreamingListRefuse(total int, page, limit int) (int, error) {
	if limit < 1 {
		return 0, errBadLimit
	}
	totalPages := total / limit
	if total%limit != 0 {
		totalPages++
	}
	return totalPages, nil
}
