package crud

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/DonaldMurillo/gofastr/core/query"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/internal/casing"
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
	wheres   []entity.Condition
	orders   []entity.Order
	limit    *int
	offset   *int
	includes []string
}

// NewTypedQuery starts a new query against the handler's entity. Callers
// should chain Where/Order/Limit/Include and finish with Find/First/Count.
func NewTypedQuery[T any](h *CrudHandler) *TypedQuery[T] {
	return &TypedQuery[T]{handler: h}
}

// Where appends an AND condition. Multiple Where calls AND together.
func (q *TypedQuery[T]) Where(c entity.Condition) *TypedQuery[T] {
	q.wheres = append(q.wheres, c)
	return q
}

// Order appends an ORDER BY clause. Multiple Order calls compose in input
// order (first declared = primary sort).
func (q *TypedQuery[T]) Order(o entity.Order) *TypedQuery[T] {
	q.orders = append(q.orders, o)
	return q
}

// Limit caps the result size.
func (q *TypedQuery[T]) Limit(n int) *TypedQuery[T] { q.limit = &n; return q }

// Offset skips the first n rows.
func (q *TypedQuery[T]) Offset(n int) *TypedQuery[T] { q.offset = &n; return q }

// Include eager-loads the named relations on the result rows. Names follow
// the same dotted-path syntax as ?include= (e.g. "author.profile").
func (q *TypedQuery[T]) Include(rels ...string) *TypedQuery[T] {
	q.includes = append(q.includes, rels...)
	return q
}

// buildSelect materialises a fresh SELECT QueryBuilder reflecting the
// query's current state. Re-buildable: Find/First/Count call it
// independently so each pass gets its own renumbered placeholders.
func (q *TypedQuery[T]) buildSelect(ctx context.Context) *query.QueryBuilder {
	cols := q.handler.visibleFields()
	qb := query.Select(cols...).From(q.handler.Entity.GetTable())
	for _, c := range q.wheres {
		c.Apply(qb)
	}
	for _, o := range q.orders {
		o.Apply(qb)
	}
	if q.limit != nil {
		qb.Limit(*q.limit)
	}
	if q.offset != nil {
		qb.Offset(*q.offset)
	}
	req := syntheticRequest(ctx, "GET", "/")
	q.handler.ApplyTenantScope(qb, req)
	q.handler.ApplyOwnerScope(qb, req)
	q.handler.ApplySoftDeleteFilter(qb, req)
	return qb
}

// Find executes the query and decodes results into []*T.
func (q *TypedQuery[T]) Find(ctx context.Context) ([]*T, error) {
	qb := q.buildSelect(ctx)
	sqlStr, args := qb.Build()
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
	one := 1
	q.limit = &one
	out, err := q.Find(ctx)
	if err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return nil, sql.ErrNoRows
	}
	return out[0], nil
}

// Count returns the number of rows matching the current filters. Ignores
// limit/offset/order — pure SELECT COUNT(*) over the same WHERE predicate.
func (q *TypedQuery[T]) Count(ctx context.Context) (int, error) {
	cb := query.Count(q.handler.Entity.GetTable())
	for _, c := range q.wheres {
		cb.Where(c.SQL(), c.Args()...)
	}
	req := syntheticRequest(ctx, "GET", "/")
	q.handler.ApplyTenantScopeCount(cb, req)
	q.handler.ApplyOwnerScopeCount(cb, req)
	q.handler.ApplySoftDeleteFilterCount(cb, req)
	sqlStr, args := cb.Build()
	var n int
	if err := q.handler.DB.QueryRowContext(ctx, sqlStr, args...).Scan(&n); err != nil {
		return 0, err
	}
	return n, nil
	// (legacy comment removed — wrapped COUNT was a workaround for the prior
	// design that baked filters straight into the SELECT QueryBuilder. Now
	// that wheres live as Conditions we can build a CountBuilder cleanly.)
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
	camel := casing.MapToCamel(m)
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
	return casing.MapToSnake(m), nil
}

// Exists returns true if at least one row matches the current WHERE chain.
// Cheaper than First+IsNotFound for the "do any match?" question because it
// runs a COUNT(*) with LIMIT 1 internally.
func (q *TypedQuery[T]) Exists(ctx context.Context) (bool, error) {
	n, err := q.Limit(1).Count(ctx)
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// UpdateAll applies the same field updates to every row matching the
// current Where chain and returns the number of rows touched. Ignores
// Limit/Offset/Order — the underlying SQL is a plain UPDATE with the
// same WHERE predicate that Find would use.
//
// Fields are snake-cased map[string]any (the framework's wire shape). For
// type-safe updates, marshal a partial struct with framework.MarshalEntity
// and pass the resulting map.
func (q *TypedQuery[T]) UpdateAll(ctx context.Context, fields map[string]any) (int, error) {
	if len(fields) == 0 {
		return 0, fmt.Errorf("UpdateAll: no fields to set")
	}
	ub := query.Update(q.handler.Entity.GetTable())
	for k, v := range fields {
		// Don't allow callers to mutate the primary key wholesale via bulk
		// update — that's almost always a bug.
		if k == q.handler.PrimaryKey {
			continue
		}
		ub.Set(k, v)
	}
	for _, c := range q.wheres {
		ub.Where(c.SQL(), c.Args()...)
	}
	req := syntheticRequest(ctx, "PATCH", "/")
	q.handler.ApplyTenantScopeUpdate(ub, req)
	q.handler.ApplyOwnerScopeUpdate(ub, req)

	sqlStr, args := ub.Build()
	res, err := q.handler.DB.ExecContext(ctx, sqlStr, args...)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

// DeleteAll removes every row matching the current Where chain. For
// SoftDelete entities, sets deleted_at instead of issuing a DELETE.
func (q *TypedQuery[T]) DeleteAll(ctx context.Context) (int, error) {
	req := syntheticRequest(ctx, "DELETE", "/")
	if q.handler.Entity.Config.SoftDelete {
		ub := query.Update(q.handler.Entity.GetTable()).
			Set("deleted_at", time.Now().UTC())
		for _, c := range q.wheres {
			ub.Where(c.SQL(), c.Args()...)
		}
		q.handler.ApplyTenantScopeUpdate(ub, req)
		q.handler.ApplyOwnerScopeUpdate(ub, req)
		sqlStr, args := ub.Build()
		res, err := q.handler.DB.ExecContext(ctx, sqlStr, args...)
		if err != nil {
			return 0, err
		}
		n, _ := res.RowsAffected()
		return int(n), nil
	}

	db := query.Delete(q.handler.Entity.GetTable())
	for _, c := range q.wheres {
		db.Where(c.SQL(), c.Args()...)
	}
	q.handler.ApplyTenantScopeDelete(db, req)
	q.handler.ApplyOwnerScopeDelete(db, req)
	sqlStr, args := db.Build()
	res, err := q.handler.DB.ExecContext(ctx, sqlStr, args...)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

// IsNotFound reports whether err corresponds to a not-found result on a
// typed CRUD operation. Treats sql.ErrNoRows and the framework's internal
// errNotFound (from Update/Delete misses) as equivalent.
func IsNotFound(err error) bool {
	return errors.Is(err, sql.ErrNoRows) || errors.Is(err, errNotFound)
}
