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

func capturedImmediate(total, limit int) int {
	if limit == 0 {
		return 0
	}
	return (func() int { return total / limit })()
}

func capturedCalledAfterWrite(total, limit int) int {
	if limit <= 0 {
		return 0
	}
	divide := func() int { return total / limit } // want `integer division by limit`
	limit = 0
	return divide()
}

func capturedCalledAfterGuard(total, limit int) int {
	divide := func() int { return total / limit }
	if limit <= 0 {
		return 0
	}
	return divide()
}

func capturedEscapes(total, limit int) func() int {
	if limit <= 0 {
		return nil
	}
	return func() int { return total / limit } // want `integer division by limit`
}

func capturedUnsafeSecondCall(total, limit int, early bool) int {
	divide := func() int { return total / limit } // want `integer division by limit`
	if early {
		return divide()
	}
	if limit <= 0 {
		return 0
	}
	return divide()
}

func capturedOtherParams(total, limit int, mode string) int {
	if mode == "" || limit <= 0 {
		return 0
	}
	divide := func() int {
		println(mode)
		return total / limit
	}
	return divide()
}
