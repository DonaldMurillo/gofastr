package d

func capturedLimitReassigned(total, limit int) func() int {
	if limit <= 0 {
		return nil
	}
	divide := func() int { return total / limit } // want `integer division by limit`
	limit = 0
	return divide
}

func capturedLimitGuardedInside(total, limit int) func() int {
	return func() int {
		if limit == 0 {
			return 0
		}
		return total / limit
	}
}
