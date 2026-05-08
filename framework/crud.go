package framework

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/gofastr/gofastr/core/query"
	"github.com/gofastr/gofastr/core/schema"
)

// DBExecutor is the interface for database operations. Both *sql.DB and *sql.Tx satisfy it.
type DBExecutor interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

// CrudHandler provides auto-generated CRUD HTTP handlers for an Entity.
type CrudHandler struct {
	Entity     *Entity
	DB         DBExecutor
	PrimaryKey string        // defaults to "id"
	JSONCase   JSONCase      // casing strategy for JSON keys
	Hooks      *HookRegistry // optional lifecycle hooks
}

// NewCrudHandler creates a new CrudHandler for the given entity and database.
func NewCrudHandler(entity *Entity, db DBExecutor) *CrudHandler {
	return &CrudHandler{Entity: entity, DB: db, PrimaryKey: "id", JSONCase: CaseCamel, Hooks: nil}
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

// applyTenantScope adds a tenant_id filter to the query when the entity
// is configured for multi-tenancy and a tenant ID is present in the context.
// Note: uses PostgreSQL-style $1 placeholders.
func (ch *CrudHandler) applyTenantScope(qb *query.QueryBuilder, r *http.Request) {
	if ch.Entity.Config.MultiTenant {
		tenantID := GetTenantID(r.Context())
		if tenantID != "" {
			qb.Where("tenant_id = $1", tenantID)
		}
	}
}

// applyTenantScopeCount adds a tenant_id filter to a count query builder.
// Note: uses PostgreSQL-style $1 placeholders.
func (ch *CrudHandler) applyTenantScopeCount(cb *query.CountBuilder, r *http.Request) {
	if ch.Entity.Config.MultiTenant {
		tenantID := GetTenantID(r.Context())
		if tenantID != "" {
			cb.Where("tenant_id = $1", tenantID)
		}
	}
}

// applyTenantScopeUpdate adds a tenant_id filter to an update query builder.
// Note: uses PostgreSQL-style $1 placeholders.
func (ch *CrudHandler) applyTenantScopeUpdate(ub *query.UpdateBuilder, r *http.Request) {
	if ch.Entity.Config.MultiTenant {
		tenantID := GetTenantID(r.Context())
		if tenantID != "" {
			ub.Where("tenant_id = $1", tenantID)
		}
	}
}

// applyTenantScopeDelete adds a tenant_id filter to a delete query builder.
// Note: uses PostgreSQL-style $1 placeholders.
func (ch *CrudHandler) applyTenantScopeDelete(db *query.DeleteBuilder, r *http.Request) {
	if ch.Entity.Config.MultiTenant {
		tenantID := GetTenantID(r.Context())
		if tenantID != "" {
			db.Where("tenant_id = $1", tenantID)
		}
	}
}

// injectTenant injects the tenant_id into a data map when multi-tenancy is enabled.
func (ch *CrudHandler) injectTenant(data map[string]any, r *http.Request) {
	if ch.Entity.Config.MultiTenant {
		tenantID := GetTenantID(r.Context())
		if tenantID != "" {
			data["tenant_id"] = tenantID
		}
	}
}

// applySoftDeleteFilter adds a deleted_at IS NULL filter unless the caller
// requests trashed records via ?trashed=true.
func (ch *CrudHandler) applySoftDeleteFilter(qb *query.QueryBuilder, r *http.Request) {
	if ch.Entity.Config.SoftDelete {
		showTrashed := r.URL.Query().Get("trashed") == "true"
		if !showTrashed {
			qb.Where("deleted_at IS NULL")
		}
	}
}

// applySoftDeleteFilterCount adds a deleted_at IS NULL filter to a count query.
func (ch *CrudHandler) applySoftDeleteFilterCount(cb *query.CountBuilder, r *http.Request) {
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

// visibleFields returns field names that are not Hidden.
func (ch *CrudHandler) visibleFields() []string {
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
		return toCamelCase(col)
	}
}

// convertMapKeys applies the configured JSON casing to all keys in a map.
func (ch *CrudHandler) convertMapKeys(m map[string]any) map[string]any {
	switch ch.JSONCase {
	case CaseSnake:
		return m
	default: // CaseCamel
		return mapToCamelCase(m)
	}
}

// unconvertMapKeys reverses the JSON casing back to DB column names (snake_case).
func (ch *CrudHandler) unconvertMapKeys(m map[string]any) map[string]any {
	switch ch.JSONCase {
	case CaseSnake:
		return m
	default: // CaseCamel
		return mapToSnakeCase(m)
	}
}

// entitySchema returns the schema for validation.
func (ch *CrudHandler) entitySchema() schema.Schema {
	return schema.Schema{Fields: ch.Entity.GetFields()}
}

// List returns an http.HandlerFunc that lists entity records with filtering,
// sorting, and pagination.
func (ch *CrudHandler) List() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		page, perPage := parsePagination(r)

		filters, err := ParseFilters(r, ch.Entity.GetFields())
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid filters: "+err.Error())
			return
		}
		sorts := ParseSort(r, ch.Entity.GetFields())

		// Count total matching rows
		countQb := query.Count(ch.Entity.GetTable())
		applyFiltersToCountQuery(countQb, filters)
		ch.applyTenantScopeCount(countQb, r)
		ch.applySoftDeleteFilterCount(countQb, r)
		countSQL, countArgs := countQb.Build()
		var total int
		if err := ch.DB.QueryRowContext(ctx, countSQL, countArgs...).Scan(&total); err != nil {
			writeJSONError(w, http.StatusInternalServerError, "count query failed: "+err.Error())
			return
		}

		// Build data query — select only visible fields
		cols := ch.visibleFields()
		qb := query.Select(cols...)
		qb.From(ch.Entity.GetTable())
		applyFiltersToQuery(qb, filters)
		ch.applyTenantScope(qb, r)
		ch.applySoftDeleteFilter(qb, r)
		applySortToQuery(qb, sorts)

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
func (ch *CrudHandler) Get() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		id := r.PathValue("id")
		if id == "" {
			writeJSONError(w, http.StatusBadRequest, "missing id")
			return
		}

		cols := ch.visibleFields()
		qb := query.Select(cols...)
		qb.From(ch.Entity.GetTable())
		qb.Where(ch.PrimaryKey+" = $1", id)
		ch.applyTenantScope(qb, r)
		ch.applySoftDeleteFilter(qb, r)

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

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(result)
	}
}

// Create returns an http.HandlerFunc that creates a new entity record.
// Auto-generated fields are populated server-side and excluded from the request body.
func (ch *CrudHandler) Create() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
			return
		}
		body = ch.unconvertMapKeys(body)

		// Inject tenant_id for multi-tenant entities
		ch.injectTenant(body, r)

		// Generate values for all auto-generated fields
		for _, f := range ch.Entity.GetFields() {
			if f.AutoGenerate != schema.AutoNone {
				body[f.Name] = generateFieldValue(f.AutoGenerate)
			}
		}

		// Execute BeforeCreate hooks
		if ch.Hooks != nil {
			if err := ch.Hooks.ExecuteHooks(r.Context(), BeforeCreate, body); err != nil {
				writeJSONError(w, http.StatusBadRequest, err.Error())
				return
			}
		}

		// Validate only writable fields
		vr := schema.ValidateAll(ch.entitySchema(), body)
		if !vr.Valid {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]any{
				"error":   "validation failed",
				"success": false,
				"fields":  vr.Errors,
			})
			return
		}

		// Build INSERT — skip auto-generated, read-only, and hidden fields
		var cols []string
		var vals []any
		for _, f := range ch.Entity.GetFields() {
			if f.AutoGenerate != schema.AutoNone {
				// Already handled above
				val := body[f.Name]
				cols = append(cols, f.Name)
				vals = append(vals, val)
				continue
			}
			if f.ReadOnly || f.Hidden {
				continue // clients should not set these
			}
			val, ok := body[f.Name]
			if !ok {
				if f.Default != nil {
					val = f.Default
				} else {
					continue
				}
			}
			cols = append(cols, f.Name)
			vals = append(vals, val)
		}

		// Ensure tenant_id is included for multi-tenant entities.
		// It may not be in the entity's field list, so we add it explicitly.
		if ch.Entity.Config.MultiTenant {
			if tenantID := GetTenantID(r.Context()); tenantID != "" {
				cols = append(cols, "tenant_id")
				vals = append(vals, tenantID)
			}
		}

		// Return only visible fields
		visFields := ch.visibleFields()
		ib := query.Insert(ch.Entity.GetTable()).
			Columns(cols...).
			Values(vals...).
			Returning(visFields...)

		sqlStr, args := ib.Build()
		row := ch.DB.QueryRowContext(ctx, sqlStr, args...)

		result, err := scanRow(row, visFields, ch.convertKey)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "insert failed: "+err.Error())
			return
		}

		// Execute AfterCreate hooks
		if ch.Hooks != nil {
			ch.Hooks.ExecuteHooks(r.Context(), AfterCreate, result)
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(result)
	}
}

// Update returns an http.HandlerFunc that updates an entity by ID.
func (ch *CrudHandler) Update() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		id := r.PathValue("id")
		if id == "" {
			writeJSONError(w, http.StatusBadRequest, "missing id")
			return
		}

		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
			return
		}
		body = ch.unconvertMapKeys(body)

		// Execute BeforeUpdate hooks
		if ch.Hooks != nil {
			if err := ch.Hooks.ExecuteHooks(r.Context(), BeforeUpdate, body); err != nil {
				writeJSONError(w, http.StatusBadRequest, err.Error())
				return
			}
		}

		vr := schema.ValidateAll(ch.entitySchema(), body)
		if !vr.Valid {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]any{
				"error":   "validation failed",
				"success": false,
				"fields":  vr.Errors,
			})
			return
		}

		ub := query.Update(ch.Entity.GetTable())
		anySet := false
		for _, f := range ch.Entity.GetFields() {
			if f.Name == ch.PrimaryKey || f.AutoGenerate != schema.AutoNone || f.ReadOnly || f.Hidden {
				continue
			}
			val, ok := body[f.Name]
			if !ok {
				continue
			}
			ub.Set(f.Name, val)
			anySet = true
		}

		if !anySet {
			writeJSONError(w, http.StatusBadRequest, "no fields to update")
			return
		}

		ub.Where(ch.PrimaryKey+" = $1", id)
		ch.applyTenantScopeUpdate(ub, r)
		visFields := ch.visibleFields()
		ub.Returning(visFields...)

		sqlStr, args := ub.Build()
		row := ch.DB.QueryRowContext(ctx, sqlStr, args...)

		result, err := scanRow(row, visFields, ch.convertKey)
		if err != nil {
			if err == sql.ErrNoRows {
				writeJSONError(w, http.StatusNotFound, "not found")
				return
			}
			writeJSONError(w, http.StatusInternalServerError, "update failed: "+err.Error())
			return
		}

		// Execute AfterUpdate hooks
		if ch.Hooks != nil {
			ch.Hooks.ExecuteHooks(r.Context(), AfterUpdate, result)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(result)
	}
}

// Delete returns an http.HandlerFunc that deletes an entity by ID.
// If the entity has SoftDelete=true, it sets deleted_at instead.
func (ch *CrudHandler) Delete() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		id := r.PathValue("id")
		if id == "" {
			writeJSONError(w, http.StatusBadRequest, "missing id")
			return
		}

		// Execute BeforeDelete hooks
		if ch.Hooks != nil {
			if err := ch.Hooks.ExecuteHooks(r.Context(), BeforeDelete, id); err != nil {
				writeJSONError(w, http.StatusBadRequest, err.Error())
				return
			}
		}

		if ch.Entity.Config.SoftDelete {
			ub := query.Update(ch.Entity.GetTable()).
				Set("deleted_at", time.Now().UTC()).
				Where(ch.PrimaryKey+" = $1", id)
			ch.applyTenantScopeUpdate(ub, r)
			sqlStr, args := ub.Build()
			res, err := ch.DB.ExecContext(ctx, sqlStr, args...)
			if err != nil {
				writeJSONError(w, http.StatusInternalServerError, "delete failed: "+err.Error())
				return
			}
			affected, _ := res.RowsAffected()
			if affected == 0 {
				writeJSONError(w, http.StatusNotFound, "not found")
				return
			}
		} else {
			db := query.Delete(ch.Entity.GetTable()).
				Where(ch.PrimaryKey+" = $1", id)
			ch.applyTenantScopeDelete(db, r)
			sqlStr, args := db.Build()
			res, err := ch.DB.ExecContext(ctx, sqlStr, args...)
			if err != nil {
				writeJSONError(w, http.StatusInternalServerError, "delete failed: "+err.Error())
				return
			}
			affected, _ := res.RowsAffected()
			if affected == 0 {
				writeJSONError(w, http.StatusNotFound, "not found")
				return
			}
		}

		// Execute AfterDelete hooks
		if ch.Hooks != nil {
			ch.Hooks.ExecuteHooks(r.Context(), AfterDelete, id)
		}

		w.WriteHeader(http.StatusNoContent)
	}
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
var _ fmt.Stringer = (*Entity)(nil)
