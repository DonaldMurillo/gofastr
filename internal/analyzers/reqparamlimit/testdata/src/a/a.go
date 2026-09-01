package a

// Positive fixtures: one want-comment per unclamped request-sourced
// limit reaching a limit-shaped parameter.

type hit struct{ Topic string }

// searchDB mirrors docs.SearchWithLimit(term string, limit int).
func searchDB(term string, limit int) []hit {
	return []hit{{Topic: term}}
}

func takeN(top int) int { return top }

func pick(prefix string, take ...int) int {
	if len(take) == 0 {
		return 0
	}
	return take[0]
}

// seedSwitch replicates framework/mcp_introspection.go toolDocsSearch:
// type switch over params["limit"], every clause forwarding into limit.
func seedSwitch(params map[string]any, term string) []hit {
	limit := 0
	switch v := params["limit"].(type) {
	case int:
		limit = v
	case int64:
		limit = int(v)
	case float64:
		limit = int(v)
	}
	return searchDB(term, limit) // want `reqparamlimit: request-sourced params\["limit"\] passed unclamped to searchDB's "limit" parameter`
}

// plainAssert: comma-ok type assertion extraction.
func plainAssert(params map[string]any, term string) []hit {
	limit, _ := params["limit"].(int)
	return searchDB(term, limit) // want `reqparamlimit: request-sourced params\["limit"\] passed unclamped to searchDB's "limit" parameter`
}

// otherKeyFlow: hits key, taint surviving a conversion and reassignment.
func otherKeyFlow(args map[string]any) []hit {
	raw, _ := args["hits"].(float64)
	n := int(raw)
	return searchDB("t", n) // want `reqparamlimit: request-sourced params\["hits"\] passed unclamped to searchDB's "limit" parameter`
}

// inlineAssert: extraction directly in the argument.
func inlineAssert(params map[string]any, term string) []hit {
	return searchDB(term, params["limit"].(int)) // want `reqparamlimit: request-sourced params\["limit"\] passed unclamped to searchDB's "limit" parameter`
}

// aliasedMap: the params map arrives through a type alias — map type
// identity, not spelling, is what the lane sees.
type Params = map[string]any

func aliasedMap(p Params, term string) []hit {
	limit, _ := p["max"].(int)
	return searchDB(term, limit) // want `reqparamlimit: request-sourced params\["max"\] passed unclamped to searchDB's "limit" parameter`
}

// topNamedParam: callee parameter named top, key top.
func topNamedParam(params map[string]any) int {
	t, _ := params["top"].(int)
	return takeN(t) // want `reqparamlimit: request-sourced params\["top"\] passed unclamped to takeN's "top" parameter`
}

// underscoreKey: page_size matches page_?size.
func underscoreKey(q map[string]any, term string) []hit {
	n, _ := q["page_size"].(float64)
	return searchDB(term, int(n)) // want `reqparamlimit: request-sourced params\["page_size"\] passed unclamped to searchDB's "limit" parameter`
}

// variadicSink: variadic parameter named take.
func variadicSink(params map[string]any) int {
	n, _ := params["take"].(int)
	return pick("x", n) // want `reqparamlimit: request-sourced params\["take"\] passed unclamped to pick's "take" parameter`
}

// softBound: compared against a plain identifier (neither constant nor
// max*) — not a clamp under the documented heuristic.
func softBound(params map[string]any, term string, def int) []hit {
	limit, _ := params["limit"].(int)
	if limit < def {
		limit = def
	}
	return searchDB(term, limit) // want `reqparamlimit: request-sourced params\["limit"\] passed unclamped to searchDB's "limit" parameter`
}

// branchLocalClamp: the clamp only runs on one path, so the straight
// line stays unclamped.
func branchLocalClamp(params map[string]any, term string) []hit {
	limit, _ := params["limit"].(int)
	if term == "" {
		if limit > 100 {
			limit = 100
		}
	}
	return searchDB(term, limit) // want `reqparamlimit: request-sourced params\["limit"\] passed unclamped to searchDB's "limit" parameter`
}
