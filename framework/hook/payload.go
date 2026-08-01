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
//
// REDACTION: masking a field here changes what the caller reads, not what
// the database filtered and sorted on. The stored value is still a live
// column, so ?field_like=… and ?sort=field recover it from which rows come
// back and in what order, while every response shows the mask. Mark such a
// field NoQuery in its schema.Field so the query surface refuses it. See
// framework/crud/redaction_oracle_security_test.go.
//
// Masking here covers every HTTP path that returns the row: List, Get,
// keyset pages, ?include= children (via the child entity's own hooks),
// _events deliveries, and create/update response bodies. Register the same
// mask on AfterGet too — each path runs the hook matching the shape it
// serves, so a to-one ?include= runs the child's AfterGet, the way its own
// GET /child/{id} route does.
//
// The in-process Go API returns stored values unless the caller passes
// crud.WithReadHooks, so read-modify-write still works. On an ?include=
// payload a hook may not change the row count — each row is already keyed to
// its parent — though sorting Results is harmless there (rows are matched by
// primary key, so order is free; keep the id when projecting).
// ?stream=true refuses rather than bypass.
// A value that must never leave the server raw belongs in a Hidden field,
// which is enforced in the projection. See the hook-skip matrix in
// framework/docs/content/hooks-and-transactions.md.
type ListPayload struct {
	Request *http.Request
	Where   []WhereClause
	Results []map[string]any
}

// AddWhere appends a parameterised WHERE clause. Use $1, $2, … placeholders,
// one per argument, in order: when the clause is composed into the final
// query its placeholders are renumbered positionally (by encounter), so
// pass exactly one argument per placeholder token and do NOT reuse a number
// as a back-reference to an earlier bind — write the value twice if you need
// it twice. The clause is parenthesised when composed, so OR/AND inside it
// cannot leak past framework-injected scopes, and a $N appearing inside a
// single-quoted string literal is treated as data and left untouched.
func (p *ListPayload) AddWhere(sql string, args ...any) {
	p.Where = append(p.Where, WhereClause{SQL: sql, Args: args})
}

// GetPayload is the data argument passed to BeforeGet and AfterGet hooks.
//
// BeforeGet: Request and ID are populated, Where starts empty, Result is nil.
// Hooks call AddWhere() to scope the lookup (mismatches → 404).
//
// AfterGet: Request, ID, and Result are populated; Where is no longer applied.
// Hooks may mutate Result in place to redact / transform. The redaction
// warning on ListPayload applies here too: mark a masked field NoQuery, or
// the List surface still filters and sorts on the stored value.
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
