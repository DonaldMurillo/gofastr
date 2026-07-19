package crud

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/query"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/core/upload"
	"github.com/DonaldMurillo/gofastr/framework/db"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/event"
	"github.com/DonaldMurillo/gofastr/framework/filter"
	"github.com/DonaldMurillo/gofastr/framework/hook"
	"github.com/DonaldMurillo/gofastr/framework/internal/casing"
	"github.com/DonaldMurillo/gofastr/framework/tenant"
)

// getHandlerUser is a thin alias for core/handler.GetUser kept in this
// package so the soft-delete filter doesn't reach across packages
// directly inside an inline closure. The bool is true iff the request
// is authenticated.
func getHandlerUser(ctx context.Context) (any, bool) {
	return handler.GetUser(ctx)
}

// beforeHookError flags a BeforeCreate/BeforeUpdate/BeforeDelete hook
// rejection so the caller can map it to 400 instead of 500.
type beforeHookError struct{ err error }

func (e *beforeHookError) Error() string { return e.err.Error() }
func (e *beforeHookError) Unwrap() error { return e.err }

// tenantMissingError signals a Create attempt against a MultiTenant
// entity with no tenant in the request context. Surfaces as 400 in
// the HTTP handler so an orphan row can never be written.
type tenantMissingError struct{}

func (e *tenantMissingError) Error() string {
	return "tenant context required for multi-tenant entity"
}

// DBExecutor is an alias for db.Executor, retained so existing callers keep
// using framework.DBExecutor. New code should reference framework/db directly.
type DBExecutor = db.Executor

// CrudHandler provides auto-generated CRUD HTTP handlers for an Entity.
type CrudHandler struct {
	Entity     *entity.Entity
	DB         DBExecutor
	PrimaryKey string             // defaults to "id"
	JSONCase   JSONCase           // casing strategy for JSON keys
	Hooks      *hook.HookRegistry // optional lifecycle hooks
	Storage    upload.Storage     // optional; enables multipart uploads for Image/File fields
	Events     *event.EventBus    // optional; receives entity.created/updated/deleted on commit
	Outbox     EventOutbox        // optional; when set, lifecycle events are staged in-tx (transactional outbox) and delivered to declared consumers by the relay. EmitEvent still notifies Events (real-time lane); the relay does not, so there is no double delivery.
	Registry   entity.Registry    // optional; required for nested ?include=author.profile resolution
	BasePath   string             // optional; URL prefix where this entity's routes are mounted (e.g. "/api/v1"). Used by MCP tools to dispatch against the same path the HTTP routes live at; empty = bare "/table".

	visibleFieldsCache []string
	visibleJSONKeys    []string
	visibleFieldSig    uint64
}

// NewCrudHandler creates a new CrudHandler for the given entity and database.
func NewCrudHandler(ent *entity.Entity, db DBExecutor) *CrudHandler {
	ch := &CrudHandler{Entity: ent, DB: db, PrimaryKey: "id", JSONCase: CaseCamel, Hooks: nil}
	ch.refreshFieldCache()
	return ch
}

// WithJSONCase sets the JSON casing strategy for the handler.
func (ch *CrudHandler) WithJSONCase(c JSONCase) *CrudHandler {
	ch.JSONCase = c
	ch.refreshFieldCache()
	return ch
}

// ListResponse is the standard JSON response for list endpoints.
type ListResponse struct {
	Data       []map[string]any `json:"data"`
	Total      int              `json:"total"`
	Page       int              `json:"page"`
	PerPage    int              `json:"perPage"`
	TotalPages int              `json:"totalPages"`
}

// singleResponse is the standard JSON envelope for single-record CRUD
// endpoints (create, get, update, and patch).
type singleResponse struct {
	Data map[string]any `json:"data"`
}

// ApplyTenantScope adds a tenant_id filter to the query when the entity
// is configured for multi-tenancy and a tenant ID is present in the context.
// Note: uses PostgreSQL-style $1 placeholders.
func (ch *CrudHandler) ApplyTenantScope(qb *query.QueryBuilder, r *http.Request) {
	if ch.Entity.Config.MultiTenant {
		tenantID := tenant.GetTenantID(r.Context())
		if tenantID != "" {
			qb.Where(ch.Entity.Config.TenantColumn()+" = $1", tenantID)
		}
	}
}

// ApplyTenantScopeCount adds a tenant_id filter to a count query builder.
// Note: uses PostgreSQL-style $1 placeholders.
func (ch *CrudHandler) ApplyTenantScopeCount(cb *query.CountBuilder, r *http.Request) {
	if ch.Entity.Config.MultiTenant {
		tenantID := tenant.GetTenantID(r.Context())
		if tenantID != "" {
			cb.Where(ch.Entity.Config.TenantColumn()+" = $1", tenantID)
		}
	}
}

// ApplyTenantScopeUpdate adds a tenant_id filter to an update query builder.
// Note: uses PostgreSQL-style $1 placeholders.
func (ch *CrudHandler) ApplyTenantScopeUpdate(ub *query.UpdateBuilder, r *http.Request) {
	if ch.Entity.Config.MultiTenant {
		tenantID := tenant.GetTenantID(r.Context())
		if tenantID != "" {
			ub.Where(ch.Entity.Config.TenantColumn()+" = $1", tenantID)
		}
	}
}

// ApplyTenantScopeDelete adds a tenant_id filter to a delete query builder.
// Note: uses PostgreSQL-style $1 placeholders.
func (ch *CrudHandler) ApplyTenantScopeDelete(db *query.DeleteBuilder, r *http.Request) {
	if ch.Entity.Config.MultiTenant {
		tenantID := tenant.GetTenantID(r.Context())
		if tenantID != "" {
			db.Where(ch.Entity.Config.TenantColumn()+" = $1", tenantID)
		}
	}
}

// InjectTenant injects the tenant_id into a data map when multi-tenancy is
// enabled. It reads the tenant ID from ctx so it works whether the caller is
// outside or inside an in-tx context derived from the request.
func (ch *CrudHandler) InjectTenant(data map[string]any, ctx context.Context) {
	if ch.Entity.Config.MultiTenant {
		tenantID := tenant.GetTenantID(ctx)
		if tenantID != "" {
			data[ch.Entity.Config.TenantColumn()] = tenantID
		}
	}
}

// ApplySoftDeleteFilter adds a deleted_at IS NULL filter unless the caller
// requests trashed records via ?trashed=true AND the request is
// authenticated. An anonymous caller passing ?trashed=true on a public
// list endpoint must not be allowed to enumerate soft-deleted rows —
// that's an information-disclosure path. The query param is honoured
// only when a user is present in the request context.
func (ch *CrudHandler) ApplySoftDeleteFilter(qb *query.QueryBuilder, r *http.Request) {
	if ch.Entity.Config.SoftDelete {
		if !ch.trashedAllowed(r) {
			qb.Where("deleted_at IS NULL")
		}
	}
}

// ApplySoftDeleteFilterCount adds a deleted_at IS NULL filter to a count query.
// Same authentication gate as ApplySoftDeleteFilter.
func (ch *CrudHandler) ApplySoftDeleteFilterCount(cb *query.CountBuilder, r *http.Request) {
	if ch.Entity.Config.SoftDelete {
		if !ch.trashedAllowed(r) {
			cb.Where("deleted_at IS NULL")
		}
	}
}

// trashedAllowed reports whether the caller may see soft-deleted rows on
// this request. True only when ?trashed=true AND the request carries an
// authenticated user — anonymous callers are denied visibility into
// soft-deleted data regardless of how they ask.
func (ch *CrudHandler) trashedAllowed(r *http.Request) bool {
	if r.URL.Query().Get("trashed") != "true" {
		return false
	}
	if _, ok := getHandlerUser(r.Context()); !ok {
		return false
	}
	return true
}

// entityFields returns all field names for queries (SELECT, RETURNING).
func (ch *CrudHandler) entityFields() []string {
	fields := ch.Entity.GetFields()
	names := make([]string, 0, len(fields))
	for _, f := range fields {
		names = append(names, f.Name)
	}
	return names
}

// VisibleFields returns field names that are not Hidden.
func (ch *CrudHandler) VisibleFields() []string {
	return append([]string(nil), ch.visibleFields()...)
}

func (ch *CrudHandler) visibleFields() []string {
	sig := ch.fieldCacheSignature()
	if len(ch.visibleFieldsCache) == 0 || ch.visibleFieldSig != sig {
		ch.refreshFieldCache()
	}
	return ch.visibleFieldsCache
}

func (ch *CrudHandler) refreshFieldCache() {
	if ch.Entity == nil {
		ch.visibleFieldsCache = nil
		ch.visibleJSONKeys = nil
		ch.visibleFieldSig = 0
		return
	}
	names := make([]string, 0, len(ch.Entity.GetFields()))
	for _, f := range ch.Entity.GetFields() {
		if !f.Hidden {
			names = append(names, f.Name)
		}
	}
	ch.visibleFieldsCache = names
	ch.visibleJSONKeys = convertedKeys(names, ch.convertKey)
	ch.visibleFieldSig = ch.fieldCacheSignature()
}

func (ch *CrudHandler) jsonKeysFor(cols []string) []string {
	if ch.visibleFieldSig != ch.fieldCacheSignature() {
		ch.refreshFieldCache()
	}
	if len(cols) == len(ch.visibleFieldsCache) {
		match := true
		for i := range cols {
			if cols[i] != ch.visibleFieldsCache[i] {
				match = false
				break
			}
		}
		if match {
			return ch.visibleJSONKeys
		}
	}
	return convertedKeys(cols, ch.convertKey)
}

func (ch *CrudHandler) fieldCacheSignature() uint64 {
	if ch.Entity == nil {
		return 0
	}
	const (
		offset64 = 1469598103934665603
		prime64  = 1099511628211
	)
	h := uint64(offset64)
	for i := 0; i < len(ch.JSONCase); i++ {
		h ^= uint64(ch.JSONCase[i])
		h *= prime64
	}
	for _, f := range ch.Entity.GetFields() {
		for i := 0; i < len(f.Name); i++ {
			h ^= uint64(f.Name[i])
			h *= prime64
		}
		if f.Hidden {
			h ^= 1
		} else {
			h ^= 2
		}
		h *= prime64
	}
	return h
}

// convertKey applies the configured JSON casing to a DB column name.
func (ch *CrudHandler) convertKey(col string) string {
	switch ch.JSONCase {
	case CaseSnake:
		return col
	default: // CaseCamel
		return casing.ToCamel(col)
	}
}

// convertMapKeys applies the configured JSON casing to all keys in a map.
func (ch *CrudHandler) convertMapKeys(m map[string]any) map[string]any {
	switch ch.JSONCase {
	case CaseSnake:
		return m
	default: // CaseCamel
		return casing.MapToCamel(m)
	}
}

// unconvertMapKeys reverses the JSON casing back to DB column names (snake_case).
func (ch *CrudHandler) unconvertMapKeys(m map[string]any) map[string]any {
	switch ch.JSONCase {
	case CaseSnake:
		return m
	default: // CaseCamel
		return casing.MapToSnake(m)
	}
}

// entitySchema returns the schema for validation.
func (ch *CrudHandler) entitySchema() schema.Schema {
	return schema.Schema{Fields: ch.Entity.GetFields()}
}

// List returns an http.HandlerFunc that lists entity records with filtering,
// sorting, pagination, and optional ?include= eager-loaded relations.
//
// Hook chain: BeforeList → SELECT → AfterList. The BeforeList hook can
// append WHERE predicates via the *hook.ListPayload.AddWhere helper;
// appended clauses apply to both the data query and the count query.
// AfterList receives the fetched results and may mutate them in place.
func (ch *CrudHandler) List() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !ch.requireScope(w, r, opRead) {
			return
		}
		ctx := r.Context()
		page, perPage := parsePagination(r, ch.Entity.Config.MaxListLimit)

		includes, err := parseIncludeTree(r, ch.Entity, ch.Registry)
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, err.Error())
			return
		}

		var filterOpts []filter.FilterOption
		if ch.Entity.Config.LenientFilters {
			filterOpts = append(filterOpts, filter.Lenient())
		}
		if extra := ch.Entity.Config.AllowedFilterParams; len(extra) > 0 {
			filterOpts = append(filterOpts, filter.Allow(extra...))
		}
		filters, err := filter.ParseFilters(r, ch.Entity.GetFields(), filterOpts...)
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid filters: "+err.Error())
			return
		}

		// q-column edge case: when the entity has SearchFields and ?q= is
		// present, a plain ?q=value would be parsed as an OpEq filter on a
		// column named "q". Drop it — plain ?q= means search. Suffixed ops
		// (?q_like=, ?q_gt=, …) still filter the column.
		filters = stripQColumnEqFilter(filters, len(ch.Entity.Config.SearchFields) > 0, r.URL.Query().Has("q"))

		// Nested filters like ?author.name=alice. Parsed once and applied to
		// both the count + data queries below.
		nested, err := parseNestedFilters(r, ch.Entity, ch.Registry)
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, err.Error())
			return
		}

		// ?q= free-text search: when the entity declares SearchFields and
		// the request carries a non-blank ?q=, build filter.SearchConditions
		// and append them as Where clauses. They feed the count, data,
		// cursor, and stream sinks uniformly because listPayload.Where is
		// applied to each. Zero signature changes.
		searchWheres := ch.searchWhereClauses(r)

		// ?where=<json> nested predicate tree (OR-groups / nested AND-OR).
		// Compiles to ONE parenthesized WHERE clause that AND-composes with
		// the owner/tenant/soft-delete scopes exactly like the search
		// clauses above — a user OR-group can never widen past a scope.
		treeWheres, err := ch.whereTreeClauses(r)
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid where: "+err.Error())
			return
		}
		searchWheres = append(searchWheres, treeWheres...)

		// BeforeList hook — collect any extra WHERE clauses the host wants
		// to scope the query by. Runs before cursor / streaming branches so
		// both paths inherit the same scope.
		listPayload := &hook.ListPayload{Request: r}
		if ch.Hooks != nil {
			if err := ch.Hooks.ExecuteHooks(ctx, hook.BeforeList, listPayload); err != nil {
				writeJSONError(w, http.StatusBadRequest, err.Error())
				return
			}
		}

		// Merge ?q= search conditions with hook-appended Where clauses so
		// both feed the count, data, cursor, and stream sinks.
		listPayload.Where = append(searchWheres, listPayload.Where...)

		// Cursor pagination is opt-in: presence of the ?cursor key (even
		// empty for first-page) switches to keyset mode and emits the
		// CursorPage envelope.
		if r.URL.Query().Has("cursor") {
			ch.serveCursorList(ctx, w, r, includes, filters, nested, listPayload.Where)
			return
		}

		sorts, err := filter.ParseSort(r, ch.Entity.GetFields())
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, err.Error())
			return
		}

		cols, err := ch.projectFromRequest(r)
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, err.Error())
			return
		}

		// Streaming-list opt-in: explicit ?stream=true, or auto-on when the
		// requested limit is huge. Streaming skips include resolution to keep
		// memory bounded — so it CANNOT honour ?include= or per-row AfterList
		// transforms (AfterList runs once over the full slice the stream
		// never materialises). Silently streaming anyway is wrong: includes
		// would vanish, and an AfterList redactor would be BYPASSED — leaking
		// the very fields it exists to hide.
		//
		// Explicit ?stream=true → refuse with 400 so the caller knows their
		// include / hook contract can't be met. Auto-streaming (a huge limit,
		// not an explicit opt-in) → fall through to the buffered path, which
		// resolves includes and runs AfterList correctly. Correctness wins
		// over the streaming memory optimisation when the two conflict.
		explicitStream := r.URL.Query().Get("stream") == "true"
		hasAfterList := ch.Hooks != nil && len(ch.Hooks.HooksFor(hook.AfterList)) > 0
		if explicitStream || perPage >= streamListThreshold {
			if len(includes) > 0 {
				if explicitStream {
					writeJSONError(w, http.StatusBadRequest, "streaming list does not support include; drop ?include= or ?stream=true")
					return
				}
			} else if hasAfterList {
				if explicitStream {
					writeJSONError(w, http.StatusBadRequest, "streaming list does not support AfterList hooks; drop ?stream=true")
					return
				}
			} else {
				ch.ServeStreamingList(ctx, w, r, cols, filters, nested, sorts, page, perPage, listPayload.Where)
				return
			}
			// Fall through to the buffered path (auto-stream with includes or
			// AfterList) — it honours both correctly.
		}

		var total int
		// Count total matching rows
		countQb := query.Count(ch.Entity.GetTable())
		filter.ApplyToCountQuery(countQb, filters)
		ch.ApplyTenantScopeCount(countQb, r)
		ch.ApplyOwnerScopeCount(countQb, r)
		ch.ApplySoftDeleteFilterCount(countQb, r)
		applyNestedFilters(
			func(sql string, args ...any) { countQb.Where(sql, args...) },
			ch.Entity.GetTable(), ch.PrimaryKey, nested,
		)
		for _, c := range listPayload.Where {
			countQb.Where(c.SQL, c.Args...)
		}
		countSQL, countArgs := countQb.Build()
		if err := ch.DB.QueryRowContext(ctx, countSQL, countArgs...).Scan(&total); err != nil {
			log.Printf("crud: list count failed: %v", err)
			writeJSONError(w, http.StatusInternalServerError, "internal server error")
			return
		}

		// Build data query — select only projected (or all visible by default).
		qb := query.Select(cols...)
		qb.From(ch.Entity.GetTable())
		filter.ApplyToQuery(qb, filters)
		ch.ApplyTenantScope(qb, r)
		ch.ApplyOwnerScope(qb, r)
		ch.ApplySoftDeleteFilter(qb, r)
		applyNestedFilters(
			func(sql string, args ...any) { qb.Where(sql, args...) },
			ch.Entity.GetTable(), ch.PrimaryKey, nested,
		)
		for _, c := range listPayload.Where {
			qb.Where(c.SQL, c.Args...)
		}
		filter.ApplySortToQuery(qb, sorts)

		offset := (page - 1) * perPage
		// An explicit ?offset= overrides the page-derived offset. The
		// process-module broker paginates by raw offset (it sets ?offset=
		// without ?page=), and it is a documented control param — honoring it
		// here is what makes those requests return the intended window
		// instead of silently serving page 1.
		if o, ok := explicitOffset(r); ok {
			offset = o
		}
		qb.Limit(perPage)
		qb.Offset(offset)

		dataSQL, dataArgs := qb.Build()
		rows, err := ch.DB.QueryContext(ctx, dataSQL, dataArgs...)
		if err != nil {
			log.Printf("crud: list query failed: %v", err)
			writeJSONError(w, http.StatusInternalServerError, "internal server error")
			return
		}
		defer rows.Close()

		keys := ch.jsonKeysFor(cols)
		var (
			results      []map[string]any
			pooledRows   *[]map[string]any
			pooledEncode bool
		)
		if len(includes) == 0 && ch.Hooks == nil {
			pooledRows, err = scanRowsPooledWithKeys(rows, cols, keys)
			if err == nil {
				results = *pooledRows
				pooledEncode = true
			}
		} else {
			results, err = scanRowsWithKeys(rows, cols, keys)
		}
		if err != nil {
			log.Printf("crud: list scan failed: %v", err)
			writeJSONError(w, http.StatusInternalServerError, "internal server error")
			return
		}

		if err := ch.applyIncludeTree(ctx, results, includes); err != nil {
			log.Printf("crud: list include failed: %v", err)
			writeJSONError(w, http.StatusInternalServerError, "internal server error")
			return
		}

		// AfterList hook — host can redact / transform / drop rows.
		if ch.Hooks != nil {
			listPayload.Results = results
			if err := ch.Hooks.ExecuteHooks(ctx, hook.AfterList, listPayload); err != nil {
				log.Printf("crud: after-list hook failed: %v", err)
				writeJSONError(w, http.StatusInternalServerError, "internal server error")
				return
			}
			results = listPayload.Results
		}

		totalPages := total / perPage
		if total%perPage != 0 {
			totalPages++
		}

		resp := ListResponse{
			Data:       results,
			Total:      total,
			Page:       page,
			PerPage:    perPage,
			TotalPages: totalPages,
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
		if pooledEncode {
			returnRowSlice(pooledRows)
		}
	}
}

// searchWhereClauses builds hook.WhereClause entries from ?q= when the
// entity declares SearchFields. Returns nil when SearchFields is empty
// or ?q= is blank — the entity then ignores ?q= exactly as before
// (back-compat). The conditions AND-compose safely with owner/tenant/
// soft-delete scopes because the query builder wraps each Where clause
// in parens.
func (ch *CrudHandler) searchWhereClauses(r *http.Request) []hook.WhereClause {
	if len(ch.Entity.Config.SearchFields) == 0 {
		return nil
	}
	q := r.URL.Query().Get("q")
	conds := filter.SearchConditions(ch.Entity.Config.SearchFields, q)
	if len(conds) == 0 {
		return nil
	}
	wheres := make([]hook.WhereClause, len(conds))
	for i, c := range conds {
		wheres[i] = hook.WhereClause{SQL: c.SQL, Args: c.Args}
	}
	return wheres
}

// whereTreeClauses parses a ?where=<json> nested predicate tree, validates
// every field against the entity's (non-Hidden) schema and every operator
// against the supported set, and compiles it to one hook.WhereClause. It
// returns (nil, nil) when ?where= is absent/blank (back-compat). An
// invalid tree returns an error the caller maps to 400. The single clause
// AND-composes with owner/tenant/soft-delete scopes because the query
// builder wraps each Where in parens.
func (ch *CrudHandler) whereTreeClauses(r *http.Request) ([]hook.WhereClause, error) {
	raw := r.URL.Query().Get("where")
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	p, err := filter.ParseWhere(raw, ch.Entity.GetFields())
	if err != nil {
		return nil, err
	}
	if p == nil {
		return nil, nil
	}
	c := filter.BuildPredicate(p)
	if c.SQL == "" {
		return nil, nil
	}
	return []hook.WhereClause{{SQL: c.SQL, Args: c.Args}}, nil
}

// stripQColumnEqFilter removes a plain OpEq filter on a column named "q"
// when the entity has SearchFields AND ?q= is present. This resolves the
// edge case: an entity WITH SearchFields that also has a physical column
// named "q" — plain ?q= means search (not filter on the column). Suffixed
// ops (?q_like=, ?q_gt=, …) still filter the column.
func stripQColumnEqFilter(filters []filter.ParsedFilter, hasSearchFields, qPresent bool) []filter.ParsedFilter {
	if !hasSearchFields || !qPresent || len(filters) == 0 {
		return filters
	}
	out := filters[:0]
	for _, f := range filters {
		if f.Op == filter.OpEq && f.Field == "q" {
			continue
		}
		out = append(out, f)
	}
	return out
}

// Get returns an http.HandlerFunc that fetches a single entity by ID.
// Honours ?include= eager-loaded relations.
//
// Hook chain: BeforeGet → SELECT → AfterGet. The BeforeGet hook can
// append WHERE predicates via *hook.GetPayload.AddWhere to scope the
// lookup (mismatches return 404). AfterGet may mutate the result map
// (redact, transform).
func (ch *CrudHandler) Get() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !ch.requireScope(w, r, opRead) {
			return
		}
		ctx := r.Context()
		id := r.PathValue("id")
		if id == "" {
			writeJSONError(w, http.StatusBadRequest, "missing id")
			return
		}

		includes, err := parseIncludeTree(r, ch.Entity, ch.Registry)
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, err.Error())
			return
		}

		getPayload := &hook.GetPayload{Request: r, ID: id}
		if ch.Hooks != nil {
			if err := ch.Hooks.ExecuteHooks(ctx, hook.BeforeGet, getPayload); err != nil {
				writeJSONError(w, http.StatusBadRequest, err.Error())
				return
			}
		}

		cols, err := ch.projectFromRequest(r)
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, err.Error())
			return
		}
		qb := query.Select(cols...)
		qb.From(ch.Entity.GetTable())
		qb.Where(ch.PrimaryKey+" = $1", id)
		ch.ApplyTenantScope(qb, r)
		ch.ApplyOwnerScope(qb, r)
		ch.ApplySoftDeleteFilter(qb, r)
		for _, c := range getPayload.Where {
			qb.Where(c.SQL, c.Args...)
		}

		sqlStr, args := qb.Build()
		row := ch.DB.QueryRowContext(ctx, sqlStr, args...)

		result, err := scanRow(row, cols, ch.convertKey)
		if err != nil {
			if err == sql.ErrNoRows {
				writeJSONError(w, http.StatusNotFound, "not found")
				return
			}
			log.Printf("crud: get query failed: %v", err)
			writeJSONError(w, http.StatusInternalServerError, "internal server error")
			return
		}

		if err := ch.applyIncludeTree(ctx, []map[string]any{result}, includes); err != nil {
			log.Printf("crud: get include failed: %v", err)
			writeJSONError(w, http.StatusInternalServerError, "internal server error")
			return
		}

		if ch.Hooks != nil {
			getPayload.Result = result
			if err := ch.Hooks.ExecuteHooks(ctx, hook.AfterGet, getPayload); err != nil {
				log.Printf("crud: after-get hook failed: %v", err)
				writeJSONError(w, http.StatusInternalServerError, "internal server error")
				return
			}
			result = getPayload.Result
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(singleResponse{Data: result})
	}
}

// Create returns an http.HandlerFunc that creates a new entity record.
// Auto-generated fields are populated server-side and excluded from the
// request body. The hook chain (BeforeCreate → INSERT → AfterCreate) runs
// inside a single transaction; if any step errors the write is rolled back.
//
// Accepts application/json or multipart/form-data. When the request is
// multipart, parts whose name matches an Image/File field are streamed
// through the handler's Storage backend and persisted as a URL string.
func (ch *CrudHandler) Create() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := enforceJSONContentType(r); err != nil {
			writeJSONError(w, http.StatusUnsupportedMediaType, "unsupported media type")
			return
		}
		if !ch.requireScope(w, r, opCreate) {
			return
		}
		limitJSONBody(w, r)
		body, err := ch.readRequestBody(r)
		if err != nil {
			if errors.Is(err, errBodyTooLarge) {
				writeJSONError(w, http.StatusRequestEntityTooLarge, "request body too large")
				return
			}
			writeJSONError(w, http.StatusBadRequest, err.Error())
			return
		}

		var result map[string]any
		err = ch.inTx(WithAuditRequest(r.Context(), r), func(ctx context.Context, ch *CrudHandler) error {
			res, err := ch.doCreate(ctx, r, body)
			if err != nil {
				return err
			}
			result = res
			return nil
		})
		if err != nil {
			writeCRUDError(w, err)
			return
		}

		ch.EmitEvent(r.Context(), event.EntityCreated, result)

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(singleResponse{Data: result})
	}
}

// Update returns an http.HandlerFunc that updates an entity by ID. The hook
// chain (BeforeUpdate → UPDATE → AfterUpdate) runs inside a transaction.
// Accepts application/json or multipart/form-data (same rules as Create).
func (ch *CrudHandler) Update() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := enforceJSONContentType(r); err != nil {
			writeJSONError(w, http.StatusUnsupportedMediaType, "unsupported media type")
			return
		}
		if !ch.requireScope(w, r, opUpdate) {
			return
		}
		id := r.PathValue("id")
		if id == "" {
			writeJSONError(w, http.StatusBadRequest, "missing id")
			return
		}

		limitJSONBody(w, r)
		body, err := ch.readRequestBody(r)
		if err != nil {
			if errors.Is(err, errBodyTooLarge) {
				writeJSONError(w, http.StatusRequestEntityTooLarge, "request body too large")
				return
			}
			writeJSONError(w, http.StatusBadRequest, err.Error())
			return
		}

		var result map[string]any
		err = ch.inTx(WithAuditRequest(r.Context(), r), func(ctx context.Context, ch *CrudHandler) error {
			res, err := ch.doUpdate(ctx, r, id, body)
			if err != nil {
				return err
			}
			result = res
			return nil
		})
		if err != nil {
			writeCRUDError(w, err)
			return
		}

		ch.EmitEvent(r.Context(), event.EntityUpdated, result)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(singleResponse{Data: result})
	}
}

// Delete returns an http.HandlerFunc that deletes an entity by ID. If the
// entity has SoftDelete=true, it sets deleted_at instead. The hook chain
// (BeforeDelete → DELETE/UPDATE → AfterDelete) runs inside a transaction.
func (ch *CrudHandler) Delete() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !ch.requireScope(w, r, opDelete) {
			return
		}
		id := r.PathValue("id")
		if id == "" {
			writeJSONError(w, http.StatusBadRequest, "missing id")
			return
		}

		err := ch.inTx(WithAuditRequest(r.Context(), r), func(ctx context.Context, ch *CrudHandler) error {
			return ch.doDelete(ctx, r, id)
		})
		if err != nil {
			writeCRUDError(w, err)
			return
		}

		ch.EmitEvent(r.Context(), event.EntityDeleted, map[string]any{ch.convertKey(ch.PrimaryKey): id})

		w.WriteHeader(http.StatusNoContent)
	}
}

// ValidationError carries field-level validation errors from inside inTx
// out to the response writer. It is the error type returned by Create /
// Update / Upsert when schema validation rejects the body. Callers branch
// on it with errors.As:
//
//	var ve *crud.ValidationError
//	if errors.As(err, &ve) { ... ve.Fields() ... }
//
// The fields map is exposed read-only via Fields; mutating it has no
// effect on the handler or the wire response.
type ValidationError struct{ fields map[string][]string }

// Error implements the error interface. The string is deliberately
// generic ("validation failed"); per-field detail lives in Fields().
func (e *ValidationError) Error() string { return "validation failed" }

// Fields returns the per-field validation messages keyed by column name.
// The returned map is the handler's internal copy; callers MUST treat it
// as read-only.
func (e *ValidationError) Fields() map[string][]string { return e.fields }

// NewValidationError constructs a ValidationError from a field→messages
// map. Intended for tests and host code that needs to synthesize a
// validation failure (e.g. from a custom BeforeCreate hook).
func NewValidationError(fields map[string][]string) *ValidationError {
	return &ValidationError{fields: fields}
}

// Sentinel errors for CRUD flows.
var (
	errNotFound         = errors.New("not found")
	errNoFieldsToUpdate = errors.New("no fields to update")
)

// writeCRUDError maps a CRUD-flow error to the appropriate HTTP response.
// Sentinel and typed errors are translated to specific status codes; anything
// else becomes a 500.
func writeCRUDError(w http.ResponseWriter, err error) {
	var bhe *beforeHookError
	if errors.As(err, &bhe) {
		writeJSONError(w, http.StatusBadRequest, bhe.Error())
		return
	}
	var ve *ValidationError
	if errors.As(err, &ve) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]any{
			"error":   "validation failed",
			"success": false,
			"fields":  ve.Fields(),
		})
		return
	}
	var tme *tenantMissingError
	if errors.As(err, &tme) {
		writeJSONError(w, http.StatusBadRequest, tme.Error())
		return
	}
	if errors.Is(err, errNotFound) {
		writeJSONError(w, http.StatusNotFound, "not found")
		return
	}
	if errors.Is(err, errNoFieldsToUpdate) {
		writeJSONError(w, http.StatusBadRequest, "no fields to update")
		return
	}
	if isUniqueViolation(err) {
		// Map UNIQUE-constraint failures to 409 Conflict so callers can
		// distinguish duplicate-key errors from a real server fault.
		// The error message itself is generic — we deliberately don't
		// echo the violated column to avoid leaking schema details to
		// an enumeration probe.
		writeJSONError(w, http.StatusConflict, "conflict")
		return
	}
	// Unrecognised error → 500 with a generic message. Returning
	// err.Error() here leaks driver-specific details (`pq: relation
	// "users" does not exist`, `dial tcp 10.0.0.1:5432: ...`,
	// `UNIQUE constraint failed: users.email`) that fingerprint the
	// schema and backend. The full message is logged on the server
	// side; the client sees a generic "internal server error" with
	// the original error remaining matchable via errors.Is in tests.
	log.Printf("crud: internal error: %v", err)
	writeJSONError(w, http.StatusInternalServerError, "internal server error")
}

// isUniqueViolation reports whether err looks like a UNIQUE-constraint
// violation from any of the supported drivers. We sniff the message
// string because the drivers don't share a typed error and the CRUD
// layer is otherwise driver-agnostic. False positives are rare —
// "UNIQUE constraint failed" (sqlite), "duplicate key value" (pq),
// "Error 1062" (mysql) are all distinctive.
func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	for _, sig := range []string{
		"UNIQUE constraint failed",
		"duplicate key value",
		"Error 1062",
	} {
		if strings.Contains(msg, sig) {
			return true
		}
	}
	return false
}

// parsePagination extracts the page number (?page) and page size (?limit,
// or its ?per_page alias) from query params. Defaults: page=1, perPage=20.
//
// The per_page cap is 100 by default. Entities can raise this via
// EntityConfig.MaxListLimit. ?stream=true on its own does NOT raise
// the cap — that path is opt-in per entity (MaxListLimit > 100) so
// public endpoints can't be coerced into 10× larger responses by
// adding a query param. When the entity has explicitly raised the
// limit, the streaming-list path uses min(MaxListLimit, streamListThreshold).
func parsePagination(r *http.Request, entityMax int) (page, perPage int) {
	page = 1
	perPage = 20

	if v := r.URL.Query().Get("page"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			page = n
		}
	}

	maxPerPage := listLimitCap(entityMax)

	// ?limit is the canonical page-size param; ?per_page is accepted as an
	// alias (a common REST convention) so a client using it gets the size it
	// asked for rather than a silent default. ?limit wins when both are sent.
	sizeParam := r.URL.Query().Get("limit")
	if sizeParam == "" {
		sizeParam = r.URL.Query().Get("per_page")
	}
	if sizeParam != "" {
		if n, err := strconv.Atoi(sizeParam); err == nil && n > 0 {
			perPage = n
		}
	}
	// Clamp BOTH the requested and the default page size: an oversized
	// ?limit must cap (not silently fall back to the default, which
	// itself exceeds MaxListLimit whenever the cap is below 20).
	if perPage > maxPerPage {
		perPage = maxPerPage
	}
	return
}

// explicitOffset reads a raw ?offset= row skip. Returns (n, true) only for a
// well-formed non-negative integer; a missing, malformed, or negative value
// yields (0, false) so the caller keeps the page-derived offset. LIMIT still
// caps the row count, so an oversized offset just returns an empty window —
// no need to clamp it here.
func explicitOffset(r *http.Request) (int, bool) {
	v := r.URL.Query().Get("offset")
	if v == "" {
		return 0, false
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 0 {
		return 0, false
	}
	return n, true
}

// listLimitCap is the effective per-request row cap for an entity:
// the global default (100) unless EntityConfig.MaxListLimit raises or
// lowers it, never above streamListThreshold. Shared by the offset,
// streaming, and cursor list paths so no path can exceed the cap.
func listLimitCap(entityMax int) int {
	limitCap := 100
	if entityMax > 0 {
		limitCap = entityMax
		if limitCap > streamListThreshold {
			limitCap = streamListThreshold
		}
	}
	return limitCap
}

// scanRows scans all rows into a slice of maps, applying keyFunc to column names.
// scanRowsPooled is the pool-backed version in pool.go.
func scanRows(rows *sql.Rows, cols []string, keyFunc func(string) string) ([]map[string]any, error) {
	return scanRowsWithKeys(rows, cols, convertedKeys(cols, keyFunc))
}

func scanRowsWithKeys(rows *sql.Rows, cols, keys []string) ([]map[string]any, error) {
	var results []map[string]any
	for rows.Next() {
		values := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range values {
			ptrs[i] = &values[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, err
		}
		row := make(map[string]any, len(cols))
		for i := range cols {
			row[keys[i]] = convertValue(values[i])
		}
		results = append(results, row)
	}
	// rows.Next() returning false can mean EOF OR a mid-iteration error (a
	// dropped connection, a read fault). Without this check the read path
	// would silently return partial/empty results as success — the eager
	// loaders already guard this; the primary scanner must too.
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return results, nil
}

// scanRow scans a single row into a map, applying keyFunc to column names.
func scanRow(row *sql.Row, cols []string, keyFunc func(string) string) (map[string]any, error) {
	values := make([]any, len(cols))
	ptrs := make([]any, len(cols))
	for i := range values {
		ptrs[i] = &values[i]
	}
	if err := row.Scan(ptrs...); err != nil {
		return nil, err
	}
	result := make(map[string]any, len(cols))
	for i, col := range cols {
		result[keyFunc(col)] = convertValue(values[i])
	}
	return result, nil
}

// convertValue normalizes database driver values into JSON-friendly types.
func convertValue(v any) any {
	switch val := v.(type) {
	case []byte:
		return string(val)
	default:
		return val
	}
}

// writeJSONError writes a structured JSON error response.
func writeJSONError(w http.ResponseWriter, code int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]any{
		"error":   message,
		"success": false,
		"code":    code,
	})
}

// generateFieldValue creates a value based on the auto-generation strategy.
func generateFieldValue(strategy schema.AutoGenerate) any {
	switch strategy {
	case schema.AutoUUID:
		return generateUUID()
	case schema.AutoTimestamp:
		return time.Now().UTC().Format("2006-01-02T15:04:05Z")
	case schema.AutoIncrement:
		return 0 // placeholder — real increment handled by DB
	default:
		return nil
	}
}

// generateUUID creates a new random UUID v4 string.
func generateUUID() string {
	var uuid [16]byte
	rand.Read(uuid[:])
	uuid[6] = (uuid[6] & 0x0f) | 0x40 // version 4
	uuid[8] = (uuid[8] & 0x3f) | 0x80 // variant 10
	return fmt.Sprintf("%x-%x-%x-%x-%x",
		uuid[0:4], uuid[4:6], uuid[6:8], uuid[8:10], uuid[10:])
}

// compile-time check
var _ fmt.Stringer = (*entity.Entity)(nil)
