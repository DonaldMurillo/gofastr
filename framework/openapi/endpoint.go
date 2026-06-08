package openapi

import (
	"strings"

	coreoa "github.com/DonaldMurillo/gofastr/core/openapi"
	"github.com/DonaldMurillo/gofastr/framework/crud"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// EntityEndpointPath builds the absolute URL path for a custom Endpoint
// declared on an entity. Relative paths are joined under the entity's table;
// absolute paths pass through. ":id"-style params are converted to "{id}".
func EntityEndpointPath(ent *entity.Entity, path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		path = "/"
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + strings.Trim(ent.GetTable(), "/") + "/" + strings.TrimPrefix(path, "/")
	}
	return crud.NormalizePath(convertColonParams(path))
}

func convertColonParams(path string) string {
	parts := strings.Split(path, "/")
	for i, part := range parts {
		if strings.HasPrefix(part, ":") && len(part) > 1 {
			parts[i] = "{" + strings.TrimPrefix(part, ":") + "}"
		}
	}
	return strings.Join(parts, "/")
}

// objectSchema is the shapeless fallback emitted when an Endpoint declares no
// typed Input/Output schema — identical to the historical default.
func objectSchema() map[string]any { return map[string]any{"type": "object"} }

// EndpointInputSchema returns the JSON-Schema object describing an endpoint's
// request body. When ep.InputSchema is set it is converted via the same
// FieldsToSchema machinery the entity CRUD body uses; otherwise the historical
// {type:object} fallback is returned. This is the single source the OpenAPI
// requestBody and the generated MCP tool input schema both consume.
func EndpointInputSchema(ep entity.Endpoint) map[string]any {
	if len(ep.InputSchema) == 0 {
		return objectSchema()
	}
	return coreoa.FieldsToSchema(ep.InputSchema)
}

// EndpointOutputSchema returns the JSON-Schema object describing an endpoint's
// success (200) response body, falling back to {type:object} when
// ep.OutputSchema is unset.
func EndpointOutputSchema(ep entity.Endpoint) map[string]any {
	if len(ep.OutputSchema) == 0 {
		return objectSchema()
	}
	return coreoa.FieldsToSchema(ep.OutputSchema)
}

// DefaultEndpointToolName synthesises an MCP tool name from an entity +
// method + path triple. Used as a fallback when an Endpoint doesn't supply
// an explicit MCPName.
func DefaultEndpointToolName(entityName, method, path string) string {
	cleaned := strings.Trim(path, "/")
	cleaned = strings.NewReplacer("/", "_", "{", "", "}", "", "-", "_").Replace(cleaned)
	return strings.ToLower(entityName + "_" + method + "_" + cleaned)
}
