package framework

import (
	"strings"

	"github.com/gofastr/gofastr/core/openapi"
	"github.com/gofastr/gofastr/core/schema"
)

// EntityOpenAPI generates a full OpenAPI Spec from all registered entities.
// It produces:
//   - Schema components for each entity with typed fields
//   - CRUD paths (GET, POST, PUT, DELETE) with request/response schemas
//   - List endpoint with pagination parameters
//   - Proper error response schemas
func EntityOpenAPI(registry *Registry, title, version string) *openapi.Spec {
	s := openapi.NewSpec(title, version)
	s.AddServer("/", "current")

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

	// Generate schema + paths for each entity
	for _, entity := range registry.All() {
		entityName := entity.GetName()
		tableName := entity.GetTable()
		fields := entity.GetFields()

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
				camelProps[toCamelCase(k)] = v
			}
			entitySchema["properties"] = camelProps
		}
		if reqs, ok := entitySchema["required"].([]string); ok {
			for i, r := range reqs {
				reqs[i] = toCamelCase(r)
			}
		}
		s.AddSchema(entityName, entitySchema)

		// Tag for grouping
		s.AddTag(entityName, entityName+" operations")

		// Reference to entity schema
		entityRef := map[string]any{"$ref": "#/components/schemas/" + entityName}
		listRef := map[string]any{"$ref": "#/components/schemas/ListResponse"}
		cursorRef := map[string]any{"$ref": "#/components/schemas/CursorPage"}
		errorRef := map[string]any{"$ref": "#/components/schemas/Error"}
		batchRespRef := map[string]any{"$ref": "#/components/schemas/BatchResponse"}
		path := "/" + tableName

		includeNames := make([]string, 0, len(entity.Config.Relations))
		for _, rel := range entity.Config.Relations {
			includeNames = append(includeNames, rel.Name)
		}
		includeDesc := "Comma-separated list of relations to eager-load."
		includeSchema := map[string]any{"type": "string"}
		if len(includeNames) > 0 {
			includeDesc += " Available: " + strings.Join(includeNames, ", ") + "."
		}

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

		// Add filter parameters matching the actual filter parser
		// which accepts <field>, <field>_gt, <field>_gte, <field>_lt,
		// <field>_lte, <field>_like, <field>_in.
		// Filter parameters use raw field names (e.g. "created_at_gt")
		// because ParseFilters matches against the schema field names directly.
		for _, f := range visibleFields {
			name := f.Name
			filterSchema := fieldToFilterSchema(f)
			listOp.AddParameter(name, "query", "Exact match on "+name, false, filterSchema)
			listOp.AddParameter(name+"_gt", "query", name+" greater than", false, filterSchema)
			listOp.AddParameter(name+"_gte", "query", name+" greater than or equal", false, filterSchema)
			listOp.AddParameter(name+"_lt", "query", name+" less than", false, filterSchema)
			listOp.AddParameter(name+"_lte", "query", name+" less than or equal", false, filterSchema)
			listOp.AddParameter(name+"_like", "query", name+" contains (LIKE)", false, filterSchema)
			listOp.AddParameter(name+"_in", "query", name+" in comma-separated list", false, map[string]any{"type": "string"})
		}

		// 200 is one of two envelopes — clients pick by whether they sent ?cursor.
		listOp.AddResponse(200, "List of "+entityName, map[string]any{"oneOf": []any{listRef, cursorRef}})
		listOp.AddResponse(400, "Invalid filters or unknown include", errorRef)
		s.AddPath("GET", path, *listOp)

		// --- POST /{table} — Create ---
		createOp := openapi.NewOperation()
		createOp.Summary = "Create " + entityName
		createOp.OperationID = "create_" + entityName
		createOp.Tags = []string{entityName}

		// Create request body excludes auto-generated and read-only fields
		createSchema := excludeFieldsByBehavior(entitySchema, fields)
		createOp.SetRequestBody("application/json", createSchema, true)
		createOp.AddResponse(201, "Created "+entityName, entityRef)
		createOp.AddResponse(400, "Validation error", errorRef)
		s.AddPath("POST", path, *createOp)

		// --- GET /{table}/:id — Get by ID ---
		getOp := openapi.NewOperation()
		getOp.Summary = "Get " + entityName + " by ID"
		getOp.OperationID = "get_" + entityName
		getOp.Tags = []string{entityName}
		getOp.AddParameter("include", "query", includeDesc, false, includeSchema)
		getOp.AddResponse(200, "Single "+entityName, entityRef)
		getOp.AddResponse(400, "Unknown include", errorRef)
		getOp.AddResponse(404, entityName+" not found", errorRef)
		s.AddPath("GET", path+"/:id", *getOp)

		// --- PUT /{table}/:id — Update ---
		updateOp := openapi.NewOperation()
		updateOp.Summary = "Update " + entityName
		updateOp.OperationID = "update_" + entityName
		updateOp.Tags = []string{entityName}
		updateOp.SetRequestBody("application/json", excludeFieldsByBehavior(entitySchema, fields), false)
		updateOp.AddResponse(200, "Updated "+entityName, entityRef)
		updateOp.AddResponse(400, "Validation error", errorRef)
		updateOp.AddResponse(404, entityName+" not found", errorRef)
		s.AddPath("PUT", path+"/:id", *updateOp)

		// --- DELETE /{table}/:id — Delete ---
		deleteOp := openapi.NewOperation()
		deleteOp.Summary = "Delete " + entityName
		deleteOp.OperationID = "delete_" + entityName
		deleteOp.Tags = []string{entityName}
		deleteOp.Responses = map[int]map[string]any{
			204: {"description": "Deleted"},
		}
		deleteOp.AddResponse(404, entityName+" not found", errorRef)
		s.AddPath("DELETE", path+"/:id", *deleteOp)

		// --- POST /{table}/_batch — BatchCreate ---
		batchCreateBody := map[string]any{
			"type": "object",
			"properties": map[string]any{
				"items": map[string]any{
					"type":     "array",
					"maxItems": MaxBatchSize,
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
					"maxItems": MaxBatchSize,
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
		s.AddPath("GET", path+"/_events", *eventsOp)

		// --- DELETE /{table}/_batch — BatchDelete ---
		batchDeleteBody := map[string]any{
			"type": "object",
			"properties": map[string]any{
				"ids": map[string]any{
					"type":     "array",
					"maxItems": MaxBatchSize,
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
		s.AddPath("DELETE", path+"/_batch", *batchDeleteOp)

		for _, endpoint := range entity.Config.Endpoints {
			if endpoint.Method == "" || endpoint.Path == "" {
				continue
			}
			customOp := openapi.NewOperation()
			customOp.Summary = endpoint.Description
			if customOp.Summary == "" {
				customOp.Summary = endpoint.Method + " " + endpoint.Path
			}
			customOp.OperationID = defaultEndpointToolName(entityName, endpoint.Method, entityEndpointPath(entity, endpoint.Path))
			customOp.Tags = []string{entityName}
			customOp.AddResponse(200, "OK", map[string]any{"type": "object"})
			s.AddPath(endpoint.Method, entityEndpointPath(entity, endpoint.Path), *customOp)
		}
	}

	return s
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
			exclude[toCamelCase(f.Name)] = true
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
