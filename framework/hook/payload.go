package hook

import "net/http"

// WhereClause is an editable SQL predicate that BeforeList / BeforeGet
// hooks can append to scope read queries (e.g. inject WHERE user_id = $1).
// CRUD applies appended clauses to the data query — and, for List, also
// to the count query — so totals reflect the filtered result.
//
// SECURITY: SQL is appended VERBATIM to the query. Never concatenate
// caller-controlled values into SQL — always use placeholders ($1, $2,
// …) and pass values as Args. The framework's query builder takes care
// of parameter binding; user code that bypasses this is the source of
// every SQL-injection bug a hook can introduce.
//
//	// SAFE: parameterised binding
//	p.AddWhere("status = $1", "published")
//
//	// UNSAFE: string concatenation
//	p.AddWhere("status = '" + userInput + "'") // SQL INJECTION
type WhereClause struct {
	SQL  string
	Args []any
}

// ListPayload is the data argument passed to BeforeList and AfterList hooks.
//
// BeforeList: Request is non-nil, Where starts empty, Results is nil.
// Hooks call AddWhere() to attach scope filters.
//
// AfterList: Request and Results are non-nil, Where is no longer applied.
// Hooks may mutate Results in place (redact fields, drop rows, etc.).
type ListPayload struct {
	Request *http.Request
	Where   []WhereClause
	Results []map[string]any
}

// AddWhere appends a parameterised WHERE clause. Use $1, $2, … placeholders
// to match the query builder's PostgreSQL-style binding.
func (p *ListPayload) AddWhere(sql string, args ...any) {
	p.Where = append(p.Where, WhereClause{SQL: sql, Args: args})
}

// GetPayload is the data argument passed to BeforeGet and AfterGet hooks.
//
// BeforeGet: Request and ID are populated, Where starts empty, Result is nil.
// Hooks call AddWhere() to scope the lookup (mismatches → 404).
//
// AfterGet: Request, ID, and Result are populated; Where is no longer applied.
// Hooks may mutate Result in place to redact / transform.
type GetPayload struct {
	Request *http.Request
	ID      string
	Where   []WhereClause
	Result  map[string]any
}

// AddWhere appends a parameterised WHERE clause. Use $1, $2, … placeholders.
func (p *GetPayload) AddWhere(sql string, args ...any) {
	p.Where = append(p.Where, WhereClause{SQL: sql, Args: args})
}
