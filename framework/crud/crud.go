package crud

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"uuid"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/query"
	"github.com/DonaldMurillo/gofastr/core/schema"
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
	// ImageDeriver, when set, runs over every schema.Image upload to
	// produce stored renditions plus a BlurHash/LQIP. Derived values land
	// in sibling columns the entity declares. See applyDerivedColumns.
	// framework/imagefield provides the implementation.
	ImageDeriver file.ImageDeriver
	// FieldImageDerivers overrides ImageDeriver for specific image fields,
	// keyed by field name. An avatar wants portrait components and animated
	// sources rejected; a hero cover wants wide renditions, one app-wide
	// config cannot express both.
	FieldImageDerivers map[string]file.ImageDeriver
	Events             *event.EventBus // optional; receives entity.created/updated/deleted on commit
	Outbox             EventOutbox     // optional; when set, lifecycle events are staged in-tx (transactional outbox) and delivered to declared consumers by the relay. EmitEvent still notifies Events (real-time lane); the relay does not, so there is no double delivery.
	Registry           entity.Registry // optional; required for nested ?include=author.profile resolution
	// ChildHooks resolves ANOTHER entity's hook registry by name, so rows
	// eager-loaded through ?include= run the read hooks of the entity they
	// belong to rather than this one's. Without it a redaction on the child
	// applies to GET /children but not to the same row one relation hop away.
	// Must not create registries, it is called on the request path.
	// framework.App wires it; nil leaves includes unhooked.
	ChildHooks   func(entityName string) *hook.HookRegistry
	BasePath     string // optional; URL prefix where this entity's routes are mounted (e.g. "/api/v1"). Used by MCP tools to dispatch against the same path the HTTP routes live at; empty = bare "/table".
	MCPNamespace string // optional; when set (e.g. "admin"), MCP tools are named "<ns>.<entity>.<action>" instead of the flat "<entity>_<action>". Empty preserves the historical flat tool names.
	// MaxOffset caps the row skip a list request may ask for, via ?offset=
	// or a deep ?page=. Zero (the default) resolves to the page cap × 1000
	// — 100,000 at the default Pagination.MaxListLimit, scaling with the
	// entity's page cap — so a client cannot force a per-request
	// full-table skip scan. See requireBoundedOffset.
	MaxOffset int
	// EventStreamReauth is how often an established SSE feed re-runs its
	// read-permission gate when idle (the gate also runs on every
	// delivery). Zero resolves to the default, 30s. The re-check cannot
	// be switched off: a stream must not outlive the authority that
	// opened it. See EventStream / WithEventStreamReauth.
	EventStreamReauth time.Duration

	visibleFieldsCache []string
	visibleJSONKeys    []string
	visibleFieldSig    uint64

	// jsonColumns holds the DB column names declared schema.JSON;
	// jsonWireKeys holds the same fields by wire key. The write path
	// needs the column form (it binds by column), the read path the wire
	// form (scanned rows are already key-converted). Both cover hidden
	// fields too, a server-writes create can set one. Rebuilt by
	// refreshFieldCache.
	jsonColumns  map[string]struct{}
	jsonWireKeys map[string]struct{}

	// wireKeyOf maps DB column name → JSON wire key (WireName if set, else
	// case-converted Name). columnOfWire is the reverse. Both cover ALL fields
	// (not just visible ones) so input deserialization can resolve WireNames
	// for write-only fields too. Rebuilt by refreshFieldCache.
	wireKeyOf    map[string]string
	columnOfWire map[string]string
}

// NewCrudHandler creates a new CrudHandler for the given entity and database.
func NewCrudHandler(ent *entity.Entity, db DBExecutor) *CrudHandler {
	ch := &CrudHandler{Entity: ent, DB: db, PrimaryKey: "id", JSONCase: CaseCamel, Hooks: nil}
	ch.refreshFieldCache()
	return ch
}

// WithMaxOffset raises or lowers the list-offset ceiling. Zero keeps the
// default: the page cap × 1000 (100,000 at the default page cap). Values
// below 1 are ignored, the ceiling cannot be removed, only moved.
func (ch *CrudHandler) WithMaxOffset(n int) *CrudHandler {
	if n > 0 {
		ch.MaxOffset = n
	}
	return ch
}

// WithEventStreamReauth sets how often an established SSE feed re-runs its
// read-permission gate when idle. Values below one second are ignored (the
// default is 30s); the re-check itself cannot be disabled, only rescheduled.
func (ch *CrudHandler) WithEventStreamReauth(d time.Duration) *CrudHandler {
	if d >= time.Second {
		ch.EventStreamReauth = d
	}
	return ch
}

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
	if ch.Entity.Config.Scope.MultiTenant {
		tenantID := tenant.GetTenantID(r.Context())
		if tenantID != "" {
			qb.Where(ch.Entity.Config.TenantColumn()+" = $1", tenantID)
		}
	}
}

// ApplyTenantScopeCount adds a tenant_id filter to a count query builder.
// Note: uses PostgreSQL-style $1 placeholders.
func (ch *CrudHandler) ApplyTenantScopeCount(cb *query.CountBuilder, r *http.Request) {
	if ch.Entity.Config.Scope.MultiTenant {
		tenantID := tenant.GetTenantID(r.Context())
		if tenantID != "" {
			cb.Where(ch.Entity.Config.TenantColumn()+" = $1", tenantID)
		}
	}
}

// ApplyTenantScopeUpdate adds a tenant_id filter to an update query builder.
// Note: uses PostgreSQL-style $1 placeholders.
func (ch *CrudHandler) ApplyTenantScopeUpdate(ub *query.UpdateBuilder, r *http.Request) {
	if ch.Entity.Config.Scope.MultiTenant {
		tenantID := tenant.GetTenantID(r.Context())
		if tenantID != "" {
			ub.Where(ch.Entity.Config.TenantColumn()+" = $1", tenantID)
		}
	}
}

// ApplyTenantScopeDelete adds a tenant_id filter to a delete query builder.
// Note: uses PostgreSQL-style $1 placeholders.
func (ch *CrudHandler) ApplyTenantScopeDelete(db *query.DeleteBuilder, r *http.Request) {
	if ch.Entity.Config.Scope.MultiTenant {
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
	if ch.Entity.Config.Scope.MultiTenant {
		tenantID := tenant.GetTenantID(ctx)
		if tenantID != "" {
			data[ch.Entity.Config.TenantColumn()] = tenantID
		}
	}
}

// ApplySoftDeleteFilter adds a deleted_at IS NULL filter unless the caller
// requests trashed records via ?trashed=true AND the request is
// authenticated. An anonymous caller passing ?trashed=true on a public
// list endpoint must not be allowed to enumerate soft-deleted rows,
// that's an information-disclosure path. The query param is honoured
// only when a user is present in the request context.
func (ch *CrudHandler) ApplySoftDeleteFilter(qb *query.QueryBuilder, r *http.Request) {
	ch.applySoftDeleteFilterQ(qb, r.URL.Query(), r.Context())
}

// ApplySoftDeleteFilterCount adds a deleted_at IS NULL filter to a count query.
// Same authentication gate as ApplySoftDeleteFilter.
func (ch *CrudHandler) ApplySoftDeleteFilterCount(cb *query.CountBuilder, r *http.Request) {
	ch.applySoftDeleteFilterCountQ(cb, r.URL.Query(), r.Context())
}

// applySoftDeleteFilterQ is the no-reparse variant of ApplySoftDeleteFilter.
// List/ServeStreamingList parse the URL query once and thread the same
// url.Values through every helper; this variant accepts it directly so the
// soft-delete gate doesn't pay url.URL.Query a second time per call.
func (ch *CrudHandler) applySoftDeleteFilterQ(qb *query.QueryBuilder, q url.Values, ctx context.Context) {
	if ch.Entity.Config.Scope.SoftDelete && !ch.trashedAllowedQ(q, ctx) {
		qb.Where("deleted_at IS NULL")
	}
}

// applySoftDeleteFilterCountQ mirrors applySoftDeleteFilterQ for count queries.
func (ch *CrudHandler) applySoftDeleteFilterCountQ(cb *query.CountBuilder, q url.Values, ctx context.Context) {
	if ch.Entity.Config.Scope.SoftDelete && !ch.trashedAllowedQ(q, ctx) {
		cb.Where("deleted_at IS NULL")
	}
}

// trashedAllowed reports whether the caller may see soft-deleted rows on
// this request. True only when ?trashed=true AND the request carries an
// authenticated user, anonymous callers are denied visibility into
// soft-deleted data regardless of how they ask.
func (ch *CrudHandler) trashedAllowed(r *http.Request) bool {
	return ch.trashedAllowedQ(r.URL.Query(), r.Context())
}

// trashedAllowedQ is the no-reparse variant. The List handler computes it
// once from its single parsed url.Values and reuses the result across the
// count + data query builders.
func (ch *CrudHandler) trashedAllowedQ(q url.Values, ctx context.Context) bool {
	if q.Get("trashed") != "true" {
		return false
	}
	if _, ok := getHandlerUser(ctx); !ok {
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
		ch.wireKeyOf = nil
		ch.columnOfWire = nil
		ch.jsonColumns = map[string]struct{}{}
		ch.jsonWireKeys = map[string]struct{}{}
		return
	}
	fields := ch.Entity.GetFields()
	names := make([]string, 0, len(fields))
	ch.wireKeyOf = make(map[string]string, len(fields))
	ch.columnOfWire = make(map[string]string, len(fields))
	ch.jsonColumns = map[string]struct{}{}
	ch.jsonWireKeys = map[string]struct{}{}
	for _, f := range fields {
		// Build the wire-key maps for ALL fields (visible or not) so input
		// deserialization can resolve a WireName on a write-only field.
		wk := ch.wireKeyOfField(f)
		ch.wireKeyOf[f.Name] = wk
		// First field wins on collision, but a collision cannot reach here:
		// entity.Validate rejects two fields resolving to one wire key at
		// registration, because the write path (this map) and the filter path
		// (filter.ParseFiltersValues, which builds its own alias map) would
		// otherwise disagree about which column the key means.
		//
		// The keep-first branch stays as a belt-and-braces guard for any
		// caller constructing a CrudHandler without going through Validate.
		if _, exists := ch.columnOfWire[wk]; !exists {
			ch.columnOfWire[wk] = f.Name
		}
		if f.Type == schema.JSON {
			ch.jsonColumns[f.Name] = struct{}{}
			ch.jsonWireKeys[wk] = struct{}{}
		}
		if !f.Hidden {
			names = append(names, f.Name)
		}
	}
	ch.visibleFieldsCache = names
	ch.visibleJSONKeys = convertedKeys(names, ch.convertKey)
	ch.visibleFieldSig = ch.fieldCacheSignature()
}

// wireKeyOfField returns the JSON wire key for a single field: WireName
// verbatim when set, else the case-converted DB column name.
func (ch *CrudHandler) wireKeyOfField(f schema.Field) string {
	if f.WireName != "" {
		return f.WireName
	}
	return ch.convertKeyRaw(f.Name)
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
	for i := range ch.JSONCase {
		h ^= uint64(ch.JSONCase[i])
		h *= prime64
	}
	for _, f := range ch.Entity.GetFields() {
		for i := range f.Name {
			h ^= uint64(f.Name[i])
			h *= prime64
		}
		// WireName is part of the signature: two versions of the same entity
		// that rename a field differently must not share a cached key set.
		for i := range f.WireName {
			h ^= uint64(f.WireName[i])
			h *= prime64
		}
		if f.Hidden {
			h ^= 1
		} else {
			h ^= 2
		}
		h *= prime64
		// The type is part of the signature because the JSON encode/decode
		// caches key on it: a field that becomes (or stops being)
		// schema.JSON must invalidate them.
		h ^= uint64(f.Type)
		h *= prime64
	}
	return h
}

// convertKey returns the JSON wire key for a DB column name. When the
// column has a WireName override (ch.wireKeyOf), it is returned verbatim;
// otherwise the configured JSONCase is applied. Callers that only want the
// raw case conversion with no WireName lookup should use convertKeyRaw.
func (ch *CrudHandler) convertKey(col string) string {
	if ch.wireKeyOf != nil {
		if wk, ok := ch.wireKeyOf[col]; ok {
			return wk
		}
	}
	return ch.convertKeyRaw(col)
}

// convertKeyRaw applies the configured JSON casing to a DB column name,
// ignoring any WireName override. Used to seed the wire-key map itself
// (WireName-empty fields) and by callers that explicitly want the derived
// form.
func (ch *CrudHandler) convertKeyRaw(col string) string {
	switch ch.JSONCase {
	case CaseSnake:
		return col
	default: // CaseCamel
		return casing.ToCamel(col)
	}
}

// convertMapKeys applies the configured JSON casing to all keys in a map,
// honouring WireName overrides.
func (ch *CrudHandler) convertMapKeys(m map[string]any) map[string]any {
	if ch.wireKeyOf == nil {
		ch.refreshFieldCache()
	}
	out := make(map[string]any, len(m))
	for k, v := range m {
		if wk, ok := ch.wireKeyOf[k]; ok {
			out[wk] = v
		} else {
			out[ch.convertKeyRaw(k)] = v
		}
	}
	return out
}

// itemKeyFoldsUnambiguous reports whether every distinct wire key in a
// batch item resolves to a distinct column under the handler's fold.
// The envelope decode refuses duplicate top-level keys, but items are
// free-form maps: two DISTINCT keys ("bodyText" and "body_text") can
// still fold onto one column, and unconvertMapKeys would then resolve
// the collision by map iteration order. Batch items never pass through
// CheckTopLevelKeys (it walks raw bytes), so the fold check runs on the
// decoded map instead — exact duplicates cannot survive a decode, the
// fold collision is the detectable shape.
func (ch *CrudHandler) itemKeyFoldsUnambiguous(item map[string]any) error {
	seen := make(map[string]struct{}, len(item))
	for k := range item {
		col := ch.wireKeyColumn(k)
		if _, dup := seen[col]; dup {
			return fmt.Errorf("keys folding onto the same field %q (sent %q and another spelling)", col, k)
		}
		seen[col] = struct{}{}
	}
	return nil
}

// wireKeyColumn folds one JSON wire key onto the DB column the request
// decode will land it on: the wire-key map first (WireName overrides and
// standard case-derived keys), then the raw case conversion fallback. It
// is the per-key form of unconvertMapKeys, and the fold handed to
// handler.CheckTopLevelKeys so two distinct wire keys that resolve to one
// column are refused before the decode instead of racing in map order.
func (ch *CrudHandler) wireKeyColumn(key string) string {
	if ch.columnOfWire == nil {
		ch.refreshFieldCache()
	}
	if col, ok := ch.columnOfWire[key]; ok {
		return col
	}
	return ch.unconvertKeyRaw(key)
}

// unconvertMapKeys reverses JSON wire keys back to DB column names. Each
// input key is matched against the wire-key map first (so WireName overrides
// and the standard case-derived keys both resolve); keys that don't match
// any known wire key fall back to the raw case conversion for backward
// compatibility with callers that send arbitrary keys.
func (ch *CrudHandler) unconvertMapKeys(m map[string]any) map[string]any {
	if ch.columnOfWire == nil {
		ch.refreshFieldCache()
	}
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[ch.wireKeyColumn(k)] = v
	}
	return out
}

// unconvertKeyRaw reverses the JSON casing back to snake_case, ignoring
// WireName overrides.
func (ch *CrudHandler) unconvertKeyRaw(key string) string {
	switch ch.JSONCase {
	case CaseSnake:
		return key
	default: // CaseCamel
		return casing.ToSnake(key)
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
		// Parse the URL query ONCE and thread it through every helper.
		// r.URL.Query() re-parses RawQuery and allocates a fresh
		// url.Values on every call; the previous List body paid that
		// ~13-15× per request (pagination, includes, filters, sort,
		// nested filters, q-column edge case, ?q= search, ?where= tree,
		// ?cursor check, projection, ?stream check, soft-delete gate ×2,
		// explicit offset). Handing the same parsed Values to every
		// helper eliminates the re-parse without changing any semantics.
		q := r.URL.Query()
		page, perPage := parsePaginationValues(q, ch.Entity.Config.Pagination.MaxListLimit)

		// The skip side of pagination is bounded like the limit side: an
		// explicit ?offset= beyond maxListOffset is a per-request
		// deep-skip scan, refused here before any filter or count work
		// runs.
		if !ch.requireBoundedOffset(w, q, page, perPage) {
			return
		}

		includes, err := parseIncludeTreeQ(q, ch.Entity, ch.Registry)
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
		filters, err := filter.ParseFiltersValues(q, ch.Entity.GetFields(), filterOpts...)
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid filters: "+err.Error())
			return
		}

		// q-column edge case: when the entity has SearchFields and ?q= is
		// present, a plain ?q=value would be parsed as an OpEq filter on a
		// column named "q". Drop it, plain ?q= means search. Suffixed ops
		// (?q_like=, ?q_gt=, …) still filter the column.
		filters = stripQColumnEqFilter(filters, len(ch.Entity.Config.SearchFields) > 0, q.Has("q"))

		// Nested filters like ?author.name=alice. Parsed once and applied to
		// both the count + data queries below.
		nested, err := parseNestedFiltersValues(q, ch.Entity, ch.Registry)
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, err.Error())
			return
		}
		// Filtering across a relation reads that relation's rows, so its
		// entity's posture applies, without this the row count is an oracle
		// for values in a column the related entity refuses to serve. This
		// call both refuses the targets the caller may not read and narrows
		// the ones they may to their own rows. The `nested` slice is mutated
		// in place, and every downstream sink (count, data, cursor, stream)
		// reads the same slice.
		if err := ch.scopeNestedFiltersForCaller(r.Context(), nested); err != nil {
			writeIncludeError(w, "list", err)
			return
		}

		// ?q= free-text search: when the entity declares SearchFields and
		// the request carries a non-blank ?q=, build filter.SearchConditions
		// and append them as Where clauses. They feed the count, data,
		// cursor, and stream sinks uniformly because listPayload.Where is
		// applied to each. Zero signature changes.
		searchWheres := ch.searchWhereClausesQ(q)

		// ?where=<json> nested predicate tree (OR-groups / nested AND-OR).
		// Compiles to ONE parenthesized WHERE clause that AND-composes with
		// the owner/tenant/soft-delete scopes exactly like the search
		// clauses above, a user OR-group can never widen past a scope.
		treeWheres, err := ch.whereTreeClausesQ(q)
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid where: "+err.Error())
			return
		}
		searchWheres = append(searchWheres, treeWheres...)

		// BeforeList hook, collect any extra WHERE clauses the host wants
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
		if q.Has("cursor") {
			// Keyset mode ignores ?sort=, the cursor fields control ORDER BY,
			// but it must still REFUSE a sort it would refuse anywhere else.
			// Returning early before this check made ?cursor=&sort=<NoQuery>
			// answer 200 where ?sort=<NoQuery> answers 400, so "every query
			// surface refuses it" had an exception reachable by appending one
			// empty parameter.
			if _, err := filter.ParseSortValues(q, ch.Entity.GetFields()); err != nil {
				writeJSONError(w, http.StatusBadRequest, err.Error())
				return
			}
			ch.serveCursorList(ctx, w, r, includes, filters, nested, listPayload.Where)
			return
		}

		sorts, err := filter.ParseSortValues(q, ch.Entity.GetFields())
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, err.Error())
			return
		}

		cols, err := ch.projectFromRequestQ(q)
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, err.Error())
			return
		}
		// Streaming-list opt-in: explicit ?stream=true, or auto-on when the
		// requested limit is huge. Streaming skips include resolution to keep
		// memory bounded, so it CANNOT honour ?include= or per-row AfterList
		// transforms (AfterList runs once over the full slice the stream
		// never materialises). Silently streaming anyway is wrong: includes
		// would vanish, and an AfterList redactor would be BYPASSED, leaking
		// the very fields it exists to hide.
		//
		// Explicit ?stream=true → refuse with 400 so the caller knows their
		// include / hook contract can't be met. Auto-streaming (a huge limit,
		// not an explicit opt-in) → fall through to the buffered path, which
		// resolves includes and runs AfterList correctly. Correctness wins
		// over the streaming memory optimisation when the two conflict.
		explicitStream := q.Get("stream") == "true"
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
			// AfterList), it honours both correctly.
		}

		var total int
		// Count total matching rows
		countQb := query.Count(ch.Entity.GetTable())
		filter.ApplyToCountQuery(countQb, filters)
		ch.ApplyTenantScopeCount(countQb, r)
		ch.ApplyOwnerScopeCount(countQb, r)
		ch.ApplyReadScopeCount(countQb, r)
		ch.applySoftDeleteFilterCountQ(countQb, q, ctx)
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

		// Build data query, select only projected (or all visible by default).
		qb := query.Select(cols...)
		qb.From(ch.Entity.GetTable())
		filter.ApplyToQuery(qb, filters)
		ch.ApplyTenantScope(qb, r)
		ch.ApplyOwnerScope(qb, r)
		ch.ApplyReadScope(qb, r)
		ch.applySoftDeleteFilterQ(qb, q, ctx)
		applyNestedFilters(
			func(sql string, args ...any) { qb.Where(sql, args...) },
			ch.Entity.GetTable(), ch.PrimaryKey, nested,
		)
		for _, c := range listPayload.Where {
			qb.Where(c.SQL, c.Args...)
		}
		filter.ApplySortToQuery(qb, sorts)

		offset := pagination.OffsetForPage(page, perPage)
		// An explicit ?offset= overrides the page-derived offset. The
		// process-module broker paginates by raw offset (it sets ?offset=
		// without ?page=), and it is a documented control param, honoring it
		// here is what makes those requests return the intended window
		// instead of silently serving page 1.
		if o, ok := explicitOffsetValues(q); ok {
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
			pooledRows, err = scanRowsPooledWithKeysForEntity(rows, cols, keys, ch.Entity)
			if err == nil {
				results = *pooledRows
				pooledEncode = true
			}
		} else {
			results, err = scanRowsWithKeysForEntity(rows, cols, keys, ch.Entity)
		}
		if err != nil {
			log.Printf("crud: list scan failed: %v", err)
			writeJSONError(w, http.StatusInternalServerError, "internal server error")
			return
		}
		// This path scans through the keyed/pooled scanners rather than
		// ch.scanMany, so it decodes its own JSON columns.
		ch.decodeJSONRows(results)

		if err := ch.applyIncludeTree(withRealRequest(WithReadHooks(ctx), r), results, includes); err != nil {
			writeIncludeError(w, "list", err)
			return
		}

		// AfterList hook, host can redact / transform / drop rows.
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
// or ?q= is blank, the entity then ignores ?q= exactly as before
// (back-compat). The conditions AND-compose safely with owner/tenant/
// soft-delete scopes because the query builder wraps each Where clause
// in parens.
func (ch *CrudHandler) searchWhereClauses(r *http.Request) []hook.WhereClause {
	return ch.searchWhereClausesQ(r.URL.Query())
}

// searchWhereClausesQ is the no-reparse variant. Accepts a pre-parsed
// url.Values so the List handler can share its single parse across every
// helper that previously called r.URL.Query() itself.
func (ch *CrudHandler) searchWhereClausesQ(q url.Values) []hook.WhereClause {
	if len(ch.Entity.Config.SearchFields) == 0 {
		return nil
	}
	qVal := q.Get("q")
	conds := filter.SearchConditions(ch.Entity.Config.SearchFields, qVal)
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
	return ch.whereTreeClausesQ(r.URL.Query())
}

// whereTreeClausesQ is the no-reparse variant of whereTreeClauses.
func (ch *CrudHandler) whereTreeClausesQ(q url.Values) ([]hook.WhereClause, error) {
	raw := q.Get("where")
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
// named "q", plain ?q= means search (not filter on the column). Suffixed
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
		// A row outside the read scope must 404, not 403: the filter lives
		// in the WHERE clause, so the row is simply not found: the caller
		// must not learn it exists. Same shape as the owner-scope path.
		ch.ApplyReadScope(qb, r)
		ch.ApplySoftDeleteFilter(qb, r)
		for _, c := range getPayload.Where {
			qb.Where(c.SQL, c.Args...)
		}

		sqlStr, args := qb.Build()
		row := ch.DB.QueryRowContext(ctx, sqlStr, args...)

		result, err := ch.scanOne(row, cols)
		if err != nil {
			if err == sql.ErrNoRows {
				writeJSONError(w, http.StatusNotFound, "not found")
				return
			}
			log.Printf("crud: get query failed: %v", err)
			writeJSONError(w, http.StatusInternalServerError, "internal server error")
			return
		}

		if err := ch.applyIncludeTree(withRealRequest(WithReadHooks(ctx), r), []map[string]any{result}, includes); err != nil {
			writeIncludeError(w, "get", err)
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
		limitRequestBody(w, r)
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

		// AfterGet over the response body. RETURNING gives back every visible
		// column, including ones the caller never sent, so without this a
		// create echoes stored values that GET masks. AfterCreate has already
		// run above; this is the read-shaped view of the same row.
		//
		// A hook error degrades the body to the new row's id rather than
		// answering 500: the row is committed and the event has shipped, so a
		// 500 would be a lie the caller acts on by retrying, creating it
		// twice. See identityOnly.
		body, hookErr := ch.runResponseHooks(r, result)
		if hookErr != nil {
			log.Printf("crud: after-get hook failed on create response, returning id only: %v", hookErr)
			body = ch.identityOnly(result)
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(singleResponse{Data: body})
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

		limitRequestBody(w, r)
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

		// AfterGet over the response body. See the note on Create. A partial
		// PUT/PATCH otherwise returns stored values for every field the
		// caller did not send.
		// A hook error degrades to the id, not a 500, the update is already
		// committed. See identityOnly.
		body, hookErr := ch.runResponseHooks(r, result)
		if hookErr != nil {
			log.Printf("crud: after-get hook failed on update response, returning id only: %v", hookErr)
			body = ch.identityOnly(result)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(singleResponse{Data: body})
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
	if bhe, ok := errors.AsType[*beforeHookError](err); ok {
		writeJSONError(w, http.StatusBadRequest, bhe.Error())
		return
	}
	if ve, ok := errors.AsType[*ValidationError](err); ok {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]any{
			"error":   "validation failed",
			"success": false,
			"fields":  ve.Fields(),
		})
		return
	}
	if tme, ok := errors.AsType[*tenantMissingError](err); ok {
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
		// The error message itself is generic, we deliberately don't
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
// layer is otherwise driver-agnostic. False positives are rare,
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
// the cap, that path is opt-in per entity (MaxListLimit > 100) so
// public endpoints can't be coerced into 10× larger responses by
// adding a query param. When the entity has explicitly raised the
// limit, the streaming-list path uses min(MaxListLimit, streamListThreshold).
func parsePagination(r *http.Request, entityMax int) (page, perPage int) {
	return parsePaginationValues(r.URL.Query(), entityMax)
}

func parsePaginationValues(q url.Values, entityMax int) (page, perPage int) {
	page = 1
	perPage = 20

	if v := q.Get("page"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			page = n
		}
	}

	maxPerPage := listLimitCap(entityMax)

	// ?limit is the canonical page-size param; ?per_page is accepted as an
	// alias (a common REST convention) so a client using it gets the size it
	// asked for rather than a silent default. ?limit wins when both are sent.
	sizeParam := q.Get("limit")
	if sizeParam == "" {
		sizeParam = q.Get("per_page")
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
// caps the row count, and requireBoundedOffset refuses the skip side beyond
// maxListOffset, so an oversized offset never reaches the query.
func explicitOffset(r *http.Request) (int, bool) {
	return explicitOffsetValues(r.URL.Query())
}

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

// maxListOffset is the effective offset ceiling for this handler:
// MaxOffset when set, otherwise the page cap × 1000. The limit side of
// pagination is clamped to listLimitCap; the skip side gets 1000× that
// headroom so every legitimate deep page fits while a single request
// cannot force a full-table skip scan on a populated table.
func (ch *CrudHandler) maxListOffset() int {
	if ch.MaxOffset > 0 {
		return ch.MaxOffset
	}
	return listLimitCap(ch.Entity.Config.Pagination.MaxListLimit) * 1000
}

// requireBoundedOffset refuses a list request whose skip, explicit
// (?offset=) or page-derived (?page= × ?limit=), exceeds maxListOffset:
// both spellings reach the same OFFSET clause and the same deep skip
// scan, so both are refused with 400 rather than served as an empty
// window at full cost. A page within the cap past the last row still
// returns 200 with no data.
func (ch *CrudHandler) requireBoundedOffset(w http.ResponseWriter, q url.Values, page, perPage int) bool {
	offset, ok := explicitOffsetValues(q)
	if !ok {
		offset = pagination.OffsetForPage(page, perPage)
	}
	if offset <= ch.maxListOffset() {
		return true
	}
	writeJSONError(w, http.StatusBadRequest,
		fmt.Sprintf("offset %d exceeds the maximum %d", offset, ch.maxListOffset()))
	return false
}

// listLimitCap is the effective per-request row cap for an entity:
// the global default (100) unless EntityConfig.MaxListLimit raises or
// lowers it, never above streamListThreshold. Shared by the offset,
// streaming, and cursor list paths so no path can exceed the cap.
func listLimitCap(entityMax int) int {
	limitCap := 100
	if entityMax > 0 {
		limitCap = min(entityMax, streamListThreshold)
	}
	return limitCap
}

// scanRows scans all rows into a slice of maps, applying keyFunc to column names.
// scanRowsPooled is the pool-backed version in pool.go.
func scanRows(rows *sql.Rows, cols []string, keyFunc func(string) string) ([]map[string]any, error) {
	return scanRowsForEntity(rows, cols, keyFunc, nil)
}

func scanRowsWithKeys(rows *sql.Rows, cols, keys []string) ([]map[string]any, error) {
	return scanRowsWithKeysForEntity(rows, cols, keys, nil)
}

func scanRowsForEntity(rows *sql.Rows, cols []string, keyFunc func(string) string, ent *entity.Entity) ([]map[string]any, error) {
	return scanRowsWithKeysForEntity(rows, cols, convertedKeys(cols, keyFunc), ent)
}

func scanRowsWithKeysForEntity(rows *sql.Rows, cols, keys []string, ent *entity.Entity) ([]map[string]any, error) {
	boolCols := databaseBoolColumnsForEntity(rows, len(cols), ent, cols)
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
			row[keys[i]] = convertDatabaseValue(values[i], boolCols[i])
		}
		results = append(results, row)
	}
	// rows.Next() returning false can mean EOF OR a mid-iteration error (a
	// dropped connection, a read fault). Without this check the read path
	// would silently return partial/empty results as success, the eager
	// loaders already guard this; the primary scanner must too.
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return results, nil
}

// scanRow scans a single row into a map, applying keyFunc to column names.
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
	types := make(map[string]schema.FieldType, len(ch.Entity.GetFields()))
	for _, field := range ch.Entity.GetFields() {
		types[field.Name] = field.Type
	}
	for i, col := range cols {
		out[i] = types[col] == schema.Bool
	}
	return out
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

// databaseBoolColumns records which result columns have a boolean database
// type. database/sql exposes SQLite booleans as int64(0|1) with modernc.org/
// sqlite, while other drivers return bool. Keep the row shape driver-neutral
// without turning ordinary integer columns containing 0 or 1 into booleans.
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

func databaseBoolColumnsForEntity(rows *sql.Rows, n int, ent *entity.Entity, cols []string) []bool {
	out := databaseBoolColumns(rows, n)
	for i, isBool := range entityBoolColumns(ent, cols) {
		if i < len(out) && isBool {
			out[i] = true
		}
	}
	return out
}

func entityBoolColumns(ent *entity.Entity, cols []string) []bool {
	out := make([]bool, len(cols))
	if ent == nil {
		return out
	}
	types := make(map[string]schema.FieldType, len(ent.GetFields()))
	for _, field := range ent.GetFields() {
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

// textIsTrue decodes a textual boolean. Drivers disagree on whether a TEXT
// column arrives as string or []byte, so both routes share this to keep the
// same bytes from decoding two different ways.
//
// Note the deliberate asymmetry with the numeric branches above, which treat
// any non-zero as true: text accepts only "true" and "1", so "2" is false.
// Numeric 2 comes from a real driver widening a bool; textual "2" does not,
// and guessing at it would invent a value the column never held.
func textIsTrue(s string) bool {
	s = strings.TrimSpace(s)
	return strings.EqualFold(s, "true") || s == "1"
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
		// Microsecond precision, FIXED WIDTH (.000000 keeps trailing
		// zeros). Whole-second resolution made two rows written in the
		// same second share an identical created_at, which stalls
		// single-field cursor pagination on ties, the documented
		// tiebreak. Microseconds match PostgreSQL TIMESTAMPTZ storage
		// (its max), so the value round-trips losslessly on Postgres;
		// SQLite TEXT keeps the exact string. Fixed width is load-
		// bearing: SQLite compares these as strings, and a zero-stripped
		// "…07.5Z" sorts AFTER "…07.5001Z" while being chronologically
		// earlier, mis-ordering both the cursor keyset and ORDER BY.
		return time.Now().UTC().Format("2006-01-02T15:04:05.000000Z07:00")
	case schema.AutoIncrement:
		return 0 // placeholder, real increment handled by DB
	default:
		return nil
	}
}

// generateUUID creates a new random UUID v4 string.
func generateUUID() string {
	return uuid.NewV4().String()
}

// compile-time check
var _ fmt.Stringer = (*entity.Entity)(nil)
