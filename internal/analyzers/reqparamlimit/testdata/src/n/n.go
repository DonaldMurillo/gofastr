package n

// Negative fixtures: sanctioned postures stay silent.

type hit struct{ Topic string }

func searchDB(term string, limit int) []hit { return nil }

const defaultLimit = 50

func sized(size int, factor int) int { return size * factor }

func upper(term string) string { return term }

type config struct {
	maxHits int
	Limit   int
}

// clampedLowerUpper: the ledger's canonical clamp — constant AND max*
// identifier comparisons before use.
func clampedLowerUpper(params map[string]any, term string, maxHits int) []hit {
	limit := 0
	switch v := params["limit"].(type) {
	case int:
		limit = v
	case float64:
		limit = int(v)
	}
	if limit <= 0 || limit > maxHits {
		limit = maxHits
	}
	return searchDB(term, limit)
}

// clampedConstant: comparison against a plain constant.
func clampedConstant(params map[string]any, term string) []hit {
	limit, _ := params["limit"].(int)
	if limit > 100 {
		limit = 100
	}
	return searchDB(term, limit)
}

// clampedSelector: bound is a max* field selector.
func clampedSelector(params map[string]any, term string, c config) []hit {
	limit, _ := params["limit"].(int)
	if limit > c.maxHits {
		limit = c.maxHits
	}
	return searchDB(term, limit)
}

// guarded: a reject-guard counts as a clamp too.
func guarded(params map[string]any, term string, maxHits int) []hit {
	limit, _ := params["limit"].(int)
	if limit > maxHits {
		return nil
	}
	return searchDB(term, limit)
}

// minClamp: builtin min clamps in the assigning expression.
func minClamp(params map[string]any, term string, maxHits int) []hit {
	limit := min(params["limit"].(int), maxHits)
	return searchDB(term, limit)
}

// constantSource: limit from a constant, not a request map.
func constantSource(term string) []hit {
	limit := defaultLimit
	return searchDB(term, limit)
}

// configStructSource: limit from a config struct field.
func configStructSource(c config, term string) []hit {
	return searchDB(term, c.Limit)
}

// typedMapSource: map[string]int is not the decoded-params shape.
func typedMapSource(m map[string]int, term string) []hit {
	return searchDB(term, m["limit"])
}

// cleanReassign: overwritten from a clean source before use.
func cleanReassign(params map[string]any, term string) []hit {
	limit, _ := params["limit"].(int)
	limit = defaultLimit
	return searchDB(term, limit)
}

// termParam / boolParam: extraction feeding non-limit parameters.
func termParam(params map[string]any) string {
	term, _ := params["term"].(string)
	return upper(term)
}

func boolParam(params map[string]any, verbose bool) bool {
	b, _ := params["limit"].(bool)
	return b && verbose
}

// wrongKey: key outside the name set; params["term"] never taints.
func wrongKey(params map[string]any, term string) []hit {
	n, _ := params["term"].(int)
	return searchDB(term, n)
}

// nonLimitNamedParams: callee parameters named size/factor.
func nonLimitNamedParams(params map[string]any, term string) int {
	limit, _ := params["limit"].(int)
	return sized(3, limit)
}

// unresolvedNotLast: func value with unnamed signature parameters; the
// tainted argument is not final, and unnamed parameters match nothing.
func unresolvedNotLast(params map[string]any) int {
	var f func(int, int) int
	f = func(a, b int) int { return a + b }
	limit, _ := params["limit"].(int)
	return f(limit, 7)
}

// branchLocalExtraction: taint learned inside a branch does not escape.
func branchLocalExtraction(params map[string]any, term string, fast bool) []hit {
	var limit int
	if fast {
		limit, _ = params["limit"].(int)
	}
	return searchDB(term, limit)
}

// switchCaseClamp: expression-switch case comparison clamps.
func switchCaseClamp(params map[string]any, term string, maxHits int) []hit {
	limit, _ := params["limit"].(int)
	switch {
	case limit > maxHits:
		limit = maxHits
	}
	return searchDB(term, limit)
}
