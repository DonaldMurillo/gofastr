package openapi

import (
	"github.com/DonaldMurillo/gofastr/core/openapi"
)

// addBulkPaths documents the two routes App.EntityUI mounts beside an
// entity's CRUD routes: the bulk bar's POST <path>/_bulk and the list's
// GET <path>/_export.csv. Both refuse a caller who may not read the
// entity with 403, never 401, since the handler asks the read gate rather
// than the session middleware.
func addBulkPaths(s *openapi.Spec, path, entityName, schemaName, tagName string, gated bool, errorRef map[string]any) {
	ids := map[string]any{"oneOf": []any{
		map[string]any{"type": "string"},
		map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
	}}
	bulkOp := openapi.NewOperation()
	bulkOp.Summary = "Run a bulk action on " + entityName
	bulkOp.Description = "The list's bulk bar posts here, and so does a record's action button with scope record and one id. The server re-reads the selection under the caller's scope and asks each record's write gate before writing it; a refused record counts as skipped. Over the in-request cap the run is queued (202) when the app has a job runner, else refused (422). A record action answers 200 when it ran, 403 when the record's gates skipped it and 500 when it failed."
	bulkOp.OperationID = "bulk_" + schemaName
	bulkOp.Tags = []string{tagName}
	bulkOp.SetRequestBody("application/json", map[string]any{
		"type": "object",
		"properties": map[string]any{
			"action": map[string]any{"type": "string", "description": "An action key the list's bulk bar offers this caller, or for scope record a run:<key> app action"},
			"scope":  map[string]any{"type": "string", "enum": []string{"selected", "page", "every", "record"}},
			"ids":    ids,
			"page":   ids,
			"key":    map[string]any{"type": "string", "description": "The list's key, which namespaces its query parameters"},
			"query":  map[string]any{"type": "string", "description": "The list's query string, for scope every"},
			"match":  map[string]any{"type": "string", "description": "For scope every: the digest of the ids the list offered, from the bar's match field. The run is refused (409) when the query now matches any other set"},
		},
		"required": []string{"action", "scope"},
		"if":       map[string]any{"properties": map[string]any{"scope": map[string]any{"const": "every"}}, "required": []string{"scope"}},
		"then":     map[string]any{"required": []string{"match"}},
	}, true)
	bulkOp.AddResponse(200, "Ran in the request", objectSchemaWith(map[string]any{
		"run":     map[string]any{"type": "string"},
		"done":    map[string]any{"type": "integer"},
		"skipped": map[string]any{"type": "integer"},
		"failed":  map[string]any{"type": "integer"},
	}))
	bulkOp.AddResponse(202, "Queued", objectSchemaWith(map[string]any{
		"job":   map[string]any{"type": "string"},
		"count": map[string]any{"type": "integer"},
	}))
	bulkOp.AddResponse(400, "Invalid request body", errorRef)
	bulkOp.AddResponse(403, "Forbidden, or an action not offered to this caller", errorRef)
	bulkOp.AddResponse(404, entityName+" has no bulk actions (a list scope with bulk off)", errorRef)
	bulkOp.AddResponse(409, "Every match no longer matches the rows the list offered", errorRef)
	bulkOp.AddResponse(413, "Request body too large", errorRef)
	bulkOp.AddResponse(415, "A body that is not JSON", errorRef)
	bulkOp.AddResponse(422, "Nothing selected, a scope or filter the list refuses, a record scope without exactly one readable id, or over a cap", errorRef)
	bulkOp.AddResponse(500, "A record action that failed", errorRef)
	exportOp := openapi.NewOperation()
	exportOp.Summary = "Export " + entityName + " as CSV"
	exportOp.Description = "Every row the list's query matches, up to the every-match cap. The list's own query parameters narrow it; _list names the list's key."
	exportOp.OperationID = "export_" + schemaName
	exportOp.Tags = []string{tagName}
	exportOp.AddParameter("_list", "query", "The list's key, which namespaces its query parameters", false, map[string]any{"type": "string"})
	exportOp.Responses[200] = map[string]any{
		"description": "CSV, one header row then one row per record",
		"content":     map[string]any{"text/csv": map[string]any{"schema": map[string]any{"type": "string"}}},
	}
	exportOp.AddResponse(422, "A list key or filter the list refuses, or more matches than the cap", errorRef)
	exportOp.AddResponse(403, "Forbidden", errorRef)
	exportOp.AddResponse(404, entityName+" has no export", errorRef)
	for _, op := range []*openapi.Operation{bulkOp, exportOp} {
		if gated {
			op.AddSecurity("bearerAuth", nil)
			op.AddSecurity("cookieAuth", nil)
		}
	}
	s.AddPath("POST", path+"/_bulk", *bulkOp)
	s.AddPath("GET", path+"/_export.csv", *exportOp)
}
