package crud

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"reflect"
	"strings"
	"time"

	"github.com/DonaldMurillo/gofastr/core/query"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/internal/casing"
	"github.com/DonaldMurillo/gofastr/internal/jsonexact"
)

// TypedQuery is a fluent query over a CrudHandler's table that decodes
// rows into []*T, under the same scopes and includes as the HTTP reads.
type TypedQuery[T any] struct {
	handler       *CrudHandler
	wheres        []entity.Condition
	orders        []entity.Order
	limit         *int
	offset        *int
	includes      []string
	nestedFilters []NestedFilter
}

// NewTypedQuery starts a new query against the handler's entity.
func NewTypedQuery[T any](h *CrudHandler) *TypedQuery[T] {
	return &TypedQuery[T]{handler: h}
}

// Where appends an AND condition. Multiple Where calls AND together.
func (q *TypedQuery[T]) Where(c entity.Condition) *TypedQuery[T] {
	q.wheres = append(q.wheres, c)
	return q
}

// WhereNested appends relation-scoped nested filters (e.g. relation "author.profile", field "bio", op OpLike, value "Alice").
func (q *TypedQuery[T]) WhereNested(filters ...NestedFilter) *TypedQuery[T] {
	q.nestedFilters = append(q.nestedFilters, filters...)
	return q
}

// Order appends an ORDER BY clause; the first call is the primary sort.
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

func (q *TypedQuery[T]) buildSelect(ctx context.Context) (*query.QueryBuilder, error) {
	if err := q.handler.refuseLegacyTimestampConditions(ctx, q.wheres); err != nil {
		return nil, err
	}
	cols := q.handler.visibleFields()
	qb := query.Select(cols...).From(q.handler.Entity.GetTable())
	for _, c := range q.wheres {
		c.Apply(qb)
	}
	for _, o := range q.orders {
		o.Apply(qb)
	}
	if len(q.nestedFilters) > 0 {
		nested, err := resolveNestedFilters(q.handler.Entity, q.handler.Registry, q.nestedFilters)
		if err != nil {
			return nil, err
		}
		if err := q.handler.scopeNestedFiltersInProcess(ctx, nested); err != nil {
			return nil, err
		}
		applyNestedFilters(func(sql string, args ...any) { qb.Where(sql, args...) }, q.handler.Entity.GetTable(), q.handler.keyCols(), nested)
	}
	if q.limit != nil {
		qb.Limit(*q.limit)
	}
	if q.offset != nil {
		for _, k := range q.handler.keyCols() {
			qb.Order(k, "ASC")
		}
		qb.Offset(*q.offset)
	}
	req := syntheticRequest(ctx, "GET", "/")
	q.handler.ApplyTenantScope(qb, req)
	q.handler.ApplyOwnerScope(qb, req)
	q.handler.ApplyReadScope(qb, req)
	q.handler.ApplySoftDeleteFilter(qb, req)
	return qb, nil
}

func (q *TypedQuery[T]) requireTenantCtx(ctx context.Context) error {
	return q.handler.requireTenantContext(ctx)
}

func (q *TypedQuery[T]) findRaw(ctx context.Context) ([]map[string]any, *http.Request, error) {
	if err := q.requireTenantCtx(ctx); err != nil {
		return nil, nil, err
	}
	qb, err := q.buildSelect(ctx)
	if err != nil {
		return nil, nil, err
	}
	req := syntheticRequest(ctx, http.MethodGet, "/")
	hookWheres, err := q.handler.runBeforeList(ctx, req)
	if err != nil {
		return nil, nil, err
	}
	for _, c := range hookWheres {
		qb.Where(c.SQL, c.Args...)
	}
	sqlStr, args := qb.Build()
	rows, err := q.exec(ctx).QueryContext(ctx, sqlStr, args...)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	cols := q.handler.visibleFields()
	raw, err := q.handler.scanMany(rows, cols)
	if err != nil {
		return nil, nil, err
	}

	if len(q.includes) > 0 {
		nodes, err := buildIncludeNodesFromNames(q.handler.Entity, q.handler.Registry, q.includes)
		if err != nil {
			return nil, nil, err
		}
		if err := q.handler.applyIncludeTree(ctx, raw, nodes); err != nil {
			return nil, nil, fmt.Errorf("include: %w", err)
		}
	}
	return raw, req, nil
}

func decodeRows[T any](raw []map[string]any) ([]*T, error) {
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

// Find executes the query and decodes results into []*T. Under
// WithReadHooks the AfterList chain runs first, as in ListAll.
func (q *TypedQuery[T]) Find(ctx context.Context) ([]*T, error) {
	raw, req, err := q.findRaw(ctx)
	if err != nil {
		return nil, err
	}
	raw, err = q.handler.runAfterList(ctx, req, raw)
	if err != nil {
		return nil, err
	}
	return decodeRows[T](raw)
}

// First runs the query with Limit(1) and returns the lone result, or
// sql.ErrNoRows. Under WithReadHooks it runs AfterGet, as GetOne does.
func (q *TypedQuery[T]) First(ctx context.Context) (*T, error) {
	prevLimit := q.limit
	defer func() { q.limit = prevLimit }()
	one := 1
	q.limit = &one
	raw, req, err := q.findRaw(ctx)
	if err != nil {
		return nil, err
	}
	if len(raw) == 0 {
		return nil, sql.ErrNoRows
	}
	row := raw[0]
	id, _ := q.handler.rowID(row)
	row, err = q.handler.runAfterGet(ctx, req, id, row)
	if err != nil {
		return nil, err
	}
	var v T
	if err := unmarshalRowToStruct(row, &v); err != nil {
		return nil, err
	}
	return &v, nil
}

// Count returns the number of rows the query matches, ignoring Limit, Offset and Order.
func (q *TypedQuery[T]) Count(ctx context.Context) (int, error) {
	if err := q.requireTenantCtx(ctx); err != nil {
		return 0, err
	}
	if err := q.handler.refuseLegacyTimestampConditions(ctx, q.wheres); err != nil {
		return 0, err
	}
	cb := query.Count(q.handler.Entity.GetTable())
	for _, c := range q.wheres {
		cb.Where(c.SQL(), c.Args()...)
	}
	if len(q.nestedFilters) > 0 {
		nested, err := resolveNestedFilters(q.handler.Entity, q.handler.Registry, q.nestedFilters)
		if err != nil {
			return 0, err
		}
		if err := q.handler.scopeNestedFiltersInProcess(ctx, nested); err != nil {
			return 0, err
		}
		applyNestedFilters(func(sql string, args ...any) { cb.Where(sql, args...) }, q.handler.Entity.GetTable(), q.handler.keyCols(), nested)
	}
	req := syntheticRequest(ctx, "GET", "/")
	q.handler.ApplyTenantScopeCount(cb, req)
	q.handler.ApplyOwnerScopeCount(cb, req)
	q.handler.ApplyReadScopeCount(cb, req)
	q.handler.ApplySoftDeleteFilterCount(cb, req)
	hookWheres, err := q.handler.runBeforeList(ctx, req)
	if err != nil {
		return 0, err
	}
	for _, c := range hookWheres {
		cb.Where(c.SQL, c.Args...)
	}
	sqlStr, args := cb.Build()
	var n int
	if err := q.exec(ctx).QueryRowContext(ctx, sqlStr, args...).Scan(&n); err != nil {
		return 0, err
	}
	return n, nil
}

// UnmarshalEntity decodes a snake-cased row map into dest, a struct with
// camelCase json tags.
func UnmarshalEntity(m map[string]any, dest any) error {
	return unmarshalRowToStruct(m, dest)
}

// MarshalEntity encodes src into the snake-cased map the CRUD writes take,
// leaving out omitempty fields that hold their zero value.
func MarshalEntity(src any) (map[string]any, error) {
	return marshalStructToRow(src)
}

func unmarshalRowToStruct(m map[string]any, dest any) error {
	camel := casing.MapToCamel(m)
	if t := reflect.TypeOf(dest); t != nil {
		camel, _ = widenDates(camel, t).(map[string]any)
	}
	b, err := json.Marshal(camel)
	if err != nil {
		return err
	}
	return jsonexact.Decode(b, dest, func(n json.Number) (any, error) {
		return storedJSONNumbers(n), nil
	})
}

var timeType = reflect.TypeOf(time.Time{})

// widenDates spells each YYYY-MM-DD string t decodes into a time.Time as RFC 3339 midnight.
func widenDates(v any, t reflect.Type) any {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	switch {
	case t == timeType:
		if s, ok := v.(string); ok {
			if d, err := time.Parse(time.DateOnly, s); err == nil {
				return d.Format(time.RFC3339)
			}
		}
	case t.Kind() == reflect.Struct:
		if m, ok := v.(map[string]any); ok {
			return widenStructDates(m, t)
		}
	case t.Kind() == reflect.Slice || t.Kind() == reflect.Array:
		switch s := v.(type) {
		case []any:
			out := make([]any, len(s))
			for i, item := range s {
				out[i] = widenDates(item, t.Elem())
			}
			return out
		case []map[string]any:
			out := make([]any, len(s))
			for i, item := range s {
				out[i] = widenDates(item, t.Elem())
			}
			return out
		}
	}
	return v
}

func widenStructDates(m map[string]any, t reflect.Type) map[string]any {
	out := m
	copied := false
	seen := make(map[reflect.Type]bool)
	var walk func(t reflect.Type)
	walk = func(t reflect.Type) {
		if seen[t] {
			return
		}
		seen[t] = true
		for i := range t.NumField() {
			f := t.Field(i)
			name, _, _ := strings.Cut(f.Tag.Get("json"), ",")
			if name == "-" {
				continue
			}
			ft := f.Type
			for ft.Kind() == reflect.Pointer {
				ft = ft.Elem()
			}
			if f.Anonymous && name == "" && ft.Kind() == reflect.Struct {
				walk(ft)
				continue
			}
			if !f.IsExported() {
				continue
			}
			switch ft.Kind() {
			case reflect.Struct, reflect.Slice, reflect.Array:
			default:
				continue
			}
			if name == "" {
				name = f.Name
			}
			for k, item := range out {
				if !strings.EqualFold(k, name) {
					continue
				}
				if _, isText := item.(string); !isText && ft == timeType {
					continue
				}
				widened := widenDates(item, f.Type)
				if s, ok := widened.(string); ok && s == item {
					continue
				}
				if !copied {
					out, copied = maps.Clone(m), true
				}
				out[k] = widened
			}
		}
	}
	walk(t)
	return out
}

func marshalStructToRow(src any) (map[string]any, error) {
	b, err := json.Marshal(src)
	if err != nil {
		return nil, err
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	var m map[string]any
	if err := dec.Decode(&m); err != nil {
		return nil, err
	}
	normalizeJSONNumbers(m)
	return casing.MapToSnake(m), nil
}

// Exists reports whether at least one row matches the query.
func (q *TypedQuery[T]) Exists(ctx context.Context) (bool, error) {
	n, err := q.Count(ctx)
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// UpdateAll sets fields on every live row the query matches, under the
// tenant and owner scopes but not read scopes, ignoring Limit, Offset and
// Order, and returns the rows touched. fields is keyed as CreateOne reads
// a body and is not modified. A body that sets no writable field is an
// error. Replaced stored objects are released after commit.
func (q *TypedQuery[T]) UpdateAll(ctx context.Context, fields map[string]any) (int, error) {
	if err := q.requireTenantCtx(ctx); err != nil {
		return 0, err
	}
	if len(fields) == 0 {
		return 0, fmt.Errorf("UpdateAll: no fields to set")
	}
	if err := q.handler.refuseLegacyTimestampConditions(ctx, q.wheres); err != nil {
		return 0, err
	}
	body := maps.Clone(fields)
	if err := q.handler.bodyColumns(body); err != nil {
		return 0, err
	}
	if err := q.handler.checkStateBulk(body); err != nil {
		return 0, err
	}

	if err := q.handler.validateMediaURLs(body); err != nil {
		return 0, err
	}
	if err := q.handler.checkMediaProvenance(ctx, body, nil); err != nil {
		return 0, err
	}

	if err := q.handler.coerceNumberColumnValues(body); err != nil {
		return 0, err
	}

	vr := schema.ValidatePartial(q.handler.entitySchema(), body)
	if !vr.Valid {
		return 0, &ValidationError{fields: vr.Errors}
	}
	if err := q.handler.refuseInexactDecimals(ctx, body); err != nil {
		return 0, err
	}

	sets := func(f schema.Field) bool { return q.handler.updateSets(ctx, f, false) }
	if err := q.handler.checkBelongsToScope(ctx, nil, fkWrite{body: body, sets: sets, holds: q.handler.scopedHeld(ctx)}); err != nil {
		return 0, err
	}
	ub := query.Update(q.handler.Entity.GetTable())
	anySet := false
	for _, f := range q.handler.Entity.GetFields() {
		if !sets(f) {
			continue
		}
		val, ok := body[f.Name]
		if !ok {
			continue
		}
		ub.Set(f.Name, q.handler.bindJSONValue(f.Name, val))
		anySet = true
	}
	if col := autoUpdatedAtColumn(q.handler.Entity); col != "" {
		if val, supplied := body[col]; supplied {
			ub.Set(col, q.handler.bindJSONValue(col, val))
			anySet = true
		} else {
			ub.Set(col, generateFieldValue(schema.AutoTimestamp))
		}
	}
	if !anySet {
		return 0, errNoFieldsToUpdate
	}
	for _, c := range q.wheres {
		ub.Where(c.SQL(), c.Args()...)
	}
	if len(q.nestedFilters) > 0 {
		nested, err := resolveNestedFilters(q.handler.Entity, q.handler.Registry, q.nestedFilters)
		if err != nil {
			return 0, err
		}
		if err := q.handler.scopeNestedFiltersInProcess(ctx, nested); err != nil {
			return 0, err
		}
		applyNestedFilters(func(sql string, args ...any) { ub.Where(sql, args...) }, q.handler.Entity.GetTable(), q.handler.keyCols(), nested)
	}
	if q.handler.Entity.Config.Scope.SoftDelete {
		ub.Where("deleted_at IS NULL")
	}
	req := syntheticRequest(ctx, "PATCH", "/")
	q.handler.ApplyTenantScopeUpdate(ub, req)
	q.handler.ApplyOwnerScopeUpdate(ub, req)

	if q.handler.Storage != nil && setsMediaColumn(q.handler.mediaFields(), body) {
		return q.handler.updateReturningObjects(ctx, ub)
	}
	sqlStr, args := ub.Build()
	res, err := q.exec(ctx).ExecContext(ctx, sqlStr, args...)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

func (q *TypedQuery[T]) exec(ctx context.Context) DBExecutor {
	return q.handler.conn(ctx)
}

// DeleteAll removes every row matching the current Where chain. For
// SoftDelete entities, sets deleted_at instead of issuing a DELETE.
func (q *TypedQuery[T]) DeleteAll(ctx context.Context) (int, error) {
	if err := q.requireTenantCtx(ctx); err != nil {
		return 0, err
	}
	if err := q.handler.refuseLegacyTimestampConditions(ctx, q.wheres); err != nil {
		return 0, err
	}
	req := syntheticRequest(ctx, "DELETE", "/")
	if q.handler.Entity.Config.Scope.SoftDelete {
		ub := query.Update(q.handler.Entity.GetTable()).
			Set("deleted_at", time.Now().UTC())
		for _, c := range q.wheres {
			ub.Where(c.SQL(), c.Args()...)
		}
		if len(q.nestedFilters) > 0 {
			nested, err := resolveNestedFilters(q.handler.Entity, q.handler.Registry, q.nestedFilters)
			if err != nil {
				return 0, err
			}
			if err := q.handler.scopeNestedFiltersInProcess(ctx, nested); err != nil {
				return 0, err
			}
			applyNestedFilters(func(sql string, args ...any) { ub.Where(sql, args...) }, q.handler.Entity.GetTable(), q.handler.keyCols(), nested)
		}
		ub.Where("deleted_at IS NULL")
		q.handler.ApplyTenantScopeUpdate(ub, req)
		q.handler.ApplyOwnerScopeUpdate(ub, req)
		sqlStr, args := ub.Build()
		res, err := q.exec(ctx).ExecContext(ctx, sqlStr, args...)
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
	if len(q.nestedFilters) > 0 {
		nested, err := resolveNestedFilters(q.handler.Entity, q.handler.Registry, q.nestedFilters)
		if err != nil {
			return 0, err
		}
		if err := q.handler.scopeNestedFiltersInProcess(ctx, nested); err != nil {
			return 0, err
		}
		applyNestedFilters(func(sql string, args ...any) { db.Where(sql, args...) }, q.handler.Entity.GetTable(), q.handler.keyCols(), nested)
	}
	q.handler.ApplyTenantScopeDelete(db, req)
	q.handler.ApplyOwnerScopeDelete(db, req)
	return q.handler.deleteReturningObjects(ctx, db)
}

// IsNotFound reports whether err is sql.ErrNoRows or a typed update or
// delete that matched no row.
func IsNotFound(err error) bool {
	return errors.Is(err, sql.ErrNoRows) || errors.Is(err, errNotFound)
}
