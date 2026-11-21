package hook

import "net/http"

// WhereClause is a Before read hook's predicate; SQL is used verbatim, so caller values go in Args behind $N.
type WhereClause struct {
	SQL  string
	Args []any
}

// ListPayload is the data of BeforeList (AddWhere scopes it) and AfterList (Results; rows may be edited or redacted in place, the row count must not change).
type ListPayload struct {
	Request *http.Request
	Where   []WhereClause
	Results []map[string]any
}

// AddWhere appends a parenthesised clause; its $N are renumbered in order, one argument each, never reused.
func (p *ListPayload) AddWhere(sql string, args ...any) {
	p.Where = append(p.Where, WhereClause{SQL: sql, Args: args})
}

// GetPayload is the data of BeforeGet (AddWhere scopes the lookup and record writes, a miss is 404) and AfterGet (Result).
type GetPayload struct {
	Request *http.Request
	ID      string
	Where   []WhereClause
	Result  map[string]any
}

// AddWhere appends a parameterised clause, as ListPayload.AddWhere.
func (p *GetPayload) AddWhere(sql string, args ...any) {
	p.Where = append(p.Where, WhereClause{SQL: sql, Args: args})
}
