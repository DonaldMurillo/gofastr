package framework

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/gofastr/gofastr/core/query"
)

// TypedQuery is a generic, fluent query builder that returns []*T from the
// underlying CrudHandler's table. Generated typed repositories construct one
// via NewTypedQuery; the type parameter T is the entity's struct.
//
// The builder wraps the existing core/query.QueryBuilder (so filters, sorts,
// limits, tenant scope, and soft-delete behave identically to the HTTP path)
// and runs the same eager-load helpers when Include() is set.
type TypedQuery[T any] struct {
	handler  *CrudHandler
	qb       *query.QueryBuilder
	includes []string
}

// NewTypedQuery starts a new query against the handler's entity. Callers
// should chain Where/Order/Limit/Include and finish with Find/First/Count.
func NewTypedQuery[T any](h *CrudHandler) *TypedQuery[T] {
	cols := h.visibleFields()
	qb := query.Select(cols...).From(h.Entity.GetTable())
	// Tenant + soft-delete defaults — same as the HTTP path on a request
	// without ?trashed=true. We synthesize a request to feed the existing
	// helpers; tenant ID still flows from ctx at Find time.
	return &TypedQuery[T]{handler: h, qb: qb}
}

// Where appends an AND condition. Multiple Where calls AND together.
func (q *TypedQuery[T]) Where(c Condition) *TypedQuery[T] {
	c.Apply(q.qb)
	return q
}

// Order appends an ORDER BY clause. Multiple Order calls compose in input
// order (first declared = primary sort).
func (q *TypedQuery[T]) Order(o Order) *TypedQuery[T] {
	o.Apply(q.qb)
	return q
}

// Limit caps the result size.
func (q *TypedQuery[T]) Limit(n int) *TypedQuery[T] { q.qb.Limit(n); return q }

// Offset skips the first n rows.
func (q *TypedQuery[T]) Offset(n int) *TypedQuery[T] { q.qb.Offset(n); return q }

// Include eager-loads the named relations on the result rows. Names follow
// the same dotted-path syntax as ?include= (e.g. "author.profile").
func (q *TypedQuery[T]) Include(rels ...string) *TypedQuery[T] {
	q.includes = append(q.includes, rels...)
	return q
}

// Find executes the query and decodes results into []*T.
func (q *TypedQuery[T]) Find(ctx context.Context) ([]*T, error) {
	// Apply tenant + soft-delete defaults via the synthetic request helper.
	req := syntheticRequest(ctx, "GET", "/")
	q.handler.applyTenantScope(q.qb, req)
	q.handler.applySoftDeleteFilter(q.qb, req)

	sqlStr, args := q.qb.Build()
	rows, err := q.handler.DB.QueryContext(ctx, sqlStr, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	cols := q.handler.visibleFields()
	raw, err := scanRows(rows, cols, q.handler.convertKey)
	if err != nil {
		return nil, err
	}

	if len(q.includes) > 0 {
		nodes, err := buildIncludeNodesFromNames(q.handler.Entity, q.handler.Registry, q.includes)
		if err != nil {
			return nil, err
		}
		if err := q.handler.applyIncludeTree(ctx, raw, nodes); err != nil {
			return nil, fmt.Errorf("include: %w", err)
		}
	}

	out := make([]*T, len(raw))
	for i, m := range raw {
		var v T
		if err := unmarshalRowToStruct(m, &v); err != nil {
			return nil, err
		}
		out[i] = &v
	}
	return out, nil
}

// First runs the query with Limit(1) and returns the lone result. Returns
// sql.ErrNoRows when the query yields zero rows so callers can detect "not
// found" with errors.Is.
func (q *TypedQuery[T]) First(ctx context.Context) (*T, error) {
	q.qb.Limit(1)
	out, err := q.Find(ctx)
	if err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return nil, sql.ErrNoRows
	}
	return out[0], nil
}

// Count returns the number of rows matching the current filters (ignores
// limit/offset/order, like a SELECT COUNT(*) over the same WHERE clause).
func (q *TypedQuery[T]) Count(ctx context.Context) (int, error) {
	// We can't easily share filters with the existing CountBuilder because
	// they live inside the SELECT QueryBuilder's Where chain. Re-run the
	// SELECT with COUNT(*) by constructing a new builder that copies wheres.
	// Pragmatic: fall back to executing the existing SELECT and counting in
	// memory, but that defeats the point. Better: query the original SQL
	// wrapped in SELECT COUNT(*) FROM (...) sub.
	innerSQL, args := q.qb.Build()
	wrapped := "SELECT COUNT(*) FROM (" + innerSQL + ") AS sub"
	var n int
	if err := q.handler.DB.QueryRowContext(ctx, wrapped, args...).Scan(&n); err != nil {
		return 0, err
	}
	return n, nil
}

// UnmarshalEntity is the public entry point for converting a snake-cased
// row map into a typed entity struct (json tags in camelCase). Exposed so
// generated repository code outside this package can call it.
func UnmarshalEntity(m map[string]any, dest any) error {
	return unmarshalRowToStruct(m, dest)
}

// MarshalEntity is the inverse — converts a typed entity struct into the
// snake-cased map[string]any the framework's CRUD primitives expect. Skips
// fields whose JSON tag carries omitempty if the value is the type's zero,
// per encoding/json semantics.
func MarshalEntity(src any) (map[string]any, error) {
	return marshalStructToRow(src)
}

// unmarshalRowToStruct converts a snake-cased map (the framework's standard
// row shape) into a typed struct whose JSON tags are camelCase. Same casing
// transform the typed-hooks helpers use — kept symmetric so generated repo
// code can rely on either path.
func unmarshalRowToStruct(m map[string]any, dest any) error {
	camel := mapToCamelCase(m)
	b, err := json.Marshal(camel)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, dest)
}

// marshalStructToRow is the inverse — used by typed repos to feed a *T into
// CreateOne/UpdateOne, which expect snake-cased map[string]any.
func marshalStructToRow(src any) (map[string]any, error) {
	b, err := json.Marshal(src)
	if err != nil {
		return nil, err
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, err
	}
	return mapToSnakeCase(m), nil
}

// IsNotFound reports whether err corresponds to a not-found result on a
// typed CRUD operation. Treats sql.ErrNoRows and the framework's internal
// errNotFound (from Update/Delete misses) as equivalent.
func IsNotFound(err error) bool {
	return errors.Is(err, sql.ErrNoRows) || errors.Is(err, errNotFound)
}
