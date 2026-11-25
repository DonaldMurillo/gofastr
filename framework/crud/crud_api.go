package crud

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"time"

	"github.com/DonaldMurillo/gofastr/core/query"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/event"
	"github.com/DonaldMurillo/gofastr/framework/filter"
	"github.com/DonaldMurillo/gofastr/internal/casing"
)

// syntheticRequest is the placeholder request an in-process call feeds
// the request-taking scope helpers, marked as in-process.
func syntheticRequest(ctx context.Context, method, path string) *http.Request {
	r := httptest.NewRequest(method, path, nil)
	return r.WithContext(context.WithValue(ctx, inProcessKey{}, true))
}

// bodyColumns renames, in place, each key of an in-process body to the
// column it names: a wire key, or a key MarshalEntity derived, by its
// camel then its snake form. Two keys naming one column are refused. A
// Date column's Go midnight becomes its YYYY-MM-DD.
func (ch *CrudHandler) bodyColumns(body map[string]any) error {
	snap := ch.snapshot()
	named := make(map[string]string, len(body))
	for _, k := range slices.Sorted(maps.Keys(body)) {
		col := k
		if _, isColumn := snap.wireKeyOf[k]; !isColumn {
			if c, ok := snap.columnOfWire[k]; ok {
				col = c
			} else if c := snap.columnOfCamel[casing.ToCamel(k)]; c != "" {
				col = c
			} else if c := snap.columnOfSnake[k]; c != "" {
				col = c
			}
		}
		if prev, dup := named[col]; dup {
			return fmt.Errorf("keys %q and %q both name the field %q", prev, k, col)
		}
		named[col] = k
		if col != k {
			body[col] = body[k]
			delete(body, k)
		}
		if _, isDate := snap.dateColumns[col]; isDate {
			body[col] = goDate(body[col])
		}
	}
	return nil
}

// goDate is v as YYYY-MM-DD when v is a Go time at midnight, else v.
func goDate(v any) any {
	var t time.Time
	switch x := v.(type) {
	case time.Time:
		t = x
	case string:
		parsed, err := time.Parse(time.RFC3339Nano, x)
		if err != nil {
			return v
		}
		t = parsed
	default:
		return v
	}
	if h, m, s := t.Clock(); h != 0 || m != 0 || s != 0 || t.Nanosecond() != 0 {
		return v
	}
	return t.Format(time.DateOnly)
}

type inProcessKey struct{}

// inProcess reports whether r was built by syntheticRequest.
func inProcess(r *http.Request) bool {
	v, _ := r.Context().Value(inProcessKey{}).(bool)
	return v
}

// CreateOne creates a record through the full Create pipeline in one
// transaction and emits entity.created on commit. It returns the row keyed
// by wire key, or its id only when the caller's ReadScope hides it.
// ReadOnly and Hidden fields in body are skipped unless ctx carries
// WithServerWrites.
func (ch *CrudHandler) CreateOne(ctx context.Context, body map[string]any) (map[string]any, error) {
	ctx = withEventStamps(ctx)
	if err := ch.requireOwnerContext(ctx); err != nil {
		return nil, err
	}
	if err := ch.requireTenantContext(ctx); err != nil {
		return nil, err
	}
	if err := ch.bodyColumns(body); err != nil {
		return nil, err
	}
	req := syntheticRequest(ctx, http.MethodPost, "/")
	var result, out map[string]any
	err := ch.inTx(ctx, func(ctx context.Context, ch *CrudHandler) error {
		res, err := ch.doCreate(ctx, req, body)
		if err != nil {
			return err
		}
		result = res
		out, err = ch.scopedReadBack(ctx, res)
		return err
	})
	if err != nil {
		return nil, err
	}
	ch.EmitEvent(ctx, event.EntityCreated, result)
	return out, nil
}

// UpdateOne patches record id as the HTTP Update does and returns the row
// as CreateOne does. The owner and tenant columns are never written from
// body.
func (ch *CrudHandler) UpdateOne(ctx context.Context, id string, body map[string]any) (map[string]any, error) {
	ctx = withEventStamps(ctx)
	if err := ch.requireOwnerContext(ctx); err != nil {
		return nil, err
	}
	if err := ch.requireTenantContext(ctx); err != nil {
		return nil, err
	}
	if err := ch.bodyColumns(body); err != nil {
		return nil, err
	}
	req := syntheticRequest(ctx, http.MethodPut, "/")
	var result, out map[string]any
	err := ch.inTx(ctx, func(ctx context.Context, ch *CrudHandler) error {
		res, err := ch.doUpdate(ctx, req, id, body)
		if err != nil {
			return err
		}
		result = res
		out, err = ch.scopedReadBack(ctx, res)
		return err
	})
	if err != nil {
		return nil, err
	}
	ch.EmitEvent(ctx, event.EntityUpdated, result)
	return out, nil
}

// DeleteOne deletes (or soft-deletes) a record by id.
func (ch *CrudHandler) DeleteOne(ctx context.Context, id string) error {
	ctx = withEventStamps(ctx)
	if err := ch.requireOwnerContext(ctx); err != nil {
		return err
	}
	if err := ch.requireTenantContext(ctx); err != nil {
		return err
	}
	req := syntheticRequest(ctx, http.MethodDelete, "/")
	err := ch.inTx(ctx, func(ctx context.Context, ch *CrudHandler) error {
		return ch.doDelete(ctx, req, id)
	})
	if err != nil {
		return err
	}
	ch.EmitEvent(ctx, event.EntityDeleted, ch.keyPayload(id))
	return nil
}

// GetOne fetches record id under every read scope, with the named
// includes and the AfterGet hooks. A missing or hidden record answers
// errNotFound.
func (ch *CrudHandler) GetOne(ctx context.Context, id string, includes []string) (map[string]any, error) {
	if err := ch.requireOwnerContext(ctx); err != nil {
		return nil, err
	}
	if err := ch.requireTenantContext(ctx); err != nil {
		return nil, err
	}
	cols := ch.visibleFields()
	kc, ka := ch.keyWhere(id)
	qb := query.Select(ch.keyedCols(cols)...).
		From(ch.Entity.GetTable()).
		Where(kc, ka...)
	req := syntheticRequest(ctx, http.MethodGet, "/")
	ch.ApplyTenantScope(qb, req)
	ch.ApplyOwnerScope(qb, req)
	ch.ApplyReadScope(qb, req)
	ch.ApplySoftDeleteFilter(qb, req)
	hookWheres, err := ch.runBeforeGet(ctx, req, id)
	if err != nil {
		return nil, err
	}
	for _, c := range hookWheres {
		qb.Where(c.SQL, c.Args...)
	}

	sqlStr, args := qb.Build()
	result, err := ch.scanOwn(ctx, id, cols, sqlStr, args)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, errNotFound
		}
		return nil, fmt.Errorf("get %s: %w", ch.Entity.GetName(), err)
	}

	if len(includes) > 0 {
		nodes, err := buildIncludeNodesFromNames(ch.Entity, ch.Registry, includes)
		if err != nil {
			return nil, err
		}
		if err := ch.applyIncludeTree(ctx, []map[string]any{result}, nodes); err != nil {
			return nil, fmt.Errorf("include: %w", err)
		}
	}

	return ch.runAfterGet(ctx, req, id, result)
}

// ListOptions controls ListAll.
type ListOptions struct {
	// Filters are ANDed conditions, checked as ?field_<op>= is: a real,
	// non-Hidden, non-NoQuery column and an operator and value its type
	// takes.
	Filters []filter.ParsedFilter
	// NestedFilters are relation predicates, as ?author.name=alice.
	NestedFilters []NestedFilter
	Sorts         []filter.ParsedSort
	// Where is a predicate tree ANDed, as one clause, with everything
	// else; it is validated before use and never written to. Build one
	// with dsl.ParsePredicate or filter.ParseWhere.
	Where *filter.Predicate
	// Fields limits the columns read to these plus the key, as ?fields=
	// does; an unknown or Hidden name is refused. Empty reads every
	// visible column.
	Fields   []string
	Limit    int
	Offset   int
	Includes []string
	// Search is a ?q= term; set on an entity without SearchFields it is
	// an error.
	Search string
	// Deleted lists only soft-deleted rows, every other scope unchanged;
	// on an entity without Scope.SoftDelete it answers ErrNoSoftDelete.
	Deleted bool
}

// ListAll returns the rows opts selects under every read scope, with the
// AfterList hooks run over them.
func (ch *CrudHandler) ListAll(ctx context.Context, opts ListOptions) ([]map[string]any, error) {
	cols := ch.visibleFields()
	if len(opts.Fields) > 0 {
		var err error
		cols, err = ch.projectListFields(opts.Fields)
		if err != nil {
			return nil, fmt.Errorf("ListAll: %w", err)
		}
	}
	qb, req, err := ch.scopedSelect(ctx, "ListAll", opts, cols)
	if err != nil {
		return nil, err
	}
	filter.ApplySortToQuery(qb, ch.sortTerms(ctx, opts.Sorts))
	if opts.Limit > 0 {
		qb.Limit(opts.Limit)
	}
	if opts.Offset > 0 {
		qb.Offset(opts.Offset)
	}

	sqlStr, args := qb.Build()
	rows, err := ch.conn(ctx).QueryContext(ctx, sqlStr, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	results, err := ch.scanMany(rows, cols)
	if err != nil {
		return nil, err
	}

	if len(opts.Includes) > 0 {
		nodes, err := buildIncludeNodesFromNames(ch.Entity, ch.Registry, opts.Includes)
		if err != nil {
			return nil, err
		}
		if err := ch.applyIncludeTree(ctx, results, nodes); err != nil {
			return nil, fmt.Errorf("include: %w", err)
		}
	}

	return ch.runAfterList(ctx, req, results)
}

// scopedSelect builds SELECT cols under every scope and filter a list
// read runs; op names the caller in refusals.
func (ch *CrudHandler) scopedSelect(ctx context.Context, op string, opts ListOptions, cols []string) (*query.QueryBuilder, *http.Request, error) {
	if err := ch.requireOwnerContext(ctx); err != nil {
		return nil, nil, err
	}
	if err := ch.requireTenantContext(ctx); err != nil {
		return nil, nil, err
	}
	if opts.Deleted && !ch.Entity.Config.Scope.SoftDelete {
		return nil, nil, ErrNoSoftDelete
	}
	nested, err := resolveNestedFilters(ch.Entity, ch.Registry, opts.NestedFilters)
	if err != nil {
		return nil, nil, err
	}
	if err := ch.scopeNestedFiltersInProcess(ctx, nested); err != nil {
		return nil, nil, err
	}
	if opts.Search != "" && len(ch.Entity.Config.SearchFields) == 0 {
		return nil, nil, fmt.Errorf("%s: Search set on entity %q without SearchFields", op, ch.Entity.GetName())
	}
	where, err := ch.validateWherePredicate(opts.Where)
	if err != nil {
		return nil, nil, fmt.Errorf("%s: %w", op, err)
	}
	filters, err := ch.validateFilters(opts.Filters)
	if err != nil {
		return nil, nil, fmt.Errorf("%s: %w", op, err)
	}
	if err := ch.refuseLegacyTimestampComparisons(ctx, filters, where); err != nil {
		return nil, nil, fmt.Errorf("%s: %w", op, err)
	}
	searchConds, err := filter.SearchConditions(ch.Entity.Config.SearchFields, opts.Search)
	if err != nil {
		return nil, nil, fmt.Errorf("%s: %w", op, err)
	}
	qb := query.Select(cols...).From(ch.Entity.GetTable())
	filter.ApplyToQuery(qb, filters)
	if c := filter.BuildPredicate(where); c.SQL != "" {
		qb.Where(c.SQL, c.Args...)
	}
	req := syntheticRequest(ctx, http.MethodGet, "/")
	ch.ApplyTenantScope(qb, req)
	ch.ApplyOwnerScope(qb, req)
	ch.ApplyReadScope(qb, req)
	if opts.Deleted {
		qb.Where("deleted_at IS NOT NULL")
	} else {
		ch.ApplySoftDeleteFilter(qb, req)
	}
	applyNestedFilters(
		func(sql string, args ...any) { qb.Where(sql, args...) },
		ch.Entity.GetTable(), ch.keyCols(), nested,
	)
	for _, c := range searchConds {
		qb.Where(c.SQL, c.Args...)
	}
	hookWheres, err := ch.runBeforeList(ctx, req)
	if err != nil {
		return nil, nil, err
	}
	for _, c := range hookWheres {
		qb.Where(c.SQL, c.Args...)
	}
	return qb, req, nil
}

// BatchCreateMany creates each body as CreateOne does, all in one
// transaction: any error rolls back the batch. Events go out after commit.
func (ch *CrudHandler) BatchCreateMany(ctx context.Context, bodies []map[string]any) ([]map[string]any, error) {
	ctx = withEventStamps(ctx)
	if err := ch.requireOwnerContext(ctx); err != nil {
		return nil, err
	}
	if err := ch.requireTenantContext(ctx); err != nil {
		return nil, err
	}
	for _, body := range bodies {
		if err := ch.bodyColumns(body); err != nil {
			return nil, err
		}
	}
	results := make([]map[string]any, len(bodies))
	out := make([]map[string]any, len(bodies))
	req := syntheticRequest(ctx, "POST", "/")
	txErr := ch.inTx(ctx, func(ctx context.Context, ch *CrudHandler) error {
		for i, body := range bodies {
			res, err := ch.doCreate(ctx, req, body)
			if err != nil {
				return err
			}
			results[i] = res
			if out[i], err = ch.scopedReadBack(ctx, res); err != nil {
				return err
			}
		}
		return nil
	})
	if txErr != nil {
		return nil, txErr
	}
	for _, res := range results {
		ch.EmitEvent(ctx, event.EntityCreated, res)
	}
	return out, nil
}

// BatchUpdateMany patches each (id, body) pair as UpdateOne does, in one
// transaction.
func (ch *CrudHandler) BatchUpdateMany(ctx context.Context, ids []string, bodies []map[string]any) ([]map[string]any, error) {
	ctx = withEventStamps(ctx)
	if err := ch.requireOwnerContext(ctx); err != nil {
		return nil, err
	}
	if err := ch.requireTenantContext(ctx); err != nil {
		return nil, err
	}
	if len(ids) != len(bodies) {
		return nil, fmt.Errorf("BatchUpdateMany: ids and bodies length mismatch (%d vs %d)", len(ids), len(bodies))
	}
	for _, body := range bodies {
		if err := ch.bodyColumns(body); err != nil {
			return nil, err
		}
	}
	results := make([]map[string]any, len(ids))
	out := make([]map[string]any, len(ids))
	req := syntheticRequest(ctx, "PATCH", "/")
	txErr := ch.inTx(ctx, func(ctx context.Context, ch *CrudHandler) error {
		for i, id := range ids {
			res, err := ch.doUpdate(ctx, req, id, bodies[i])
			if err != nil {
				return err
			}
			results[i] = res
			if out[i], err = ch.scopedReadBack(ctx, res); err != nil {
				return err
			}
		}
		return nil
	})
	if txErr != nil {
		return nil, txErr
	}
	for _, res := range results {
		ch.EmitEvent(ctx, event.EntityUpdated, res)
	}
	return out, nil
}

// BatchDeleteMany deletes, or soft-deletes, each id in one transaction
// and returns the ids.
func (ch *CrudHandler) BatchDeleteMany(ctx context.Context, ids []string) ([]string, error) {
	ctx = withEventStamps(ctx)
	if err := ch.requireOwnerContext(ctx); err != nil {
		return nil, err
	}
	if err := ch.requireTenantContext(ctx); err != nil {
		return nil, err
	}
	req := syntheticRequest(ctx, "DELETE", "/")
	txErr := ch.inTx(ctx, func(ctx context.Context, ch *CrudHandler) error {
		for _, id := range ids {
			if err := ch.doDelete(ctx, req, id); err != nil {
				return err
			}
		}
		return nil
	})
	if txErr != nil {
		return nil, txErr
	}
	for _, id := range ids {
		ch.EmitEvent(ctx, event.EntityDeleted, ch.keyPayload(id))
	}
	return ids, nil
}

// CountAll counts the rows ListAll would return for opts, ignoring
// Limit and Offset, and refuses what ListAll refuses.
func (ch *CrudHandler) CountAll(ctx context.Context, opts ListOptions) (int, error) {
	if len(opts.Fields) > 0 {
		if _, err := ch.projectListFields(opts.Fields); err != nil {
			return 0, fmt.Errorf("CountAll: %w", err)
		}
	}
	qb, _, err := ch.scopedSelect(ctx, "CountAll", opts, ch.keyCols()[:1])
	if err != nil {
		return 0, err
	}
	if _, err := buildIncludeNodesFromNames(ch.Entity, ch.Registry, opts.Includes); err != nil {
		return 0, err
	}
	inner, args := qb.Build()
	var total int
	if err := ch.conn(ctx).QueryRowContext(ctx, "SELECT COUNT(*) FROM ("+inner+") AS count_src", args...).Scan(&total); err != nil {
		return 0, err
	}
	return total, nil
}

// validateWherePredicate returns a validated, resolved copy of p.
func (ch *CrudHandler) validateWherePredicate(p *filter.Predicate) (*filter.Predicate, error) {
	return filter.ValidatePredicate(p, ch.queryFields())
}

// validateFilters checks hand-built filters as ParseFilters checks URL
// ones and returns a copy keyed by column.
func (ch *CrudHandler) validateFilters(filters []filter.ParsedFilter) ([]filter.ParsedFilter, error) {
	if len(filters) == 0 {
		return nil, nil
	}
	fields := ch.queryFields()
	col := make(map[string]schema.Field, 2*len(fields))
	for _, f := range fields {
		if f.Hidden || f.NoQuery {
			continue
		}
		col[f.Name] = f
		if f.WireName != "" && f.WireName != f.Name {
			col[f.WireName] = f
		}
	}
	out := make([]filter.ParsedFilter, len(filters))
	for i, pf := range filters {
		f, ok := col[pf.Field]
		if !ok {
			return nil, fmt.Errorf("filter: unknown filter field %q", pf.Field)
		}
		if err := filter.CheckOpType(pf.Field, pf.Op, f.Type); err != nil {
			return nil, fmt.Errorf("filter: %w", err)
		}
		typed, err := pf.Typed(f.Type)
		if err != nil {
			return nil, fmt.Errorf("filter: %w", err)
		}
		typed.Field = f.Name
		out[i] = typed
	}
	return out, nil
}

// projectListFields resolves ListOptions.Fields through the ?fields=
// parser.
func (ch *CrudHandler) projectListFields(names []string) ([]string, error) {
	q := url.Values{"fields": {joinNonEmpty(names, ",")}}
	return ch.projectFromRequestQ(q)
}

// buildIncludeNodesFromNames parses include names through the ?include=
// parser.
func buildIncludeNodesFromNames(ent *entity.Entity, registry entity.Registry, names []string) ([]*IncludeNode, error) {
	if len(names) == 0 {
		return nil, nil
	}
	return parseIncludeTreeQ(url.Values{"include": {joinNonEmpty(names, ",")}}, ent, registry)
}

// joinNonEmpty is strings.Join dropping empty parts.
func joinNonEmpty(parts []string, sep string) string {
	out := ""
	for _, p := range parts {
		if p == "" {
			continue
		}
		if out != "" {
			out += sep
		}
		out += p
	}
	return out
}
