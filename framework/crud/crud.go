package crud

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"math/big"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"
	"uuid"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/core/query"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/core/stream"
	"github.com/DonaldMurillo/gofastr/core/upload"
	"github.com/DonaldMurillo/gofastr/framework/db"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/event"
	"github.com/DonaldMurillo/gofastr/framework/file"
	"github.com/DonaldMurillo/gofastr/framework/filter"
	"github.com/DonaldMurillo/gofastr/framework/hook"
	"github.com/DonaldMurillo/gofastr/framework/internal/casing"
	"github.com/DonaldMurillo/gofastr/framework/pagination"
	"github.com/DonaldMurillo/gofastr/framework/tenant"
)

func getHandlerUser(ctx context.Context) (any, bool) {
	return handler.GetUser(ctx)
}

// beforeHookError is a Before hook's rejection, answered 400.
type beforeHookError struct{ err error }

func (e *beforeHookError) Error() string { return e.err.Error() }
func (e *beforeHookError) Unwrap() error { return e.err }

// IsHookRefusal reports whether err is a BeforeCreate, BeforeUpdate or
// BeforeDelete hook's rejection, the error the HTTP handlers answer 400.
// A hook that panicked is not a refusal.
func IsHookRefusal(err error) bool {
	_, ok := errors.AsType[*beforeHookError](err)
	return ok && !isHookPanic(err)
}

func isHookPanic(err error) bool {
	_, ok := errors.AsType[*hook.PanicError](err)
	return ok
}

type tenantMissingError struct{}

func (e *tenantMissingError) Error() string { return reasonTenantRequired.English() }

// Phrased implements i18n.Phrased.
func (e *tenantMissingError) Phrased() (i18n.Phrase, map[string]any) {
	return reasonTenantRequired, nil
}

// DBExecutor is db.Executor.
type DBExecutor = db.Executor

// CrudHandler provides auto-generated CRUD HTTP handlers for an Entity.
type CrudHandler struct {
	Entity     *entity.Entity
	DB         DBExecutor
	root       *sql.DB
	dialect    *dialectMemo       // the handler's own answer when DB has no *sql.DB behind it
	PrimaryKey []string           // key columns in order; nil means "id"
	JSONCase   JSONCase           // casing strategy for JSON keys
	Hooks      *hook.HookRegistry // optional lifecycle hooks
	Storage    upload.Storage     // optional; enables multipart uploads for Image/File fields
	// ImageDeriver, when set, derives renditions and a BlurHash/LQIP from
	// every schema.Image upload into the sibling columns the entity declares.
	ImageDeriver file.ImageDeriver
	// FieldImageDerivers overrides ImageDeriver per image field, by name.
	FieldImageDerivers map[string]file.ImageDeriver
	// StripUploadMetadata strips EXIF, XMP and text segments from stored
	// uploads (JPEG, PNG, WebP). Off by default: originals are stored verbatim.
	StripUploadMetadata bool
	Events              *event.EventBus // optional; receives entity.created/updated/deleted on commit
	Outbox              EventOutbox     // optional; stages lifecycle events in the write's transaction for the relay
	Registry            entity.Registry // optional; required for nested ?include=
	// ChildHooks resolves another entity's hook registry by name, so included
	// rows run their own entity's read hooks. It runs on the request path and
	// must not create registries; nil leaves includes unhooked.
	ChildHooks   func(entityName string) *hook.HookRegistry
	BasePath     string // optional; URL prefix the entity's routes are mounted under, for MCP dispatch
	MCPNamespace string // optional; names MCP tools "<ns>.<entity>.<action>"
	// MCPRouteScoped marks an entity whose routes sit behind middleware /mcp
	// does not pass through; its MCP tools stay listed and the route decides.
	MCPRouteScoped bool
	// MaxOffset caps the row skip of ?offset= or a deep ?page=. Zero means
	// the page cap × 1000.
	MaxOffset int
	// EventStreamReauth is how often an idle SSE feed re-runs its read gate.
	// Zero means 30s; the re-check cannot be switched off.
	EventStreamReauth time.Duration
	// EventStreamSeats caps one principal's concurrent EventStream
	// connections. Zero means 16; negative lifts the cap.
	EventStreamSeats int
	// EventStreamSeatOverflow is what a principal at the cap gets:
	// stream.SeatOverflowRefuse (429, the default) or SeatOverflowEvictOldest.
	EventStreamSeatOverflow stream.SeatOverflowPolicy
	seats                   *streamSeatRegistry
	fcache                  *fieldCache
}

// NewCrudHandler creates a CrudHandler for the entity and database.
func NewCrudHandler(ent *entity.Entity, db DBExecutor) *CrudHandler {
	ch := &CrudHandler{Entity: ent, DB: db, PrimaryKey: ent.KeyColumns(), JSONCase: CaseCamel, Hooks: nil}
	ch.fcache = newFieldCache(ch)
	ch.seats = newStreamSeatRegistry()
	ch.dialect = &dialectMemo{}
	return ch
}

// WithMaxOffset sets MaxOffset; n below 1 is ignored.
func (ch *CrudHandler) WithMaxOffset(n int) *CrudHandler {
	if n > 0 {
		ch.MaxOffset = n
	}
	return ch
}

// WithEventStreamReauth sets EventStreamReauth; d below one second is ignored.
func (ch *CrudHandler) WithEventStreamReauth(d time.Duration) *CrudHandler {
	if d >= time.Second {
		ch.EventStreamReauth = d
	}
	return ch
}

func (ch *CrudHandler) WithJSONCase(c JSONCase) *CrudHandler {
	ch.JSONCase = c
	if ch.fcache != nil {
		ch.fcache.publish(buildFieldSnapshot(ch))
	}
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

type singleResponse struct {
	Data map[string]any `json:"data"`
}

// ApplyTenantScope adds the tenant filter (a $1 placeholder) when the entity
// is multi-tenant and the request carries a tenant.
func (ch *CrudHandler) ApplyTenantScope(qb *query.QueryBuilder, r *http.Request) {
	applyTenantScope(ch, qb, r)
}

// ApplyTenantScopeCount is ApplyTenantScope for a count query.
func (ch *CrudHandler) ApplyTenantScopeCount(cb *query.CountBuilder, r *http.Request) {
	applyTenantScope(ch, cb, r)
}

// ApplyTenantScopeUpdate is ApplyTenantScope for an update.
func (ch *CrudHandler) ApplyTenantScopeUpdate(ub *query.UpdateBuilder, r *http.Request) {
	applyTenantScope(ch, ub, r)
}

// ApplyTenantScopeDelete is ApplyTenantScope for a delete.
func (ch *CrudHandler) ApplyTenantScopeDelete(db *query.DeleteBuilder, r *http.Request) {
	applyTenantScope(ch, db, r)
}

func applyTenantScope[B interface{ Where(string, ...any) B }](ch *CrudHandler, b B, r *http.Request) {
	if ch.Entity.Config.Scope.MultiTenant {
		tenantID := tenant.GetTenantID(r.Context())
		if tenantID != "" {
			b.Where(query.QuoteKeyword(ch.Entity.Config.TenantColumn())+" = $1", tenantID)
		}
	}
}

// InjectTenant sets the tenant column in data from ctx's tenant when the
// entity is multi-tenant.
func (ch *CrudHandler) InjectTenant(data map[string]any, ctx context.Context) {
	if ch.Entity.Config.Scope.MultiTenant {
		tenantID := tenant.GetTenantID(ctx)
		if tenantID != "" {
			data[ch.Entity.Config.TenantColumn()] = tenantID
		}
	}
}

// ApplySoftDeleteFilter adds deleted_at IS NULL unless the request is
// authenticated and sends ?trashed=true. An undecodable or repeated
// trashed key keeps the filter.
func (ch *CrudHandler) ApplySoftDeleteFilter(qb *query.QueryBuilder, r *http.Request) {
	ch.applySoftDeleteFilterQ(qb, softDeleteQuery(r), r.Context())
}

// ApplySoftDeleteFilterCount is ApplySoftDeleteFilter for a count query.
func (ch *CrudHandler) ApplySoftDeleteFilterCount(cb *query.CountBuilder, r *http.Request) {
	ch.applySoftDeleteFilterCountQ(cb, softDeleteQuery(r), r.Context())
}

func (ch *CrudHandler) applySoftDeleteFilterQ(qb *query.QueryBuilder, q url.Values, ctx context.Context) {
	if ch.Entity.Config.Scope.SoftDelete && !ch.trashedAllowedQ(q, ctx) {
		qb.Where("deleted_at IS NULL")
	}
}

func (ch *CrudHandler) applySoftDeleteFilterCountQ(cb *query.CountBuilder, q url.Values, ctx context.Context) {
	if ch.Entity.Config.Scope.SoftDelete && !ch.trashedAllowedQ(q, ctx) {
		cb.Where("deleted_at IS NULL")
	}
}

func (ch *CrudHandler) trashedAllowedQ(q url.Values, ctx context.Context) bool {
	if q.Get("trashed") != "true" {
		return false
	}
	if _, ok := getHandlerUser(ctx); !ok {
		return false
	}
	return true
}

func (ch *CrudHandler) entityFields() []string {
	fields := ch.Entity.GetFields()
	names := make([]string, 0, len(fields))
	for _, f := range fields {
		names = append(names, f.Name)
	}
	return names
}

// VisibleFields returns the non-Hidden field names. It re-reads the
// entity's declarations and republishes the field snapshot when they changed.
func (ch *CrudHandler) VisibleFields() []string {
	ch.republishFieldSnapshot()
	return append([]string(nil), ch.snapshot().visible...)
}

func (ch *CrudHandler) visibleFields() []string {
	return ch.snapshot().visible
}

func (ch *CrudHandler) wireKeyOfField(f schema.Field) string {
	if f.WireName != "" {
		return f.WireName
	}
	return ch.convertKeyRaw(f.Name)
}

func (ch *CrudHandler) jsonKeysFor(cols []string) []string {
	s := ch.snapshot()
	if len(cols) == len(s.visible) {
		match := true
		for i := range cols {
			if cols[i] != s.visible[i] {
				match = false
				break
			}
		}
		if match {
			return s.visibleJSONs
		}
	}
	return convertedKeys(cols, ch.convertKey)
}

// convertKey is a column's wire key: its WireName, else the column in the JSONCase.
func (ch *CrudHandler) convertKey(col string) string {
	if wk, ok := ch.snapshot().wireKeyOf[col]; ok {
		return wk
	}
	if relationName, childPath, nested := strings.Cut(col, "."); nested && ch.Entity != nil {
		for _, relation := range ch.Entity.Config.Relations {
			if relation.Name != relationName {
				continue
			}
			target, err := entity.ResolveTarget(ch.Registry, ch.Entity, relation.Entity)
			if err != nil || target == nil {
				break
			}
			child := NewCrudHandler(target, ch.DB).WithJSONCase(ch.JSONCase)
			child.Registry = ch.Registry
			prefix := ch.convertKey(relationName) + "."
			if relation.Type == entity.RelHasMany || relation.Type == entity.RelManyToMany {
				if index, rest, indexed := strings.Cut(childPath, "."); indexed {
					if _, err := strconv.ParseUint(index, 10, 64); err == nil {
						return prefix + index + "." + child.convertKey(rest)
					}
				}
			}
			return prefix + child.convertKey(childPath)
		}
	}
	return ch.convertKeyRaw(col)
}

// WireKey is the key a column's value sits under in the rows ListAll,
// GetOne and the HTTP responses return: its WireName, or the column name
// in the handler's JSONCase.
func (ch *CrudHandler) WireKey(col string) string {
	return ch.convertKey(col)
}

func (ch *CrudHandler) convertKeyRaw(col string) string {
	switch ch.JSONCase {
	case CaseSnake:
		return col
	default: // CaseCamel
		return casing.ToCamel(col)
	}
}

func (ch *CrudHandler) convertMapKeys(m map[string]any) map[string]any {
	s := ch.snapshot()
	out := make(map[string]any, len(m))
	for k, v := range m {
		if wk, ok := s.wireKeyOf[k]; ok {
			out[wk] = v
		} else {
			out[ch.convertKeyRaw(k)] = v
		}
	}
	return out
}

// itemKeyFoldsUnambiguous refuses two keys of a batch item that fold onto
// one column, naming both in sorted order.
func (ch *CrudHandler) itemKeyFoldsUnambiguous(item map[string]any) error {
	seen := make(map[string]string, len(item))
	for _, k := range slices.Sorted(maps.Keys(item)) {
		col := ch.wireKeyColumn(k)
		if prev, dup := seen[col]; dup {
			return i18n.NewError(reasonKeysFold, map[string]any{"first": prev, "second": k, "column": col})
		}
		seen[col] = k
	}
	return nil
}

// wireKeyColumn is the column a wire key lands on.
func (ch *CrudHandler) wireKeyColumn(key string) string {
	if col, ok := ch.snapshot().columnOfWire[key]; ok {
		return col
	}
	return ch.unconvertKeyRaw(key)
}

func (ch *CrudHandler) unconvertMapKeys(m map[string]any) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[ch.wireKeyColumn(k)] = v
	}
	return out
}

func (ch *CrudHandler) unconvertKeyRaw(key string) string {
	switch ch.JSONCase {
	case CaseSnake:
		return key
	default: // CaseCamel
		return casing.ToSnake(key)
	}
}

func (ch *CrudHandler) entitySchema() schema.Schema {
	return schema.Schema{Fields: ch.snapshot().fields}
}

// List returns the handler that lists records with filters, sort,
// pagination and ?include=. BeforeList may add Where clauses, applied to the
// count and the data; AfterList may change rows but not their count.
func (ch *CrudHandler) List() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if !ch.requireScope(w, r, opRead) {
			return
		}
		ctx := r.Context()
		q, err := ch.readQuery(r)
		if err != nil {
			writeErrJSON(w, r, http.StatusBadRequest, err, reasonInvalidQuery)
			return
		}
		page, perPage := parsePaginationValues(q, ch.Entity.Config.Pagination.MaxListLimit)

		if !ch.requireBoundedOffset(w, r, q, page, perPage) {
			return
		}

		includes, err := parseIncludeTreeQ(q, ch.Entity, ch.Registry)
		if err != nil {
			writeErrJSON(w, r, http.StatusBadRequest, err, reasonInvalidQuery)
			return
		}

		var filterOpts []filter.FilterOption
		if ch.Entity.Config.LenientFilters {
			filterOpts = append(filterOpts, filter.Lenient())
		}
		if extra := ch.Entity.Config.AllowedFilterParams; len(extra) > 0 {
			filterOpts = append(filterOpts, filter.Allow(extra...))
		}
		filters, err := filter.ParseFiltersValues(q, ch.queryFields(), filterOpts...)
		if err != nil {
			writeErrJSON(w, r, http.StatusBadRequest, err, reasonInvalidFilters)
			return
		}

		filters = stripQColumnEqFilter(filters, len(ch.Entity.Config.SearchFields) > 0, q.Has("q"))

		nested, err := parseNestedFiltersValues(q, ch.Entity, ch.Registry)
		if err != nil {
			writeErrJSON(w, r, http.StatusBadRequest, err, reasonInvalidQuery)
			return
		}
		if err := ch.scopeNestedFiltersForCaller(withRealRequest(r.Context(), r), nested); err != nil {
			writeIncludeError(w, r, "list", err)
			return
		}

		searchWheres, err := ch.searchWhereClausesQ(q)
		if err != nil {
			writeErrJSON(w, r, http.StatusBadRequest, err, reasonInvalidFilters)
			return
		}

		treeWheres, where, err := ch.whereTreeClausesQ(q)
		if err != nil {
			writeErrJSON(w, r, http.StatusBadRequest, err, reasonInvalidWhere)
			return
		}
		if err := ch.refuseLegacyTimestampComparisons(ctx, filters, where); err != nil {
			var layout *timestampLayoutError
			if errors.As(err, &layout) {
				logf("crud: %v; rewrite it in schema.TimestampLayout or read it through a view that does", err)
				writeJSONError(w, r, http.StatusInternalServerError, reasonTimestampLayout, map[string]any{"field": strconv.Quote(layout.field)})
				return
			}
			logf("crud: timestamp layout check for %s: %v", ch.Entity.GetTable(), err)
			writeJSONError(w, r, http.StatusInternalServerError, i18n.StatusPhrase(http.StatusInternalServerError))
			return
		}
		searchWheres = append(searchWheres, treeWheres...)

		listPayload := &hook.ListPayload{Request: r}
		if ch.Hooks != nil {
			if err := runHooks(ch.Hooks, ctx, hook.BeforeList, listPayload); err != nil {
				writeErrJSON(w, r, http.StatusBadRequest, err, i18n.Phrase{})
				return
			}
		}

		hookWheres := listPayload.Where
		listPayload.Where = append(searchWheres, listPayload.Where...)

		if q.Has("cursor") {
			if _, err := filter.ParseSortValues(q, ch.snapshotFields()); err != nil {
				writeErrJSON(w, r, http.StatusBadRequest, err, reasonInvalidQuery)
				return
			}
			cols, err := ch.projectFromRequestQ(q)
			if err != nil {
				writeErrJSON(w, r, http.StatusBadRequest, err, reasonInvalidQuery)
				return
			}
			ch.serveCursorList(ctx, w, r, q, cols, includes, filters, nested, listPayload.Where, hookWheres)
			return
		}

		sorts, err := filter.ParseSortValues(q, ch.snapshotFields())
		if err != nil {
			writeErrJSON(w, r, http.StatusBadRequest, err, reasonInvalidQuery)
			return
		}

		cols, err := ch.projectFromRequestQ(q)
		if err != nil {
			writeErrJSON(w, r, http.StatusBadRequest, err, reasonInvalidQuery)
			return
		}
		explicitStream := q.Get("stream") == "true"
		hasAfterList := ch.Hooks != nil && len(ch.Hooks.HooksFor(hook.AfterList)) > 0
		if explicitStream || perPage >= streamListThreshold {
			if len(includes) > 0 {
				if explicitStream {
					writeJSONError(w, r, http.StatusBadRequest, reasonStreamInclude)
					return
				}
			} else if hasAfterList {
				if explicitStream {
					writeJSONError(w, r, http.StatusBadRequest, reasonStreamAfterList)
					return
				}
			} else {
				ch.serveStreamingList(ctx, w, r, q, cols, filters, nested, sorts, page, perPage, listPayload.Where)
				return
			}
		}

		var total int
		countQb := query.Count(ch.Entity.GetTable())
		filter.ApplyToCountQuery(countQb, filters)
		ch.ApplyTenantScopeCount(countQb, r)
		ch.ApplyOwnerScopeCount(countQb, r)
		ch.ApplyReadScopeCount(countQb, r)
		ch.applySoftDeleteFilterCountQ(countQb, q, ctx)
		applyNestedFilters(
			func(sql string, args ...any) { countQb.Where(sql, args...) },
			ch.Entity.GetTable(), ch.keyCols(), nested,
		)
		for _, c := range listPayload.Where {
			countQb.Where(c.SQL, c.Args...)
		}
		countSQL, countArgs := countQb.Build()
		if err := ch.conn(ctx).QueryRowContext(ctx, countSQL, countArgs...).Scan(&total); err != nil {
			logf("crud: list count failed: %v", err)
			writeJSONError(w, r, http.StatusInternalServerError, i18n.StatusPhrase(http.StatusInternalServerError))
			return
		}

		qb := query.Select(cols...)
		qb.From(ch.Entity.GetTable())
		filter.ApplyToQuery(qb, filters)
		ch.ApplyTenantScope(qb, r)
		ch.ApplyOwnerScope(qb, r)
		ch.ApplyReadScope(qb, r)
		ch.applySoftDeleteFilterQ(qb, q, ctx)
		applyNestedFilters(
			func(sql string, args ...any) { qb.Where(sql, args...) },
			ch.Entity.GetTable(), ch.keyCols(), nested,
		)
		for _, c := range listPayload.Where {
			qb.Where(c.SQL, c.Args...)
		}
		filter.ApplySortToQuery(qb, ch.sortTerms(ctx, sorts))

		offset := pagination.OffsetForPage(page, perPage)
		if o, ok := explicitOffsetValues(q); ok {
			offset = o
			page = o/perPage + 1
		}
		qb.Limit(perPage)
		qb.Offset(offset)

		dataSQL, dataArgs := qb.Build()
		rows, err := ch.conn(ctx).QueryContext(ctx, dataSQL, dataArgs...)
		if err != nil {
			logf("crud: list query failed: %v", err)
			writeJSONError(w, r, http.StatusInternalServerError, i18n.StatusPhrase(http.StatusInternalServerError))
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
			pooledRows, err = scanRowsPooledWithKeysForEntity(rows, cols, keys, ch.snapshotFields())
			if err == nil {
				results = *pooledRows
				pooledEncode = true
			}
		} else {
			results, err = scanRowsWithKeysForEntity(rows, cols, keys, ch.snapshotFields())
		}
		if err != nil {
			logf("crud: list scan failed: %v", err)
			writeJSONError(w, r, http.StatusInternalServerError, i18n.StatusPhrase(http.StatusInternalServerError))
			return
		}
		ch.decodeReadRows(results)

		if err := ch.applyIncludeTree(withRealRequest(WithReadHooks(ctx), r), results, includes); err != nil {
			writeIncludeError(w, r, "list", err)
			return
		}

		ch.maskOwn(ctx, results...)
		if ch.Hooks != nil {
			listPayload.Results = results
			if err := ch.applyAfterList(ctx, listPayload); err != nil {
				logf("crud: after-list hook failed: %v", err)
				writeJSONError(w, r, http.StatusInternalServerError, i18n.StatusPhrase(http.StatusInternalServerError))
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

		writeJSONStatus(w, r, http.StatusOK, resp)
		if pooledEncode {
			returnRowSlice(pooledRows)
		}
	}
}

// searchWhereClausesQ is ?q= as Where clauses. A non-blank ?q= on an entity
// with no SearchFields is refused, unless ?q= is the entity's own filter.
func (ch *CrudHandler) searchWhereClausesQ(q url.Values) ([]hook.WhereClause, error) {
	if len(ch.Entity.Config.SearchFields) == 0 && ch.ownsQParam() {
		return nil, nil
	}
	qVal := q.Get("q")
	if len(ch.Entity.Config.SearchFields) == 0 {
		if strings.TrimSpace(qVal) == "" {
			return nil, nil
		}
		return nil, &i18n.Error{Reason: reasonSearchNotConfigured}
	}
	conds, err := filter.SearchConditions(ch.Entity.Config.SearchFields, qVal)
	if err != nil || len(conds) == 0 {
		return nil, err
	}
	wheres := make([]hook.WhereClause, len(conds))
	for i, c := range conds {
		wheres[i] = hook.WhereClause{SQL: c.SQL, Args: c.Args}
	}
	return wheres, nil
}

// ownsQParam reports whether ?q= names a queryable (not Hidden, not NoQuery)
// column or an AllowedFilterParams entry.
func (ch *CrudHandler) ownsQParam() bool {
	for _, f := range ch.queryFields() {
		if f.Hidden || f.NoQuery {
			continue
		}
		if f.Name == "q" || f.WireName == "q" {
			return true
		}
	}
	return slices.Contains(ch.Entity.Config.AllowedFilterParams, "q")
}

func (ch *CrudHandler) whereTreeClausesQ(q url.Values) ([]hook.WhereClause, *filter.Predicate, error) {
	raw := q.Get("where")
	if strings.TrimSpace(raw) == "" {
		return nil, nil, nil
	}
	p, err := filter.ParseWhere(raw, ch.queryFields())
	if err != nil {
		return nil, nil, err
	}
	if p == nil {
		return nil, nil, nil
	}
	c := filter.BuildPredicate(p)
	if c.SQL == "" {
		return nil, p, nil
	}
	return []hook.WhereClause{{SQL: c.SQL, Args: c.Args}}, p, nil
}

// stripQColumnEqFilter drops ?q=value as a filter on a q column when ?q= is
// a search; ?q_like= and ?q[eq]= still filter the column.
func stripQColumnEqFilter(filters []filter.ParsedFilter, hasSearchFields, qPresent bool) []filter.ParsedFilter {
	if !hasSearchFields || !qPresent || len(filters) == 0 {
		return filters
	}
	out := filters[:0]
	for _, f := range filters {
		if f.Op == filter.OpEq && f.Field == "q" && !filter.IsBracketKey(f.Key()) {
			continue
		}
		out = append(out, f)
	}
	return out
}

// Get returns the handler that reads one record by id, with ?include=.
// BeforeGet may add Where clauses (a miss is 404); AfterGet may change the row.
func (ch *CrudHandler) Get() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if !ch.requireScope(w, r, opRead) {
			return
		}
		ctx := r.Context()
		id := ch.pathID(r)
		if id == "" {
			writeJSONError(w, r, http.StatusBadRequest, reasonMissingID)
			return
		}

		q, err := ch.readQuery(r)
		if err != nil {
			writeErrJSON(w, r, http.StatusBadRequest, err, reasonInvalidQuery)
			return
		}
		includes, err := parseIncludeTreeQ(q, ch.Entity, ch.Registry)
		if err != nil {
			writeErrJSON(w, r, http.StatusBadRequest, err, reasonInvalidQuery)
			return
		}

		getPayload := &hook.GetPayload{Request: r, ID: id}
		if ch.Hooks != nil {
			if err := runHooks(ch.Hooks, ctx, hook.BeforeGet, getPayload); err != nil {
				writeErrJSON(w, r, http.StatusBadRequest, err, i18n.Phrase{})
				return
			}
		}

		cols, err := ch.projectFromRequestQ(q)
		if err != nil {
			writeErrJSON(w, r, http.StatusBadRequest, err, reasonInvalidQuery)
			return
		}
		qb := query.Select(ch.keyedCols(cols)...)
		qb.From(ch.Entity.GetTable())
		kc, ka := ch.keyWhere(id)
		qb.Where(kc, ka...)
		ch.ApplyTenantScope(qb, r)
		ch.ApplyOwnerScope(qb, r)
		ch.ApplyReadScope(qb, r)
		ch.applySoftDeleteFilterQ(qb, q, r.Context())
		for _, c := range getPayload.Where {
			qb.Where(c.SQL, c.Args...)
		}

		sqlStr, args := qb.Build()
		result, err := ch.scanOwn(ctx, id, cols, sqlStr, args)
		if err != nil {
			if err == sql.ErrNoRows {
				writeJSONError(w, r, http.StatusNotFound, i18n.StatusPhrase(http.StatusNotFound))
				return
			}
			logf("crud: get query failed: %v", err)
			writeJSONError(w, r, http.StatusInternalServerError, i18n.StatusPhrase(http.StatusInternalServerError))
			return
		}

		if err := ch.applyIncludeTree(withRealRequest(WithReadHooks(ctx), r), []map[string]any{result}, includes); err != nil {
			writeIncludeError(w, r, "get", err)
			return
		}

		ch.maskOwn(ctx, result)
		if ch.Hooks != nil {
			getPayload.Result = result
			if err := runHooks(ch.Hooks, ctx, hook.AfterGet, getPayload); err != nil {
				logf("crud: after-get hook failed: %v", err)
				writeJSONError(w, r, http.StatusInternalServerError, i18n.StatusPhrase(http.StatusInternalServerError))
				return
			}
			result = getPayload.Result
		}

		writeJSONStatus(w, r, http.StatusOK, singleResponse{Data: result})
	}
}

// Create returns the handler that creates a record from a JSON or multipart
// body, running BeforeCreate, INSERT and AfterCreate in one transaction.
// Multipart parts for Image/File fields are stored through Storage.
func (ch *CrudHandler) Create() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		r = r.WithContext(withEventStamps(r.Context()))
		w.Header().Set("Cache-Control", "no-store")
		if err := enforceJSONContentType(r); err != nil {
			writeContentTypeError(w, r, err)
			return
		}
		if !ch.requireScope(w, r, opCreate) {
			return
		}
		limitRequestBody(w, r)
		body, savedFiles, err := ch.readRequestBody(r)
		if err != nil {
			ch.deleteSavedUploads(r.Context(), savedFiles)
			writeBodyError(w, r, err)
			return
		}

		var result map[string]any
		var hidden bool
		sent := sentKeys(body)
		err = ch.inTx(WithUploadedKeys(WithAuditRequest(r.Context(), r), savedFiles...), func(ctx context.Context, ch *CrudHandler) error {
			res, err := ch.doCreate(ctx, r, body)
			if err != nil {
				return err
			}
			result = res
			hidden, err = ch.readScopeHidesRow(ctx, res)
			return err
		})
		if err != nil {
			ch.deleteSavedUploads(r.Context(), savedFiles)
			writeCRUDError(w, r, ch.withConstraintFields(err, sent), ch.convertKey)
			return
		}

		ch.EmitEvent(r.Context(), event.EntityCreated, result)

		var resp map[string]any
		if hidden || ch.getScopeHidesRow(r, result) {
			resp = ch.identityOnly(result)
		} else {
			var hookErr error
			resp, hookErr = ch.runResponseHooks(r, result)
			if hookErr != nil {
				logf("crud: after-get hook failed on create response, returning id only: %v", hookErr)
				resp = ch.identityOnly(result)
			}
		}

		writeJSONStatus(w, r, http.StatusCreated, singleResponse{Data: resp})
	}
}

// Update returns the handler that updates a record by id, running
// BeforeUpdate, UPDATE and AfterUpdate in one transaction. Bodies as Create.
func (ch *CrudHandler) Update() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		r = r.WithContext(withEventStamps(r.Context()))
		w.Header().Set("Cache-Control", "no-store")
		if err := enforceJSONContentType(r); err != nil {
			writeContentTypeError(w, r, err)
			return
		}
		if !ch.requireScope(w, r, opUpdate) {
			return
		}
		id := ch.pathID(r)
		if id == "" {
			writeJSONError(w, r, http.StatusBadRequest, reasonMissingID)
			return
		}

		limitRequestBody(w, r)
		body, savedFiles, err := ch.readRequestBody(r)
		if err != nil {
			ch.deleteSavedUploads(r.Context(), savedFiles)
			writeBodyError(w, r, err)
			return
		}

		var result map[string]any
		var hidden bool
		sent := sentKeys(body)
		err = ch.inTx(WithUploadedKeys(WithAuditRequest(r.Context(), r), savedFiles...), func(ctx context.Context, ch *CrudHandler) error {
			res, err := ch.doUpdate(ctx, r, id, body)
			if err != nil {
				return err
			}
			result = res
			hidden, err = ch.readScopeHidesRow(ctx, res)
			return err
		})
		if err != nil {
			ch.deleteSavedUploads(r.Context(), savedFiles)
			writeCRUDError(w, r, ch.withConstraintFields(err, sent), ch.convertKey)
			return
		}

		ch.EmitEvent(r.Context(), event.EntityUpdated, result)
		ch.writeUpdated(w, r, result, hidden)
	}
}

// writeUpdated answers a committed update's row through the AfterGet hooks,
// or its id alone when the caller may not read it or a hook fails.
func (ch *CrudHandler) writeUpdated(w http.ResponseWriter, r *http.Request, result map[string]any, hidden bool) {
	var resp map[string]any
	if hidden || ch.getScopeHidesRow(r, result) {
		resp = ch.identityOnly(result)
	} else {
		var hookErr error
		resp, hookErr = ch.runResponseHooks(r, result)
		if hookErr != nil {
			logf("crud: after-get hook failed on update response, returning id only: %v", hookErr)
			resp = ch.identityOnly(result)
		}
	}

	writeJSONStatus(w, r, http.StatusOK, singleResponse{Data: resp})
}

// Delete returns the handler that deletes a record by id (sets deleted_at
// under SoftDelete), running BeforeDelete, the write and AfterDelete in one
// transaction.
func (ch *CrudHandler) Delete() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		r = r.WithContext(withEventStamps(r.Context()))
		w.Header().Set("Cache-Control", "no-store")
		if !ch.requireScope(w, r, opDelete) {
			return
		}
		id := ch.pathID(r)
		if id == "" {
			writeJSONError(w, r, http.StatusBadRequest, reasonMissingID)
			return
		}

		err := ch.inTx(WithAuditRequest(r.Context(), r), func(ctx context.Context, ch *CrudHandler) error {
			return ch.doDelete(ctx, r, id)
		})
		if err != nil {
			writeCRUDError(w, r, err, ch.convertKey)
			return
		}

		ch.EmitEvent(r.Context(), event.EntityDeleted, ch.keyPayload(id))

		w.WriteHeader(http.StatusNoContent)
	}
}

// ValidationError is the field-level refusal Create, Update and Upsert
// return when validation rejects the body. Branch on it with errors.As.
type ValidationError struct{ fields map[string][]schema.Violation }

// Error is the generic "validation failed"; Fields has the detail.
func (e *ValidationError) Error() string { return handler.ReasonValidationFailed.English() }

// Fields returns the violations by column. Treat it as read-only.
func (e *ValidationError) Fields() map[string][]schema.Violation { return e.fields }

// NewValidationError builds a ValidationError from field violations, for
// hooks and tests. Define each message with i18n.Define("validation.<code>", …).
func NewValidationError(fields map[string][]schema.Violation) *ValidationError {
	return &ValidationError{fields: fields}
}

// validationFailed heads every validation-shaped refusal.
var validationFailed = schema.Violation{Reason: handler.ReasonValidationFailed}

// Sentinel errors for CRUD flows.
var (
	errNotFound         = errors.New("not found")
	errNoFieldsToUpdate = errors.New("no fields to update")
)

// refusal is one error response; HTTP routes and batch items share it.
type refusal struct {
	code                 int
	text                 string // "error"
	reason               string // "reason", sent when set
	fieldsShape          bool
	fields, fieldReasons map[string][]string
	extra                map[string]any
}

func (f refusal) body() map[string]any {
	body := map[string]any{"error": f.text}
	if f.reason != "" {
		body["reason"] = f.reason
	}
	if f.fieldsShape {
		body["fields"] = f.fields
		body["field_reasons"] = f.fieldReasons
	}
	maps.Copy(body, f.extra)
	return body
}

// phraseRefusal is reason translated into ctx's locale, with its code.
func phraseRefusal(ctx context.Context, code int, reason i18n.Phrase, params ...map[string]any) refusal {
	return refusal{code: code, text: reason.Text(ctx, params...), reason: reason.Code()}
}

func errRefusal(ctx context.Context, code int, err error, frame i18n.Phrase) refusal {
	text, reason := i18n.ErrorText(ctx, err, frame)
	return refusal{code: code, text: text, reason: reason}
}

func fieldsRefusal(ctx context.Context, code int, head schema.Violation, fields map[string][]schema.Violation, extra map[string]any) refusal {
	messages, codes := handler.ViolationBodies(ctx, fields)
	return refusal{
		code: code, text: head.Text(ctx), reason: head.Reason.Code(),
		fieldsShape: true, fields: messages, fieldReasons: codes, extra: extra,
	}
}

func writeCRUDError(w http.ResponseWriter, r *http.Request, err error, wireKeys ...func(string) string) {
	f := crudRefusal(r.Context(), err, wireKeys...)
	writeErrorBody(w, r, f.code, f.body())
}

// crudRefusal maps a CRUD-flow error to its status; an unknown one is a
// generic 500 whose text stays in the log.
func crudRefusal(ctx context.Context, err error, wireKeys ...func(string) string) refusal {
	fieldKey := func(column string) string {
		if len(wireKeys) > 0 {
			return wireKeys[0](column)
		}
		return column
	}
	if errors.Is(err, ErrReentrantMove) || errors.Is(err, errImageSourceChanged) {
		return phraseRefusal(ctx, http.StatusConflict, i18n.StatusPhrase(http.StatusConflict))
	}
	if isHookPanic(err) {
		logf("crud: internal error: %v", err)
		return phraseRefusal(ctx, http.StatusInternalServerError, i18n.StatusPhrase(http.StatusInternalServerError))
	}
	if bhe, ok := errors.AsType[*beforeHookError](err); ok {
		return errRefusal(ctx, http.StatusBadRequest, bhe, i18n.Phrase{})
	}
	if ve, ok := errors.AsType[*ValidationError](err); ok {
		fields := make(map[string][]schema.Violation)
		for column, violations := range ve.Fields() {
			fields[fieldKey(column)] = violations
		}
		return fieldsRefusal(ctx, http.StatusBadRequest, validationFailed, fields, nil)
	}
	if se, ok := errors.AsType[*StateError](err); ok {
		p, params := se.Phrased()
		v := schema.Violation{Reason: p, Params: params}
		return fieldsRefusal(ctx, http.StatusUnprocessableEntity, v,
			map[string][]schema.Violation{fieldKey(se.Field): {v}},
			map[string]any{"moves": nonNil(se.Moves)})
	}
	if tce, ok := errors.AsType[*TransitionConflictError](err); ok {
		f := errRefusal(ctx, http.StatusConflict, tce, i18n.Phrase{})
		f.extra = map[string]any{"current": tce.Current, "moves": nonNil(tce.Moves)}
		return f
	}
	if tde, ok := errors.AsType[*transitionDeniedError](err); ok {
		return errRefusal(ctx, http.StatusForbidden, tde, i18n.Phrase{})
	}
	if errors.Is(err, ErrUnknownTransition) {
		return phraseRefusal(ctx, http.StatusNotFound, i18n.StatusPhrase(http.StatusNotFound))
	}
	if tme, ok := errors.AsType[*tenantMissingError](err); ok {
		return errRefusal(ctx, http.StatusBadRequest, tme, i18n.Phrase{})
	}
	if errors.Is(err, errNotFound) {
		return phraseRefusal(ctx, http.StatusNotFound, i18n.StatusPhrase(http.StatusNotFound))
	}
	if errors.Is(err, errNoFieldsToUpdate) {
		return phraseRefusal(ctx, http.StatusBadRequest, reasonNoFieldsToUpdate)
	}
	if isUniqueViolation(err) {
		return conflictRefusal(ctx, err, i18n.StatusPhrase(http.StatusConflict))
	}
	if isForeignKeyViolation(err) {
		return conflictRefusal(ctx, err, reasonReferenced)
	}
	if cf, ok := errors.AsType[*constraintFieldsError](err); ok && isNotNullViolation(err) {
		return fieldsRefusal(ctx, http.StatusBadRequest, validationFailed, cf.fields, nil)
	}
	logf("crud: internal error: %v", err)
	return phraseRefusal(ctx, http.StatusInternalServerError, i18n.StatusPhrase(http.StatusInternalServerError))
}

func writeBatchItemError(w http.ResponseWriter, r *http.Request, index int, err error) {
	detail, _ := i18n.ErrorText(r.Context(), err, i18n.Phrase{})
	writeJSONError(w, r, http.StatusBadRequest, reasonBatchItem, map[string]any{"index": index, "detail": detail})
}

// isUniqueViolation sniffs the drivers' UNIQUE-violation messages.
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

// isForeignKeyViolation sniffs the drivers' FOREIGN KEY-violation messages.
func isForeignKeyViolation(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	for _, sig := range []string{
		"FOREIGN KEY constraint failed",
		"violates foreign key constraint",
		"SQLSTATE 23503",
		"Error 1451",
		"Error 1452",
	} {
		if strings.Contains(msg, sig) {
			return true
		}
	}
	return false
}

// parsePaginationValues reads ?page and ?limit (alias ?per_page, ?limit wins),
// defaulting to 1 and 20 and clamping the size to listLimitCap.
func parsePaginationValues(q url.Values, entityMax int) (page, perPage int) {
	page = 1
	perPage = 20

	if v := q.Get("page"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			page = n
		}
	}

	maxPerPage := listLimitCap(entityMax)

	sizeParam := q.Get("limit")
	if sizeParam == "" {
		sizeParam = q.Get("per_page")
	}
	if sizeParam != "" {
		if n, err := strconv.Atoi(sizeParam); err == nil && n > 0 {
			perPage = n
		}
	}
	if perPage > maxPerPage {
		perPage = maxPerPage
	}
	return
}

// explicitOffsetValues reads a non-negative ?offset=.
func explicitOffsetValues(q url.Values) (int, bool) {
	v := q.Get("offset")
	if v == "" {
		return 0, false
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 0 {
		return 0, false
	}
	return n, true
}

// maxListOffset is MaxOffset, or the page cap × 1000.
func (ch *CrudHandler) maxListOffset() int {
	if ch.MaxOffset > 0 {
		return ch.MaxOffset
	}
	return listLimitCap(ch.Entity.Config.Pagination.MaxListLimit) * 1000
}

// requireBoundedOffset refuses with 400 a skip, explicit or page-derived,
// past maxListOffset, reckoned exactly at any size.
func (ch *CrudHandler) requireBoundedOffset(w http.ResponseWriter, r *http.Request, q url.Values, page, perPage int) bool {
	offset := requestedOffset(q, perPage)
	if offset.Cmp(big.NewInt(int64(ch.maxListOffset()))) <= 0 {
		return true
	}
	var shown any = offset.String()
	if offset.IsInt64() {
		shown = offset.Int64()
	}
	writeJSONError(w, r, http.StatusBadRequest, reasonOffsetTooLarge, map[string]any{"offset": shown, "max": ch.maxListOffset()})
	return false
}

func requestedOffset(q url.Values, perPage int) *big.Int {
	whole := func(v string) (*big.Int, bool) {
		n, ok := new(big.Int).SetString(v, 10)
		return n, ok && n.Sign() >= 0
	}
	if v := q.Get("offset"); v != "" {
		if n, ok := whole(v); ok {
			return n
		}
	}
	if v := q.Get("page"); v != "" {
		if n, ok := whole(v); ok && n.Sign() > 0 {
			n.Sub(n, big.NewInt(1))
			return n.Mul(n, big.NewInt(int64(perPage)))
		}
	}
	return new(big.Int)
}

// listLimitCap is the per-request row cap of every list path: 100, or
// MaxListLimit up to streamListThreshold.
func listLimitCap(entityMax int) int {
	limitCap := 100
	if entityMax > 0 {
		limitCap = min(entityMax, streamListThreshold)
	}
	return limitCap
}

// ListLimits returns a list's default page size and the ?limit cap for an
// entity whose Pagination.MaxListLimit is entityMax.
func ListLimits(entityMax int) (def, max int) {
	max = listLimitCap(entityMax)
	return min(20, max), max
}

func scanRows(rows *sql.Rows, cols []string, keyFunc func(string) string) ([]map[string]any, error) {
	return scanRowsForEntity(rows, cols, keyFunc, nil)
}

func scanRowsWithKeys(rows *sql.Rows, cols, keys []string) ([]map[string]any, error) {
	return scanRowsWithKeysForEntity(rows, cols, keys, nil)
}

func scanRowsForEntity(rows *sql.Rows, cols []string, keyFunc func(string) string, fields []schema.Field) ([]map[string]any, error) {
	return scanRowsWithKeysForEntity(rows, cols, convertedKeys(cols, keyFunc), fields)
}

func scanRowsWithKeysForEntity(rows *sql.Rows, cols, keys []string, fields []schema.Field) ([]map[string]any, error) {
	boolCols := databaseBoolColumnsForEntity(rows, len(cols), fields, cols)
	results := []map[string]any{}
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
			row[keys[i]] = convertDatabaseValue(values[i], boolCols[i])
		}
		results = append(results, row)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return results, nil
}

func scanRow(row *sql.Row, cols []string, keyFunc func(string) string) (map[string]any, error) {
	return scanRowWithBoolColumns(row, cols, keyFunc, nil)
}

func scanRowWithBoolColumns(row *sql.Row, cols []string, keyFunc func(string) string, boolCols []bool) (map[string]any, error) {
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
		isBool := i < len(boolCols) && boolCols[i]
		result[keyFunc(col)] = convertDatabaseValue(values[i], isBool)
	}
	return result, nil
}

func (ch *CrudHandler) boolColumns(cols []string) []bool {
	out := make([]bool, len(cols))
	if ch == nil || ch.Entity == nil {
		return out
	}
	return entityBoolColumns(ch.snapshotFields(), cols)
}

func convertValue(v any) any {
	switch val := v.(type) {
	case []byte:
		return string(val)
	default:
		return val
	}
}

// databaseBoolColumns marks the columns whose database type is boolean, so
// an integer column holding 0 or 1 stays an integer.
func databaseBoolColumns(rows *sql.Rows, n int) []bool {
	out := make([]bool, n)
	types, err := rows.ColumnTypes()
	if err != nil {
		return out
	}
	for i := 0; i < len(types) && i < n; i++ {
		name := strings.ToUpper(types[i].DatabaseTypeName())
		out[i] = strings.Contains(name, "BOOL")
	}
	return out
}

func entityFieldsOf(ent *entity.Entity) []schema.Field {
	if ent == nil {
		return nil
	}
	return ent.GetFields()
}

func databaseBoolColumnsForEntity(rows *sql.Rows, n int, fields []schema.Field, cols []string) []bool {
	out := databaseBoolColumns(rows, n)
	for i, isBool := range entityBoolColumns(fields, cols) {
		if i < len(out) && isBool {
			out[i] = true
		}
	}
	return out
}

func entityBoolColumns(fields []schema.Field, cols []string) []bool {
	out := make([]bool, len(cols))
	if fields == nil {
		return out
	}
	types := make(map[string]schema.FieldType, len(fields))
	for _, field := range fields {
		types[field.Name] = field.Type
	}
	for i, col := range cols {
		out[i] = types[col] == schema.Bool
	}
	return out
}

func convertDatabaseValue(v any, boolean bool) any {
	if !boolean {
		return convertValue(v)
	}
	switch val := v.(type) {
	case bool:
		return val
	case int64:
		return val != 0
	case int32:
		return val != 0
	case int:
		return val != 0
	case float64:
		return val != 0
	case []byte:
		return textIsTrue(string(val))
	case string:
		return textIsTrue(val)
	default:
		return convertValue(v)
	}
}

// textIsTrue accepts only "true" and "1", trimmed and in any case.
func textIsTrue(s string) bool {
	s = strings.TrimSpace(s)
	return strings.EqualFold(s, "true") || s == "1"
}

func writeJSONError(w http.ResponseWriter, r *http.Request, code int, reason i18n.Phrase, params ...map[string]any) {
	writeErrorBody(w, r, code, phraseRefusal(r.Context(), code, reason, params...).body())
}

// writeErrJSON answers err; a hook panic answers the generic 500.
func writeErrJSON(w http.ResponseWriter, r *http.Request, code int, err error, frame i18n.Phrase) {
	if isHookPanic(err) {
		logf("crud: internal error: %v", err)
		writeJSONError(w, r, http.StatusInternalServerError, i18n.StatusPhrase(http.StatusInternalServerError))
		return
	}
	writeErrorBody(w, r, code, errRefusal(r.Context(), code, err, frame).body())
}

func writeErrorBody(w http.ResponseWriter, r *http.Request, code int, body map[string]any) {
	w.Header().Set("Cache-Control", "no-store")
	body["success"] = false
	body["code"] = code
	writeJSONStatus(w, r, code, body)
}

// writeJSONStatus marshals body before writing, so a body that cannot
// encode answers 500.
func writeJSONStatus(w http.ResponseWriter, r *http.Request, status int, body any) {
	b, err := json.Marshal(body)
	if err != nil {
		logf("crud: encode response: %v", err)
		writeJSONError(w, r, http.StatusInternalServerError, i18n.StatusPhrase(http.StatusInternalServerError))
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	w.Write(b)
	w.Write([]byte("\n"))
}

func generateFieldValue(strategy schema.AutoGenerate) any {
	switch strategy {
	case schema.AutoUUID:
		return generateUUID()
	case schema.AutoTimestamp:
		return schema.FormatTimestamp(time.Now())
	case schema.AutoIncrement:
		return 0
	default:
		return nil
	}
}

func generateUUID() string {
	return uuid.NewV4().String()
}

var _ fmt.Stringer = (*entity.Entity)(nil)
