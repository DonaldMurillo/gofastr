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

// TypedQuery is a generic, fluent query builder that returns []*T from the
// underlying CrudHandler's table. Generated typed repositories construct one
// via NewTypedQuery; the type parameter T is the entity's struct.
//
// The builder wraps the existing core/query.QueryBuilder (so filters, sorts,
// limits, tenant scope, and soft-delete behave identically to the HTTP path)
// and runs the same eager-load helpers when Include() is set.
type TypedQuery[T any] struct {
	handler       *CrudHandler
	wheres        []entity.Condition
	orders        []entity.Order
	limit         *int
	offset        *int
	includes      []string
	nestedFilters []NestedFilter
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

// WhereNested appends relation-scoped nested filters (e.g. relation "author.profile", field "bio", op OpLike, value "Alice").
func (q *TypedQuery[T]) WhereNested(filters ...NestedFilter) *TypedQuery[T] {
	q.nestedFilters = append(q.nestedFilters, filters...)
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
// query's wheres, orders, and nested-filter EXISTS clauses.
func (q *TypedQuery[T]) buildSelect(ctx context.Context) (*query.QueryBuilder, error) {
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
		// Same total order the list paths get (withKeyTiebreak): an
		// offset page over tied rows is otherwise a different slice on
		// each call. entity.Order keeps its column private, so a key the
		// caller already ordered on repeats; a repeated ORDER BY column
		// is a no-op on both dialects.
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

// requireTenantCtx enforces tenant isolation for typed-repo queries: a
// MultiTenant entity must not be queried UNSCOPED on a context carrying no
// tenant id, because ApplyTenantScope silently no-ops on an empty tenant and
// the query would span every tenant (a cross-tenant data leak). Honors
// tenant.AllowCrossTenant for deliberate admin access.
//
// Note: owner scope is deliberately NOT gated here, typed repos are trusted
// in-process code and an owner-less caller (admin/system) legitimately reads
// across owners (see TestTypedQuery_AdminCallerWithoutOwnerSeesAll). Tenant is
// the strict boundary.
func (q *TypedQuery[T]) requireTenantCtx(ctx context.Context) error {
	return q.handler.requireTenantContext(ctx)
}

// findRaw runs the SELECT and attaches any requested includes, returning the
// raw row maps plus the synthetic request the read hooks are handed.
//
// Split out of Find so First can run the AfterGet chain over the row map
// before it is decoded into T, once a row is a struct there is nothing left
// for a map-shaped hook to redact.
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
		// ctx is threaded through unchanged so applyChildReadHooks sees the
		// same opt-in the top-level rows get, an included row must not stay
		// raw while its parent is redacted.
		if err := q.handler.applyIncludeTree(ctx, raw, nodes); err != nil {
			return nil, nil, fmt.Errorf("include: %w", err)
		}
	}
	return raw, req, nil
}

// decodeRows converts scanned row maps into []*T.
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

// Find executes the query and decodes results into []*T.
//
// Under crud.WithReadHooks the AfterList chain runs before decoding, exactly
// as ListAll does it, a typed repo backing a rendered page must not show
// stored values the entity's own list endpoint masks. Without the opt-in the
// rows are stored values, so read-modify-write still round-trips.
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

// First runs the query with Limit(1) and returns the lone result. Returns
// sql.ErrNoRows when the query yields zero rows so callers can detect "not
// found" with errors.Is.
//
// The single-row surface mirrors GET /entity/{id}, so under
// crud.WithReadHooks it runs AfterGet, the same chain GetOne runs, and the
// same one the HTTP handler runs for a by-id read. It deliberately does NOT
// also run AfterList: that would redact more than the entity's own routes do
// and make the two surfaces disagree in the other direction.
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

// Count returns the number of rows matching the current filters. Ignores
// limit/offset/order, pure SELECT COUNT(*) over the same WHERE predicate.
func (q *TypedQuery[T]) Count(ctx context.Context) (int, error) {
	if err := q.requireTenantCtx(ctx); err != nil {
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
	// (legacy comment removed, wrapped COUNT was a workaround for the prior
	// design that baked filters straight into the SELECT QueryBuilder. Now
	// that wheres live as Conditions we can build a CountBuilder cleanly.)
}

// UnmarshalEntity is the public entry point for converting a snake-cased
// row map into a typed entity struct (json tags in camelCase). Exposed so
// generated repository code outside this package can call it.
func UnmarshalEntity(m map[string]any, dest any) error {
	return unmarshalRowToStruct(m, dest)
}

// MarshalEntity is the inverse, converts a typed entity struct into the
// snake-cased map[string]any the framework's CRUD primitives expect. Skips
// fields whose JSON tag carries omitempty if the value is the type's zero,
// per encoding/json semantics.
func MarshalEntity(src any) (map[string]any, error) {
	return marshalStructToRow(src)
}

// unmarshalRowToStruct converts a snake-cased map (the framework's standard
// row shape) into a typed struct whose JSON tags are camelCase. Same casing
// transform the typed-hooks helpers use, kept symmetric so generated repo
// code can rely on either path. A number under an `any` field reads as the
// stored JSON column read gives it (storedJSONNumbers): json.Unmarshal
// read 9007199254740993 as 9007199254740992, and a MarshalEntity then
// UpdateOne wrote the rounded value back.
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

// widenDates returns v with every YYYY-MM-DD string that t decodes into a
// time.Time spelled as that date's RFC 3339 midnight. A Date column reads
// back as YYYY-MM-DD (dateText), which time.Time's UnmarshalJSON refuses,
// so a struct holding a Date in a time.Time failed to decode. Maps and
// slices it changes are copies: the caller's row is left as it was.
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

// widenStructDates is widenDates for the object a struct decodes from,
// matching keys to fields the way encoding/json does (tag name or field
// name, case-insensitively; embedded structs promote their fields).
func widenStructDates(m map[string]any, t reflect.Type) map[string]any {
	out := m
	copied := false
	var walk func(t reflect.Type)
	walk = func(t reflect.Type) {
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
				continue // nothing under it can be a time.Time
			}
			if name == "" {
				name = f.Name
			}
			for k, item := range out {
				if !strings.EqualFold(k, name) {
					continue
				}
				if _, isText := item.(string); !isText && ft == timeType {
					continue // already a time.Time, or null
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

// marshalStructToRow is the inverse, used by typed repos to feed a *T into
// CreateOne/UpdateOne, which expect snake-cased map[string]any.
func marshalStructToRow(src any) (map[string]any, error) {
	b, err := json.Marshal(src)
	if err != nil {
		return nil, err
	}
	// UseNumber + normalize so an int64 field above 2^53 (a snowflake id
	// on a typed entity) survives the round trip; a float64 decode here
	// is the same silent-rounding bug the HTTP body path refuses.
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	var m map[string]any
	if err := dec.Decode(&m); err != nil {
		return nil, err
	}
	normalizeJSONNumbers(m)
	return casing.MapToSnake(m), nil
}

// Exists returns true if at least one row matches the current WHERE chain.
// Cheaper than First+IsNotFound for the "do any match?" question because it
// runs a COUNT(*) internally without mutating query state.
func (q *TypedQuery[T]) Exists(ctx context.Context) (bool, error) {
	n, err := q.Count(ctx)
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// UpdateAll applies the same field updates to every row matching the
// current Where chain and returns the number of rows touched. Ignores
// Limit/Offset/Order, the underlying SQL is a plain UPDATE with tenant,
// owner, soft-delete, and Where/WhereNested predicates applied (mirroring
// write-side scoping, without read scopes).
//
// Fields name columns as CreateOne and UpdateOne read them: the column, its
// wire key, or the key MarshalEntity derives. For type-safe updates,
// marshal a partial struct with framework.MarshalEntity and pass the
// resulting map; the map itself is not modified. A body that sets no
// writable field returns an error rather than restamping updated_at alone.
func (q *TypedQuery[T]) UpdateAll(ctx context.Context, fields map[string]any) (int, error) {
	if err := q.requireTenantCtx(ctx); err != nil {
		return 0, err
	}
	if len(fields) == 0 {
		return 0, fmt.Errorf("UpdateAll: no fields to set")
	}
	// Name columns the way CreateOne and UpdateOne do: a wire key, or the
	// snake key MarshalEntity derives (line2 for line_2). bodyColumns
	// renames in place, so it works on a copy of the caller's map.
	body := maps.Clone(fields)
	if err := q.handler.bodyColumns(body); err != nil {
		return 0, err
	}
	// The state field and stamps move per record (states.go).
	if err := q.handler.checkStateBulk(body); err != nil {
		return 0, err
	}

	if err := q.handler.validateMediaURLs(body); err != nil {
		return 0, err
	}
	// One value written onto many rows cannot be "this row's current
	// key" for all of them, so a storage key here needs WithUploadedKeys
	// or WithServerWrites (media_provenance.go).
	if err := q.handler.checkMediaProvenance(ctx, body, nil); err != nil {
		return 0, err
	}

	// Same integer-exactness gate as the HTTP update path: a host map
	// carrying a float64 (decoded elsewhere from JSON) must not silently
	// round an Int column above 2^53.
	if err := q.handler.coerceNumberColumnValues(body); err != nil {
		return 0, err
	}

	vr := schema.ValidatePartial(q.handler.entitySchema(), body)
	if !vr.Valid {
		return 0, &ValidationError{fields: vr.Errors}
	}

	// The rows it matches can hold different stored values in a column
	// it keeps, so part of a multi-column reference cannot move here.
	sets := func(f schema.Field) bool { return q.handler.updateSets(ctx, f, false) }
	if err := q.handler.checkBelongsToScope(ctx, nil, fkWrite{body: body, sets: sets, holds: q.handler.scopedHeld(ctx)}); err != nil {
		return 0, err
	}
	ub := query.Update(q.handler.Entity.GetTable())
	anySet := false
	for _, f := range q.handler.Entity.GetFields() {
		// Mirror doUpdate, owner and tenant included (transfer-by-tamper).
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
	// Restamp updated_at on the bulk update, mirroring the single-row
	// doUpdate path. A caller-supplied updated_at (a backfill) is written
	// as given instead: the field loop above skips every AutoGenerate
	// column, so it is set here or not at all. The restamp alone is not a
	// write, as in doUpdate: a body that sets nothing else answers
	// errNoFieldsToUpdate instead of a row count for a timestamp-only
	// update.
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
	// Soft-deleted rows are logically gone, a bulk UPDATE must not reach
	// them, mirroring the SELECT/Count paths (ApplySoftDeleteFilter).
	if q.handler.Entity.Config.Scope.SoftDelete {
		ub.Where("deleted_at IS NULL")
	}
	req := syntheticRequest(ctx, "PATCH", "/")
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

// exec is what the query runs on: the transaction ctx carries (App.InTx,
// a hook's), else the handler's DB. The pool alone took a bulk write
// out of the caller's transaction, so it committed whatever the owner
// decided, and on SQLite waited behind that transaction's lock.
func (q *TypedQuery[T]) exec(ctx context.Context) DBExecutor {
	return q.handler.conn(ctx)
}

// DeleteAll removes every row matching the current Where chain. For
// SoftDelete entities, sets deleted_at instead of issuing a DELETE.
func (q *TypedQuery[T]) DeleteAll(ctx context.Context) (int, error) {
	if err := q.requireTenantCtx(ctx); err != nil {
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
		// Don't re-stamp rows that are already soft-deleted, they're
		// logically gone, and re-touching them would resurrect their
		// delete timestamp and inflate the affected-row count.
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
	// The removed rows' stored objects go with them (media_cleanup.go).
	return q.handler.deleteReturningObjects(ctx, db)
}

// IsNotFound reports whether err corresponds to a not-found result on a
// typed CRUD operation. Treats sql.ErrNoRows and the framework's internal
// errNotFound (from Update/Delete misses) as equivalent.
func IsNotFound(err error) bool {
	return errors.Is(err, sql.ErrNoRows) || errors.Is(err, errNotFound)
}
