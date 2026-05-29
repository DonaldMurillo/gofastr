package crud

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"

	"github.com/DonaldMurillo/gofastr/core/query"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/event"
	"github.com/DonaldMurillo/gofastr/framework/filter"
)

// In-process typed CRUD API
//
// The methods below let same-process callers (typed repositories, jobs, seed
// scripts) drive the framework's full CRUD pipeline — transaction wrapping,
// hook chain, event emission — without round-tripping through HTTP. They
// share the underlying do{Create,Update,Delete} primitives with the HTTP
// handlers so semantics never drift between the two paths.
//
// Internally each method synthesises a minimal *http.Request to feed the
// existing apply*Scope helpers (which read tenant ID from ctx and trashed
// flag from the URL). Tenant is honoured because GetTenantID(ctx) reads
// from context regardless of how the request was constructed.

// syntheticRequest builds a placeholder *http.Request whose Context is ctx.
// Used when an in-process caller needs to feed code paths that take
// *http.Request but only consult ctx + URL query.
func syntheticRequest(ctx context.Context, method, path string) *http.Request {
	r := httptest.NewRequest(method, path, nil)
	return r.WithContext(ctx)
}

// CreateOne runs the full Create pipeline (BeforeCreate hooks → INSERT →
// AfterCreate hooks) inside a single transaction and emits entity.created on
// commit. Returns the created row as a snake_cased map; convert to a typed
// struct in your caller.
func (ch *CrudHandler) CreateOne(ctx context.Context, body map[string]any) (map[string]any, error) {
	if err := ch.requireOwnerContext(ctx); err != nil {
		return nil, err
	}
	if err := ch.requireTenantContext(ctx); err != nil {
		return nil, err
	}
	req := syntheticRequest(ctx, http.MethodPost, "/")
	var result map[string]any
	err := ch.inTx(ctx, func(ctx context.Context, ch *CrudHandler) error {
		res, err := ch.doCreate(ctx, req, body)
		if err != nil {
			return err
		}
		result = res
		return nil
	})
	if err != nil {
		return nil, err
	}
	ch.EmitEvent(ctx, event.EntityCreated, result)
	return result, nil
}

// UpdateOne updates a record by id with the partial body. Hooks + tx +
// event emission all fire as in the HTTP path.
func (ch *CrudHandler) UpdateOne(ctx context.Context, id string, body map[string]any) (map[string]any, error) {
	if err := ch.requireOwnerContext(ctx); err != nil {
		return nil, err
	}
	if err := ch.requireTenantContext(ctx); err != nil {
		return nil, err
	}
	req := syntheticRequest(ctx, http.MethodPut, "/")
	var result map[string]any
	err := ch.inTx(ctx, func(ctx context.Context, ch *CrudHandler) error {
		res, err := ch.doUpdate(ctx, req, id, body)
		if err != nil {
			return err
		}
		result = res
		return nil
	})
	if err != nil {
		return nil, err
	}
	ch.EmitEvent(ctx, event.EntityUpdated, result)
	return result, nil
}

// DeleteOne deletes (or soft-deletes) a record by id.
func (ch *CrudHandler) DeleteOne(ctx context.Context, id string) error {
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
	ch.EmitEvent(ctx, event.EntityDeleted, map[string]any{ch.convertKey(ch.PrimaryKey): id})
	return nil
}

// GetOne fetches a single record by id, optionally eager-loading the named
// includes. Returns sql.ErrNoRows when the record doesn't exist (or is
// soft-deleted, unless options ask otherwise).
func (ch *CrudHandler) GetOne(ctx context.Context, id string, includes []string) (map[string]any, error) {
	if err := ch.requireOwnerContext(ctx); err != nil {
		return nil, err
	}
	if err := ch.requireTenantContext(ctx); err != nil {
		return nil, err
	}
	cols := ch.VisibleFields()
	qb := query.Select(cols...).
		From(ch.Entity.GetTable()).
		Where(ch.PrimaryKey+" = $1", id)
	req := syntheticRequest(ctx, http.MethodGet, "/")
	ch.ApplyTenantScope(qb, req)
	ch.ApplyOwnerScope(qb, req)
	ch.ApplySoftDeleteFilter(qb, req)

	sqlStr, args := qb.Build()
	row := ch.DB.QueryRowContext(ctx, sqlStr, args...)
	result, err := scanRow(row, cols, ch.convertKey)
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
	return result, nil
}

// ListOptions controls ListAll.
type ListOptions struct {
	Filters  []filter.ParsedFilter
	Sorts    []filter.ParsedSort
	Limit    int
	Offset   int
	Includes []string
}

// ListAll runs a list query with optional filters/sort/limit/offset/includes
// and returns the matching rows. Caller is responsible for paging if the
// result set is large.
func (ch *CrudHandler) ListAll(ctx context.Context, opts ListOptions) ([]map[string]any, error) {
	if err := ch.requireOwnerContext(ctx); err != nil {
		return nil, err
	}
	if err := ch.requireTenantContext(ctx); err != nil {
		return nil, err
	}
	cols := ch.VisibleFields()
	qb := query.Select(cols...).From(ch.Entity.GetTable())
	filter.ApplyToQuery(qb, opts.Filters)
	filter.ApplySortToQuery(qb, opts.Sorts)
	req := syntheticRequest(ctx, http.MethodGet, "/")
	ch.ApplyTenantScope(qb, req)
	ch.ApplyOwnerScope(qb, req)
	ch.ApplySoftDeleteFilter(qb, req)
	if opts.Limit > 0 {
		qb.Limit(opts.Limit)
	}
	if opts.Offset > 0 {
		qb.Offset(opts.Offset)
	}

	sqlStr, args := qb.Build()
	rows, err := ch.DB.QueryContext(ctx, sqlStr, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	results, err := scanRows(rows, cols, ch.convertKey)
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
	return results, nil
}

// BatchCreateMany runs CreateOne for each body in a single transaction.
// Events fire after commit (per item, in input order). Any per-item error
// rolls back the whole batch — same semantics as the HTTP _batch endpoint.
func (ch *CrudHandler) BatchCreateMany(ctx context.Context, bodies []map[string]any) ([]map[string]any, error) {
	if err := ch.requireOwnerContext(ctx); err != nil {
		return nil, err
	}
	if err := ch.requireTenantContext(ctx); err != nil {
		return nil, err
	}
	results := make([]map[string]any, len(bodies))
	req := syntheticRequest(ctx, "POST", "/")
	txErr := ch.inTx(ctx, func(ctx context.Context, ch *CrudHandler) error {
		for i, body := range bodies {
			res, err := ch.doCreate(ctx, req, body)
			if err != nil {
				return err
			}
			results[i] = res
		}
		return nil
	})
	if txErr != nil {
		return nil, txErr
	}
	for _, res := range results {
		ch.EmitEvent(ctx, event.EntityCreated, res)
	}
	return results, nil
}

// BatchUpdateMany runs UpdateOne for each (id, body) pair atomically.
func (ch *CrudHandler) BatchUpdateMany(ctx context.Context, ids []string, bodies []map[string]any) ([]map[string]any, error) {
	if err := ch.requireOwnerContext(ctx); err != nil {
		return nil, err
	}
	if err := ch.requireTenantContext(ctx); err != nil {
		return nil, err
	}
	if len(ids) != len(bodies) {
		return nil, fmt.Errorf("BatchUpdateMany: ids and bodies length mismatch (%d vs %d)", len(ids), len(bodies))
	}
	results := make([]map[string]any, len(ids))
	req := syntheticRequest(ctx, "PATCH", "/")
	txErr := ch.inTx(ctx, func(ctx context.Context, ch *CrudHandler) error {
		for i, id := range ids {
			res, err := ch.doUpdate(ctx, req, id, bodies[i])
			if err != nil {
				return err
			}
			results[i] = res
		}
		return nil
	})
	if txErr != nil {
		return nil, txErr
	}
	for _, res := range results {
		ch.EmitEvent(ctx, event.EntityUpdated, res)
	}
	return results, nil
}

// BatchDeleteMany deletes (or soft-deletes) each id atomically. Returns the
// ids that were successfully removed.
func (ch *CrudHandler) BatchDeleteMany(ctx context.Context, ids []string) ([]string, error) {
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
		ch.EmitEvent(ctx, event.EntityDeleted, map[string]any{ch.convertKey(ch.PrimaryKey): id})
	}
	return ids, nil
}

// CountAll returns COUNT(*) for the given filters (no pagination). Cheap
// helper for typed repos that want a totals figure without hitting List.
func (ch *CrudHandler) CountAll(ctx context.Context, opts ListOptions) (int, error) {
	if err := ch.requireOwnerContext(ctx); err != nil {
		return 0, err
	}
	if err := ch.requireTenantContext(ctx); err != nil {
		return 0, err
	}
	cb := query.Count(ch.Entity.GetTable())
	filter.ApplyToCountQuery(cb, opts.Filters)
	req := syntheticRequest(ctx, http.MethodGet, "/")
	ch.ApplyTenantScopeCount(cb, req)
	ch.ApplyOwnerScopeCount(cb, req)
	ch.ApplySoftDeleteFilterCount(cb, req)
	sqlStr, args := cb.Build()
	var total int
	if err := ch.DB.QueryRowContext(ctx, sqlStr, args...).Scan(&total); err != nil {
		return 0, err
	}
	return total, nil
}

// buildIncludeNodesFromNames takes a flat list of include names (possibly
// dotted) and runs them through the same parser HTTP requests use. Lets
// in-process callers use ?include= semantics without constructing a URL.
func buildIncludeNodesFromNames(ent *entity.Entity, registry entity.Registry, names []string) ([]*IncludeNode, error) {
	if len(names) == 0 {
		return nil, nil
	}
	// Synthesize a request whose ?include= we'll re-use parseIncludeTree on.
	r := httptest.NewRequest(http.MethodGet, "/?include="+joinNonEmpty(names, ","), nil)
	return parseIncludeTree(r, ent, registry)
}

// joinNonEmpty is strings.Join but trims and drops empties.
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
