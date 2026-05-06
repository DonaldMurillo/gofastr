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
			"data":        map[string]any{"type": "array", "items": map[string]any{"type": "object"}},
			"total":       map[string]any{"type": "integer"},
			"page":        map[string]any{"type": "integer"},
			"per_page":    map[string]any{"type": "integer"},
			"total_pages": map[string]any{"type": "integer"},
		},
	})

	// Generate schema + paths for each entity
	for _, entity := range registry.All() {
		entityName := entity.GetName()
		tableName := entity.GetTable()
		fields := entity.GetFields()

		// Generate entity schema
		entitySchema := openapi.FieldsToSchema(fields)
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

		// Add filter parameters for each field
		for _, f := range fields {
			filterSchema := fieldToFilterSchema(f)
			listOp.AddParameter("filter_"+f.Name, "query", "Filter by "+f.Name, false, filterSchema)
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
		createOp.SetRequestBody("application/json", entitySchema, true)
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
		updateOp.SetRequestBody("application/json", entitySchema, false)
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
