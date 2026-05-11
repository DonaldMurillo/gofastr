package openapi

import (
	"strings"

	"github.com/gofastr/gofastr/framework/crud"
	"github.com/gofastr/gofastr/framework/entity"
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

// DefaultEndpointToolName synthesises an MCP tool name from an entity +
// method + path triple. Used as a fallback when an Endpoint doesn't supply
// an explicit MCPName.
func DefaultEndpointToolName(entityName, method, path string) string {
	cleaned := strings.Trim(path, "/")
	cleaned = strings.NewReplacer("/", "_", "{", "", "}", "", "-", "_").Replace(cleaned)
	return strings.ToLower(entityName + "_" + method + "_" + cleaned)
}
