package openapi

import (
	"strings"

	coreoa "github.com/DonaldMurillo/gofastr/core/openapi"
	"github.com/DonaldMurillo/gofastr/framework/crud"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// EntityEndpointMethod returns a custom endpoint's trimmed, uppercase HTTP method.
func EntityEndpointMethod(method string) string {
	return strings.ToUpper(strings.TrimSpace(method))
}

// EntityEndpointPath builds the absolute path of an entity's custom
// Endpoint: a relative path joins the entity's table path, ":id" becomes
// "{id}" and dot segments are dropped.
func EntityEndpointPath(ent *entity.Entity, path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		path = "/"
	}
	if !strings.HasPrefix(path, "/") {
		base := "/" + strings.Trim(ent.GetTable(), "/")
		if ent.Version != "" {
			base = ent.Version + base
		}
		path = base + "/" + strings.TrimPrefix(path, "/")
	}
	path = dropDotSegments(path)
	return crud.NormalizePath(convertColonParams(path))
}

// EntityEndpointRoutePath is EntityEndpointPath with apiPrefix applied to
// a relative path of an unversioned entity: the path it is mounted at.
func EntityEndpointRoutePath(ent *entity.Entity, path, apiPrefix string) string {
	relative := !strings.HasPrefix(strings.TrimSpace(path), "/")
	out := EntityEndpointPath(ent, path)
	if relative && apiPrefix != "" && ent.Version == "" {
		out = strings.TrimSuffix(apiPrefix, "/") + out
	}
	return out
}

func dropDotSegments(p string) string {
	parts := strings.Split(p, "/")
	kept := parts[:0]
	for _, seg := range parts {
		if seg == "." || seg == ".." {
			continue
		}
		kept = append(kept, seg)
	}
	out := strings.Join(kept, "/")
	if out == "" {
		return "/"
	}
	return out
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

func objectSchema() map[string]any { return map[string]any{"type": "object"} }

// EndpointInputSchema returns the JSON Schema of an endpoint's request,
// {type:object} when ep.InputSchema is unset.
func EndpointInputSchema(ep entity.Endpoint) map[string]any {
	if len(ep.InputSchema) == 0 {
		return objectSchema()
	}
	return coreoa.FieldsToSchema(ep.InputSchema)
}

// EndpointOutputSchema returns the JSON Schema of an endpoint's 200 body,
// {type:object} when ep.OutputSchema is unset.
func EndpointOutputSchema(ep entity.Endpoint) map[string]any {
	if len(ep.OutputSchema) == 0 {
		return objectSchema()
	}
	return coreoa.FieldsToSchema(ep.OutputSchema)
}

// DefaultEndpointToolName is the MCP tool name for an endpoint with no
// MCPName: entity, lowered method and path, distinct per route.
func DefaultEndpointToolName(entityName, method, path string) string {
	cleaned := strings.Trim(path, "/")
	cleaned = strings.NewReplacer("/", "_", "{", "-", "}", "-").Replace(cleaned)
	return entityName + "_" + strings.ToLower(method) + "_" + cleaned
}
