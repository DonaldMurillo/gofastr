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
		// Relative paths resolve under the entity's table path. For a
		// versioned entity (registered via App.GroupEntity) the table path
		// carries the group prefix, so the endpoint must too.
		base := "/" + strings.Trim(ent.GetTable(), "/")
		if ent.Version != "" {
			base = ent.Version + base
		}
		path = base + "/" + strings.TrimPrefix(path, "/")
	}
	return crud.NormalizePath(convertColonParams(path))
}

// EntityEndpointRoutePath is EntityEndpointPath with the app's API prefix
// applied — the path the endpoint is actually mounted at.
//
// A relative Endpoint.Path is documented as resolving against the entity's
// table path. Under WithAPIPrefix that table path is prefixed, so the endpoint
// must be too; without this an app using both ends up with its API split
// across two prefixes (CRUD at /api/licenses, the custom endpoint at
// /licenses/{id}/revoke) and nothing reports it.
//
// An absolute path keeps bypassing the prefix. That is the documented escape
// hatch for mounting outside the entity's namespace.
//
// The OpenAPI spec deliberately keeps using EntityEndpointPath: it carries the
// prefix in the `servers` entry, so its paths are prefix-relative by
// construction.
func EntityEndpointRoutePath(ent *entity.Entity, path, apiPrefix string) string {
	relative := !strings.HasPrefix(strings.TrimSpace(path), "/")
	out := EntityEndpointPath(ent, path)
	if relative && apiPrefix != "" {
		out = strings.TrimSuffix(apiPrefix, "/") + out
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
