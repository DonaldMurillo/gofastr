package framework

import (
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
		},
	})

	// Add list response schema
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
		path := "/" + tableName

		// --- GET /{table} — List ---
		listOp := openapi.NewOperation()
		listOp.Summary = "List " + entityName
		listOp.OperationID = "list_" + entityName
		listOp.Tags = []string{entityName}
		listOp.AddParameter("page", "query", "Page number", false, map[string]any{"type": "integer", "default": 1})
		listOp.AddParameter("limit", "query", "Items per page (max 100)", false, map[string]any{"type": "integer", "default": 20})
		listOp.AddParameter("sort", "query", "Sort field", false, map[string]any{"type": "string"})

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

		listOp.AddResponse(200, "List of "+entityName, listRef)
		listOp.Responses[400] = map[string]any{
			"description": "Invalid filters",
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
		createOp.AddResponse(201, "Created "+entityName, entityRef)
		createOp.Responses[400] = map[string]any{
			"description": "Validation error",
		}
		s.AddPath("POST", path, *createOp)

		// --- GET /{table}/:id — Get by ID ---
		getOp := openapi.NewOperation()
		getOp.Summary = "Get " + entityName + " by ID"
		getOp.OperationID = "get_" + entityName
		getOp.Tags = []string{entityName}
		getOp.AddResponse(200, "Single "+entityName, entityRef)
		getOp.Responses[404] = map[string]any{
			"description": entityName + " not found",
		}
		s.AddPath("GET", path+"/:id", *getOp)

		// --- PUT /{table}/:id — Update ---
		updateOp := openapi.NewOperation()
		updateOp.Summary = "Update " + entityName
		updateOp.OperationID = "update_" + entityName
		updateOp.Tags = []string{entityName}
		updateOp.SetRequestBody("application/json", excludeFieldsByBehavior(entitySchema, fields), false)
		updateOp.AddResponse(200, "Updated "+entityName, entityRef)
		updateOp.Responses[400] = map[string]any{"description": "Validation error"}
		updateOp.Responses[404] = map[string]any{"description": entityName + " not found"}
		s.AddPath("PUT", path+"/:id", *updateOp)

		// --- DELETE /{table}/:id — Delete ---
		deleteOp := openapi.NewOperation()
		deleteOp.Summary = "Delete " + entityName
		deleteOp.OperationID = "delete_" + entityName
		deleteOp.Tags = []string{entityName}
		deleteOp.Responses = map[int]map[string]any{
			204: {"description": "Deleted"},
			404: {"description": entityName + " not found"},
		}
		s.AddPath("DELETE", path+"/:id", *deleteOp)

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
