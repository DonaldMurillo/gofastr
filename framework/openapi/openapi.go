package openapi

import (
	"strings"

	"github.com/DonaldMurillo/gofastr/core/openapi"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/crud"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/internal/casing"
)

// EntityOpenAPI generates a full OpenAPI Spec from all registered entities.
// It produces:
//   - Schema components for each entity with typed fields
//   - CRUD paths (GET, POST, PUT, PATCH, DELETE) with request/response schemas
//   - List endpoint with pagination parameters
//   - Proper error response schemas
//
// EntityOpenAPI builds the spec for every registered entity. An optional
// basePath (e.g. "/api", from AppConfig.APIPrefix) is expressed as the server
// URL so the documented paths match where the routes actually mount — the
// per-path keys stay relative (e.g. "/posts"), and clients prepend the server.
func EntityOpenAPI(registry entity.Registry, title, version string, basePath ...string) *openapi.Spec {
	s := openapi.NewSpec(title, version)
	server := "/"
	if len(basePath) > 0 && basePath[0] != "" && basePath[0] != "/" {
		server = basePath[0]
	}
	s.AddServer(server, "current")

	// Add common error response schema
	s.AddSchema("Error", map[string]any{
		"type": "object",
		"properties": map[string]any{
			"error":   map[string]any{"type": "string"},
			"success": map[string]any{"type": "boolean"},
			"code":    map[string]any{"type": "integer"},
			"fields":  map[string]any{"type": "object", "additionalProperties": map[string]any{"type": "array", "items": map[string]any{"type": "string"}}},
		},
	})

	// Offset-mode list envelope
	s.AddSchema("ListResponse", map[string]any{
		"type": "object",
		"properties": map[string]any{
			"data":       map[string]any{"type": "array", "items": map[string]any{"type": "object"}},
			"total":      map[string]any{"type": "integer"},
			"page":       map[string]any{"type": "integer"},
			"perPage":    map[string]any{"type": "integer"},
			"totalPages": map[string]any{"type": "integer"},
		},
	})

	// Cursor-mode list envelope (returned when ?cursor= is present)
	s.AddSchema("CursorPage", map[string]any{
		"type": "object",
		"properties": map[string]any{
			"data":    map[string]any{"type": "array", "items": map[string]any{"type": "object"}},
			"cursor":  map[string]any{"type": "string", "description": "Opaque cursor for the next page; empty when there are no more results."},
			"hasMore": map[string]any{"type": "boolean"},
			"total":   map[string]any{"type": "integer"},
		},
	})

	// Per-item shape inside a _batch response
	s.AddSchema("BatchResult", map[string]any{
		"type": "object",
		"properties": map[string]any{
			"index":   map[string]any{"type": "integer"},
			"data":    map[string]any{"type": "object"},
			"error":   map[string]any{"type": "string"},
			"fields":  map[string]any{"type": "object", "additionalProperties": map[string]any{"type": "array", "items": map[string]any{"type": "string"}}},
			"skipped": map[string]any{"type": "boolean"},
		},
	})

	// Top-level shape for every _batch response
	s.AddSchema("BatchResponse", map[string]any{
		"type": "object",
		"properties": map[string]any{
			"committed": map[string]any{"type": "boolean"},
			"results":   map[string]any{"type": "array", "items": map[string]any{"$ref": "#/components/schemas/BatchResult"}},
		},
	})

	// Track whether any entity is auth-gated so the shared security
	// schemes are registered once after the loop, not per entity.
	anyGated := false
	// Generate schema + paths for each entity. Use AllSorted so the
	// emitted /openapi.json bytes are stable across restarts —
	// otherwise the tag array order tracks Go's randomised map
	// iteration, breaking ETag caching and golden-file diffs.
	for _, ent := range registry.AllSorted() {
		entityName := ent.GetName()
		tableName := ent.GetTable()
		fields := ent.GetFields()

		// Generate entity schema (excluding hidden fields from response)
		visibleFields := make([]schema.Field, 0, len(fields))
		for _, f := range fields {
			if !f.Hidden {
				visibleFields = append(visibleFields, f)
			}
		}
		entitySchema := openapi.FieldsToSchema(visibleFields)
		// Convert snake_case property names to camelCase
		if props, ok := entitySchema["properties"].(map[string]any); ok {
			camelProps := make(map[string]any, len(props))
			for k, v := range props {
				camelProps[casing.ToCamel(k)] = v
			}
			entitySchema["properties"] = camelProps
		}
		if reqs, ok := entitySchema["required"].([]string); ok {
			for i, r := range reqs {
				reqs[i] = casing.ToCamel(r)
			}
		}
		s.AddSchema(entityName, entitySchema)

		// Tag for grouping
		s.AddTag(entityName, entityName+" operations")

		// Reference to entity schema
		entityRef := map[string]any{"$ref": "#/components/schemas/" + entityName}
		singleRef := map[string]any{
			"type":     "object",
			"required": []string{"data"},
			"properties": map[string]any{
				"data": entityRef,
			},
		}
		listRef := map[string]any{"$ref": "#/components/schemas/ListResponse"}
		cursorRef := map[string]any{"$ref": "#/components/schemas/CursorPage"}
		errorRef := map[string]any{"$ref": "#/components/schemas/Error"}
		batchRespRef := map[string]any{"$ref": "#/components/schemas/BatchResponse"}
		path := "/" + tableName

		// Auto-CRUD is secure-by-default (issue #65): an entity requires
		// an authenticated session for every operation unless it opts
		// out via Public, or an explicit mechanism already governs it
		// (owner-scoped, tenant-scoped, or an RBAC AccessControl rule —
		// which additionally rejects authenticated-but-unpermitted
		// callers with 403). The spec must advertise 401/403 on every
		// entity except a Public one, so generated SDKs and agents don't
		// assume a plain entity (no owner_field/access declared) is
		// reachable anonymously — it isn't, unless Public: true says so.
		rbacGated := ent.Config.Access.Read != "" ||
			ent.Config.Access.Create != "" ||
			ent.Config.Access.Update != "" ||
			ent.Config.Access.Delete != ""
		gated := !ent.Config.Public || ent.Config.OwnerField != "" || ent.Config.MultiTenant || rbacGated
		if gated {
			anyGated = true
		}

		includeNames := make([]string, 0, len(ent.Config.Relations))
		for _, rel := range ent.Config.Relations {
			includeNames = append(includeNames, rel.Name)
		}
		includeDesc := "Comma-separated list of relations to eager-load."
		includeSchema := map[string]any{"type": "string"}
		if len(includeNames) > 0 {
			includeDesc += " Available: " + strings.Join(includeNames, ", ") + "."
		}

		// ?fields= projects the response down to the named columns (plus the
		// primary key, always included). Advertise the visible field names so
		// SDK generators / agents can discover the projection surface.
		fieldNames := make([]string, 0, len(visibleFields))
		for _, f := range visibleFields {
			fieldNames = append(fieldNames, casing.ToCamel(f.Name))
		}
		fieldsDesc := "Comma-separated list of fields to return (projection); the primary key is always included."
		if len(fieldNames) > 0 {
			fieldsDesc += " Available: " + strings.Join(fieldNames, ", ") + "."
		}
		fieldsSchema := map[string]any{"type": "string"}

		// --- GET /{table} — List ---
		listOp := openapi.NewOperation()
		listOp.Summary = "List " + entityName
		listOp.OperationID = "list_" + entityName
		listOp.Tags = []string{entityName}
		listOp.AddParameter("page", "query", "Page number (offset mode)", false, map[string]any{"type": "integer", "default": 1})
		listOp.AddParameter("limit", "query", "Items per page (max 100)", false, map[string]any{"type": "integer", "default": 20})
		listOp.AddParameter("sort", "query", "Sort field (offset mode only; ignored when ?cursor is present)", false, map[string]any{"type": "string"})
		listOp.AddParameter("cursor", "query", "Opaque cursor; presence (even empty) switches the response to CursorPage shape and uses keyset pagination by primary key.", false, map[string]any{"type": "string"})
		listOp.AddParameter("direction", "query", "Cursor walk direction: forward (default) or backward.", false, map[string]any{"type": "string", "enum": []string{"forward", "backward"}, "default": "forward"})
		listOp.AddParameter("include", "query", includeDesc, false, includeSchema)
		listOp.AddParameter("fields", "query", fieldsDesc, false, fieldsSchema)
		// ?trashed=true retrieves soft-deleted rows (authenticated callers
		// only). Only meaningful when the entity opts into soft-delete.
		if ent.Config.SoftDelete {
			listOp.AddParameter("trashed", "query", "Include soft-deleted rows (requires authentication).", false, map[string]any{"type": "boolean", "default": false})
		}

		// Add filter parameters matching the actual filter parser
		// which accepts <field>, <field>_gt, <field>_gte, <field>_lt,
		// <field>_lte, <field>_like, <field>_in.
		// Filter parameters use raw field names (e.g. "created_at_gt")
		// because ParseFilters matches against the schema field names directly.
		for _, f := range visibleFields {
			name := f.Name
			filterSchema := fieldToFilterSchema(f)
			// Exact match and _in apply to every field type.
			listOp.AddParameter(name, "query", "Exact match on "+name, false, filterSchema)
			// Range operators only make sense for ordered/comparable
			// types (numbers, timestamps, dates). Advertising _gt/_lt on
			// a boolean or JSON blob misleads SDK generators into
			// proposing comparisons the field can't satisfy.
			if fieldSupportsRange(f.Type) {
				listOp.AddParameter(name+"_gt", "query", name+" greater than", false, filterSchema)
				listOp.AddParameter(name+"_gte", "query", name+" greater than or equal", false, filterSchema)
				listOp.AddParameter(name+"_lt", "query", name+" less than", false, filterSchema)
				listOp.AddParameter(name+"_lte", "query", name+" less than or equal", false, filterSchema)
			}
			// _like is a substring match — only meaningful for text-ish
			// fields, not booleans or JSON.
			if fieldSupportsLike(f.Type) {
				listOp.AddParameter(name+"_like", "query", name+" contains (LIKE)", false, filterSchema)
			}
			listOp.AddParameter(name+"_in", "query", name+" in comma-separated list", false, map[string]any{"type": "string"})
		}

		// ?q= free-text search: advertised only when the entity declares
		// SearchFields. The description names the searched columns so SDK
		// generators and API consumers know the scope.
		if len(ent.Config.SearchFields) > 0 {
			desc := "Free-text search across: " + strings.Join(ent.Config.SearchFields, ", ")
			listOp.AddParameter("q", "query", desc, false, map[string]any{"type": "string"})
		}

		// 200 is one of two envelopes — clients pick by whether they sent ?cursor.
		listOp.AddResponse(200, "List of "+entityName, map[string]any{"oneOf": []any{listRef, cursorRef}})
		listOp.AddResponse(400, "Invalid filters or unknown include", errorRef)
		if gated {
			listOp.AddResponse(401, "Authentication required", errorRef)
			listOp.AddResponse(403, "Forbidden", errorRef)
			listOp.AddSecurity("bearerAuth", nil)
			listOp.AddSecurity("cookieAuth", nil)
		}
		s.AddPath("GET", path, *listOp)

		// --- POST /{table} — Create ---
		createOp := openapi.NewOperation()
		createOp.Summary = "Create " + entityName
		createOp.OperationID = "create_" + entityName
		createOp.Tags = []string{entityName}

		// Create request body excludes auto-generated and read-only fields
		createSchema := excludeFieldsByBehavior(entitySchema, fields)
		createOp.SetRequestBody("application/json", createSchema, true)
		createOp.AddResponse(201, "Created "+entityName, singleRef)
		createOp.AddResponse(400, "Validation error", errorRef)
		if gated {
			createOp.AddResponse(401, "Authentication required", errorRef)
			createOp.AddResponse(403, "Forbidden", errorRef)
			createOp.AddSecurity("bearerAuth", nil)
			createOp.AddSecurity("cookieAuth", nil)
		}
		s.AddPath("POST", path, *createOp)

		// --- GET /{table}/:id — Get by ID ---
		getOp := openapi.NewOperation()
		getOp.Summary = "Get " + entityName + " by ID"
		getOp.OperationID = "get_" + entityName
		getOp.Tags = []string{entityName}
		getOp.AddParameter("include", "query", includeDesc, false, includeSchema)
		getOp.AddParameter("fields", "query", fieldsDesc, false, fieldsSchema)
		getOp.AddResponse(200, "Single "+entityName, singleRef)
		getOp.AddResponse(400, "Unknown include", errorRef)
		if gated {
			getOp.AddResponse(401, "Authentication required", errorRef)
			getOp.AddResponse(403, "Forbidden", errorRef)
			getOp.AddSecurity("bearerAuth", nil)
			getOp.AddSecurity("cookieAuth", nil)
		}
		getOp.AddResponse(404, entityName+" not found", errorRef)
		s.AddPath("GET", path+"/:id", *getOp)

		// --- PUT /{table}/:id — Update ---
		updateOp := openapi.NewOperation()
		updateOp.Summary = "Update " + entityName
		updateOp.OperationID = "update_" + entityName
		updateOp.Tags = []string{entityName}
		updateOp.SetRequestBody("application/json", excludeFieldsByBehavior(entitySchema, fields), false)
		updateOp.AddResponse(200, "Updated "+entityName, singleRef)
		updateOp.AddResponse(400, "Validation error", errorRef)
		if gated {
			updateOp.AddResponse(401, "Authentication required", errorRef)
			updateOp.AddResponse(403, "Forbidden", errorRef)
			updateOp.AddSecurity("bearerAuth", nil)
			updateOp.AddSecurity("cookieAuth", nil)
		}
		updateOp.AddResponse(404, entityName+" not found", errorRef)
		s.AddPath("PUT", path+"/:id", *updateOp)

		// --- PATCH /{table}/:id — Sparse update ---
		patchOp := openapi.NewOperation()
		patchOp.Summary = "Patch " + entityName
		patchOp.OperationID = "patch_" + entityName
		patchOp.Tags = []string{entityName}
		patchSchema := excludeFieldsByBehavior(entitySchema, fields)
		delete(patchSchema, "required")
		patchOp.SetRequestBody("application/json", patchSchema, true)
		patchOp.AddResponse(200, "Patched "+entityName, singleRef)
		patchOp.AddResponse(400, "Validation error", errorRef)
		if gated {
			patchOp.AddResponse(401, "Authentication required", errorRef)
			patchOp.AddResponse(403, "Forbidden", errorRef)
			patchOp.AddSecurity("bearerAuth", nil)
			patchOp.AddSecurity("cookieAuth", nil)
		}
		patchOp.AddResponse(404, entityName+" not found", errorRef)
		s.AddPath("PATCH", path+"/:id", *patchOp)

		// --- DELETE /{table}/:id — Delete ---
		deleteOp := openapi.NewOperation()
		deleteOp.Summary = "Delete " + entityName
		deleteOp.OperationID = "delete_" + entityName
		deleteOp.Tags = []string{entityName}
		deleteOp.Responses = map[int]map[string]any{
			204: {"description": "Deleted"},
		}
		if gated {
			deleteOp.AddResponse(401, "Authentication required", errorRef)
			deleteOp.AddResponse(403, "Forbidden", errorRef)
			deleteOp.AddSecurity("bearerAuth", nil)
			deleteOp.AddSecurity("cookieAuth", nil)
		}
		deleteOp.AddResponse(404, entityName+" not found", errorRef)
		s.AddPath("DELETE", path+"/:id", *deleteOp)

		// --- POST /{table}/_batch — BatchCreate ---
		batchCreateBody := map[string]any{
			"type": "object",
			"properties": map[string]any{
				"items": map[string]any{
					"type":     "array",
					"maxItems": crud.MaxBatchSize,
					"items":    createSchema,
				},
			},
			"required": []string{"items"},
		}
		batchCreateOp := openapi.NewOperation()
		batchCreateOp.Summary = "Batch create " + entityName + " (atomic)"
		batchCreateOp.OperationID = "batch_create_" + entityName
		batchCreateOp.Tags = []string{entityName}
		batchCreateOp.SetRequestBody("application/json", batchCreateBody, true)
		batchCreateOp.AddResponse(200, "All items committed", batchRespRef)
		batchCreateOp.AddResponse(400, "Batch rolled back; see results[]", batchRespRef)
		if gated {
			batchCreateOp.AddResponse(401, "Authentication required", errorRef)
			batchCreateOp.AddResponse(403, "Forbidden", errorRef)
			batchCreateOp.AddSecurity("bearerAuth", nil)
			batchCreateOp.AddSecurity("cookieAuth", nil)
		}
		s.AddPath("POST", path+"/_batch", *batchCreateOp)

		// --- PATCH /{table}/_batch — BatchUpdate ---
		batchUpdateItem := map[string]any{
			"allOf": []any{
				excludeFieldsByBehavior(entitySchema, fields),
				map[string]any{"type": "object", "properties": map[string]any{"id": map[string]any{"type": "string"}}, "required": []string{"id"}},
			},
		}
		batchUpdateBody := map[string]any{
			"type": "object",
			"properties": map[string]any{
				"items": map[string]any{
					"type":     "array",
					"maxItems": crud.MaxBatchSize,
					"items":    batchUpdateItem,
				},
			},
			"required": []string{"items"},
		}
		batchUpdateOp := openapi.NewOperation()
		batchUpdateOp.Summary = "Batch update " + entityName + " (atomic)"
		batchUpdateOp.OperationID = "batch_update_" + entityName
		batchUpdateOp.Tags = []string{entityName}
		batchUpdateOp.SetRequestBody("application/json", batchUpdateBody, true)
		batchUpdateOp.AddResponse(200, "All items committed", batchRespRef)
		batchUpdateOp.AddResponse(400, "Batch rolled back; see results[]", batchRespRef)
		if gated {
			batchUpdateOp.AddResponse(401, "Authentication required", errorRef)
			batchUpdateOp.AddResponse(403, "Forbidden", errorRef)
			batchUpdateOp.AddSecurity("bearerAuth", nil)
			batchUpdateOp.AddSecurity("cookieAuth", nil)
		}
		s.AddPath("PATCH", path+"/_batch", *batchUpdateOp)

		// --- GET /{table}/_events — SSE entity subscription stream ---
		eventsOp := openapi.NewOperation()
		eventsOp.Summary = "Subscribe to " + entityName + " events (SSE)"
		eventsOp.OperationID = "events_" + entityName
		eventsOp.Tags = []string{entityName}
		eventsOp.Responses[200] = map[string]any{
			"description": "Server-Sent Events stream of entity.created/updated/deleted",
			"content": map[string]any{
				"text/event-stream": map[string]any{
					"schema": map[string]any{"type": "string"},
				},
			},
		}
		if gated {
			eventsOp.AddResponse(401, "Authentication required", errorRef)
			eventsOp.AddResponse(403, "Forbidden", errorRef)
			eventsOp.AddSecurity("bearerAuth", nil)
			eventsOp.AddSecurity("cookieAuth", nil)
		}
		s.AddPath("GET", path+"/_events", *eventsOp)

		// --- DELETE /{table}/_batch — BatchDelete ---
		batchDeleteBody := map[string]any{
			"type": "object",
			"properties": map[string]any{
				"ids": map[string]any{
					"type":     "array",
					"maxItems": crud.MaxBatchSize,
					"items":    map[string]any{"type": "string"},
				},
			},
			"required": []string{"ids"},
		}
		batchDeleteOp := openapi.NewOperation()
		batchDeleteOp.Summary = "Batch delete " + entityName + " (atomic)"
		batchDeleteOp.OperationID = "batch_delete_" + entityName
		batchDeleteOp.Tags = []string{entityName}
		batchDeleteOp.SetRequestBody("application/json", batchDeleteBody, true)
		batchDeleteOp.AddResponse(200, "All items committed", batchRespRef)
		batchDeleteOp.AddResponse(400, "Batch rolled back; see results[]", batchRespRef)
		if gated {
			batchDeleteOp.AddResponse(401, "Authentication required", errorRef)
			batchDeleteOp.AddResponse(403, "Forbidden", errorRef)
			batchDeleteOp.AddSecurity("bearerAuth", nil)
			batchDeleteOp.AddSecurity("cookieAuth", nil)
		}
		s.AddPath("DELETE", path+"/_batch", *batchDeleteOp)

		for _, endpoint := range ent.Config.Endpoints {
			if endpoint.Method == "" || endpoint.Path == "" {
				continue
			}
			customOp := openapi.NewOperation()
			customOp.Summary = endpoint.Description
			if customOp.Summary == "" {
				customOp.Summary = endpoint.Method + " " + endpoint.Path
			}
			customOp.OperationID = DefaultEndpointToolName(entityName, endpoint.Method, EntityEndpointPath(ent, endpoint.Path))
			customOp.Tags = []string{entityName}
			// A typed InputSchema becomes the JSON request body — but only for
			// methods that carry one (GET/HEAD never do). Unset schemas fall
			// back to today's shapeless {type:object} response and no body.
			if len(endpoint.InputSchema) > 0 && endpoint.Method != "GET" && endpoint.Method != "HEAD" {
				customOp.SetRequestBody("application/json", EndpointInputSchema(endpoint), true)
			}
			customOp.AddResponse(200, "OK", EndpointOutputSchema(endpoint))
			s.AddPath(endpoint.Method, EntityEndpointPath(ent, endpoint.Path), *customOp)
		}
	}

	// When at least one entity is auth-gated, advertise how callers
	// authenticate so generated SDKs and agents don't treat the gated
	// endpoints as public. Both schemes are accepted per-operation; only
	// the gated operations carry a `security` block, leaving public
	// entities unmarked. This is deliberately NOT a global security
	// requirement (Spec.AddSecurityRequirement) — that would hide the
	// fact that ungated entities are anonymously reachable.
	if anyGated {
		// "__Host-session" is the production default the auth battery
		// sets in battery/auth/manager.go AuthConfig.defaults()
		// (DevMode=false). DevMode flips the name to "session_id";
		// deployments overriding AuthConfig.SessionCookie should
		// replace this scheme via Spec.SetSecurityScheme("cookieAuth", ...)
		// after building the spec.
		s.SetSecurityScheme("bearerAuth", map[string]any{
			"type":         "http",
			"scheme":       "bearer",
			"bearerFormat": "JWT",
		})
		s.SetSecurityScheme("cookieAuth", map[string]any{
			"type":        "apiKey",
			"in":          "cookie",
			"name":        "__Host-session",
			"description": "Session cookie issued by the auth battery. Name shown is the production default (`__Host-session`); DevMode uses `session_id`, and deployments overriding `AuthConfig.SessionCookie` should overwrite this scheme via `Spec.SetSecurityScheme` after building the spec.",
		})
	}
	return s
}

// fieldSupportsRange reports whether _gt/_gte/_lt/_lte filter operators are
// meaningful for a field type. Booleans and JSON blobs have no useful
// ordering, so advertising range comparisons on them only misleads SDK
// generators. Every other scalar/text type keeps its range operators.
func fieldSupportsRange(t schema.FieldType) bool {
	switch t {
	case schema.Bool, schema.JSON:
		return false
	default:
		return true
	}
}

// fieldSupportsLike reports whether the _like (substring) filter operator is
// meaningful for a field type. A LIKE match makes no sense on a boolean or
// an opaque JSON blob; all other types keep it.
func fieldSupportsLike(t schema.FieldType) bool {
	switch t {
	case schema.Bool, schema.JSON:
		return false
	default:
		return true
	}
}

// fieldToFilterSchema returns an OpenAPI query parameter schema for filtering
// on a given field type.
func fieldToFilterSchema(f schema.Field) map[string]any {
	switch f.Type {
	case schema.String, schema.Text:
		return map[string]any{"type": "string"}
	case schema.Int:
		return map[string]any{"type": "integer"}
	case schema.Float:
		return map[string]any{"type": "number"}
	case schema.Bool:
		return map[string]any{"type": "boolean"}
	case schema.Enum:
		return map[string]any{"type": "string"}
	default:
		return map[string]any{"type": "string"}
	}
}

// excludeFieldsByBehavior returns a copy of an OpenAPI schema excluding auto-generated, read-only, and hidden fields.
func excludeFieldsByBehavior(specSchema map[string]any, fields []schema.Field) map[string]any {
	cp := make(map[string]any, len(specSchema))
	for k, v := range specSchema {
		cp[k] = v
	}

	// Collect field names to exclude
	exclude := make(map[string]bool)
	for _, f := range fields {
		if f.AutoGenerate != schema.AutoNone || f.ReadOnly || f.Hidden {
			exclude[casing.ToCamel(f.Name)] = true
		}
	}

	if props, ok := cp["properties"].(map[string]any); ok {
		newProps := make(map[string]any, len(props))
		for k, v := range props {
			if !exclude[k] {
				newProps[k] = v
			}
		}
		cp["properties"] = newProps
	}
	if reqs, ok := cp["required"].([]string); ok {
		filtered := make([]string, 0, len(reqs))
		for _, r := range reqs {
			if !exclude[r] {
				filtered = append(filtered, r)
			}
		}
		if len(filtered) > 0 {
			cp["required"] = filtered
		} else {
			delete(cp, "required")
		}
	}
	return cp
}
