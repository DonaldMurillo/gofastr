package crud

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/gofastr/gofastr/core/query"
	"github.com/gofastr/gofastr/core/schema"
	"github.com/gofastr/gofastr/core/upload"
	"github.com/gofastr/gofastr/framework/db"
	"github.com/gofastr/gofastr/framework/entity"
	"github.com/gofastr/gofastr/framework/event"
	"github.com/gofastr/gofastr/framework/filter"
	"github.com/gofastr/gofastr/framework/hook"
	"github.com/gofastr/gofastr/framework/internal/casing"
	"github.com/gofastr/gofastr/framework/tenant"
)

// beforeHookError flags a BeforeCreate/BeforeUpdate/BeforeDelete hook
// rejection so the caller can map it to 400 instead of 500.
type beforeHookError struct{ err error }

func (e *beforeHookError) Error() string { return e.err.Error() }
func (e *beforeHookError) Unwrap() error { return e.err }

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
	Registry   entity.Registry          // optional; required for nested ?include=author.profile resolution
}

// NewCrudHandler creates a new CrudHandler for the given entity and database.
func NewCrudHandler(ent *entity.Entity, db DBExecutor) *CrudHandler {
	return &CrudHandler{Entity: ent, DB: db, PrimaryKey: "id", JSONCase: CaseCamel, Hooks: nil}
}

// WithJSONCase sets the JSON casing strategy for the handler.
func (ch *CrudHandler) WithJSONCase(c JSONCase) *CrudHandler {
	ch.JSONCase = c
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

// ApplyTenantScope adds a tenant_id filter to the query when the entity
// is configured for multi-tenancy and a tenant ID is present in the context.
// Note: uses PostgreSQL-style $1 placeholders.
func (ch *CrudHandler) ApplyTenantScope(qb *query.QueryBuilder, r *http.Request) {
	if ch.Entity.Config.MultiTenant {
		tenantID := tenant.GetTenantID(r.Context())
		if tenantID != "" {
			qb.Where("tenant_id = $1", tenantID)
		}
	}
}

// ApplyTenantScopeCount adds a tenant_id filter to a count query builder.
// Note: uses PostgreSQL-style $1 placeholders.
func (ch *CrudHandler) ApplyTenantScopeCount(cb *query.CountBuilder, r *http.Request) {
	if ch.Entity.Config.MultiTenant {
		tenantID := tenant.GetTenantID(r.Context())
		if tenantID != "" {
			cb.Where("tenant_id = $1", tenantID)
		}
	}
}

// ApplyTenantScopeUpdate adds a tenant_id filter to an update query builder.
// Note: uses PostgreSQL-style $1 placeholders.
func (ch *CrudHandler) ApplyTenantScopeUpdate(ub *query.UpdateBuilder, r *http.Request) {
	if ch.Entity.Config.MultiTenant {
		tenantID := tenant.GetTenantID(r.Context())
		if tenantID != "" {
			ub.Where("tenant_id = $1", tenantID)
		}
	}
}

// ApplyTenantScopeDelete adds a tenant_id filter to a delete query builder.
// Note: uses PostgreSQL-style $1 placeholders.
func (ch *CrudHandler) ApplyTenantScopeDelete(db *query.DeleteBuilder, r *http.Request) {
	if ch.Entity.Config.MultiTenant {
		tenantID := tenant.GetTenantID(r.Context())
		if tenantID != "" {
			db.Where("tenant_id = $1", tenantID)
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
			data["tenant_id"] = tenantID
		}
	}
}

// ApplySoftDeleteFilter adds a deleted_at IS NULL filter unless the caller
// requests trashed records via ?trashed=true.
func (ch *CrudHandler) ApplySoftDeleteFilter(qb *query.QueryBuilder, r *http.Request) {
	if ch.Entity.Config.SoftDelete {
		showTrashed := r.URL.Query().Get("trashed") == "true"
		if !showTrashed {
			qb.Where("deleted_at IS NULL")
		}
	}
}

// ApplySoftDeleteFilterCount adds a deleted_at IS NULL filter to a count query.
func (ch *CrudHandler) ApplySoftDeleteFilterCount(cb *query.CountBuilder, r *http.Request) {
	if ch.Entity.Config.SoftDelete {
		showTrashed := r.URL.Query().Get("trashed") == "true"
		if !showTrashed {
			cb.Where("deleted_at IS NULL")
		}
	}
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
	var names []string
	for _, f := range ch.Entity.GetFields() {
		if !f.Hidden {
			names = append(names, f.Name)
		}
	}
	return names
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
func (ch *CrudHandler) List() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		page, perPage := parsePagination(r)

		includes, err := parseIncludeTree(r, ch.Entity, ch.Registry)
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, err.Error())
			return
		}

		filters, err := filter.ParseFilters(r, ch.Entity.GetFields())
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid filters: "+err.Error())
			return
		}

		// Nested filters like ?author.name=alice. Parsed once and applied to
		// both the count + data queries below.
		nested, err := parseNestedFilters(r, ch.Entity, ch.Registry)
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, err.Error())
			return
		}

		// Cursor pagination is opt-in: presence of the ?cursor key (even
		// empty for first-page) switches to keyset mode and emits the
		// CursorPage envelope.
		if r.URL.Query().Has("cursor") {
			ch.serveCursorList(ctx, w, r, includes, filters, nested)
			return
		}

		sorts := filter.ParseSort(r, ch.Entity.GetFields())

		cols, err := ch.projectFromRequest(r)
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, err.Error())
			return
		}

		// Streaming-list opt-in: explicit ?stream=true, or auto-on when the
		// requested limit is huge. Skips include resolution to keep memory
		// bounded.
		if r.URL.Query().Get("stream") == "true" || perPage >= streamListThreshold {
			ch.ServeStreamingList(ctx, w, r, cols, filters, nested, sorts, perPage)
			return
		}

		// Count total matching rows
		countQb := query.Count(ch.Entity.GetTable())
		filter.ApplyToCountQuery(countQb, filters)
		ch.ApplyTenantScopeCount(countQb, r)
		ch.ApplySoftDeleteFilterCount(countQb, r)
		applyNestedFilters(
			func(sql string, args ...any) { countQb.Where(sql, args...) },
			ch.Entity.GetTable(), ch.PrimaryKey, nested,
		)
		countSQL, countArgs := countQb.Build()
		var total int
		if err := ch.DB.QueryRowContext(ctx, countSQL, countArgs...).Scan(&total); err != nil {
			writeJSONError(w, http.StatusInternalServerError, "count query failed: "+err.Error())
			return
		}

		// Build data query — select only projected (or all visible by default).
		qb := query.Select(cols...)
		qb.From(ch.Entity.GetTable())
		filter.ApplyToQuery(qb, filters)
		ch.ApplyTenantScope(qb, r)
		ch.ApplySoftDeleteFilter(qb, r)
		applyNestedFilters(
			func(sql string, args ...any) { qb.Where(sql, args...) },
			ch.Entity.GetTable(), ch.PrimaryKey, nested,
		)
		filter.ApplySortToQuery(qb, sorts)

		offset := (page - 1) * perPage
		qb.Limit(perPage)
		qb.Offset(offset)

		dataSQL, dataArgs := qb.Build()
		rows, err := ch.DB.QueryContext(ctx, dataSQL, dataArgs...)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "query failed: "+err.Error())
			return
		}
		defer rows.Close()

		results, err := scanRows(rows, cols, ch.convertKey)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "scan failed: "+err.Error())
			return
		}

		if err := ch.applyIncludeTree(ctx, results, includes); err != nil {
			writeJSONError(w, http.StatusInternalServerError, "include failed: "+err.Error())
			return
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
	}
}

// Get returns an http.HandlerFunc that fetches a single entity by ID.
// Honours ?include= eager-loaded relations.
func (ch *CrudHandler) Get() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
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

		cols, err := ch.projectFromRequest(r)
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, err.Error())
			return
		}
		qb := query.Select(cols...)
		qb.From(ch.Entity.GetTable())
		qb.Where(ch.PrimaryKey+" = $1", id)
		ch.ApplyTenantScope(qb, r)
		ch.ApplySoftDeleteFilter(qb, r)

		sqlStr, args := qb.Build()
		row := ch.DB.QueryRowContext(ctx, sqlStr, args...)

		result, err := scanRow(row, cols, ch.convertKey)
		if err != nil {
			if err == sql.ErrNoRows {
				writeJSONError(w, http.StatusNotFound, "not found")
				return
			}
			writeJSONError(w, http.StatusInternalServerError, "query failed: "+err.Error())
			return
		}

		if err := ch.applyIncludeTree(ctx, []map[string]any{result}, includes); err != nil {
			writeJSONError(w, http.StatusInternalServerError, "include failed: "+err.Error())
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(result)
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
		body, err := ch.readRequestBody(r)
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, err.Error())
			return
		}

		var result map[string]any
		err = ch.inTx(r.Context(), func(ctx context.Context, ch *CrudHandler) error {
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
		json.NewEncoder(w).Encode(result)
	}
}

// Update returns an http.HandlerFunc that updates an entity by ID. The hook
// chain (BeforeUpdate → UPDATE → AfterUpdate) runs inside a transaction.
// Accepts application/json or multipart/form-data (same rules as Create).
func (ch *CrudHandler) Update() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if id == "" {
			writeJSONError(w, http.StatusBadRequest, "missing id")
			return
		}

		body, err := ch.readRequestBody(r)
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, err.Error())
			return
		}

		var result map[string]any
		err = ch.inTx(r.Context(), func(ctx context.Context, ch *CrudHandler) error {
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
		json.NewEncoder(w).Encode(result)
	}
}

// Delete returns an http.HandlerFunc that deletes an entity by ID. If the
// entity has SoftDelete=true, it sets deleted_at instead. The hook chain
// (BeforeDelete → DELETE/UPDATE → AfterDelete) runs inside a transaction.
func (ch *CrudHandler) Delete() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if id == "" {
			writeJSONError(w, http.StatusBadRequest, "missing id")
			return
		}

		err := ch.inTx(r.Context(), func(ctx context.Context, ch *CrudHandler) error {
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

// validationError carries field-level validation errors from inside inTx
// out to the response writer.
type validationError struct{ fields map[string][]string }

func (e *validationError) Error() string { return "validation failed" }

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
	var ve *validationError
	if errors.As(err, &ve) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]any{
			"error":   "validation failed",
			"success": false,
			"fields":  ve.fields,
		})
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
	writeJSONError(w, http.StatusInternalServerError, err.Error())
}

// parsePagination extracts page and per_page from query params.
// Defaults: page=1, per_page=20.
func parsePagination(r *http.Request) (page, perPage int) {
	page = 1
	perPage = 20

	if v := r.URL.Query().Get("page"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			page = n
		}
	}
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 100 {
			perPage = n
		}
	}
	return
}

// scanRows scans all rows into a slice of maps, applying keyFunc to column names.
func scanRows(rows *sql.Rows, cols []string, keyFunc func(string) string) ([]map[string]any, error) {
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
		for i, col := range cols {
			row[keyFunc(col)] = convertValue(values[i])
		}
		results = append(results, row)
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
