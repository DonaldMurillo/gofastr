package openapi

import (
	"strings"
)

// Spec is a builder for an OpenAPI 3.1 specification.
type Spec struct {
	info     map[string]any
	servers  []map[string]any
	paths    map[string]map[string]any
	schemas  map[string]map[string]any
	tags     []map[string]any
	security []map[string][]string
	schemes  map[string]map[string]any
}

// NewSpec creates a new Spec with the given title and version.
func NewSpec(title, version string) *Spec {
	return &Spec{
		info: map[string]any{
			"title":   title,
			"version": version,
		},
		paths:   make(map[string]map[string]any),
		schemas: make(map[string]map[string]any),
		schemes: make(map[string]map[string]any),
	}
}

// AddServer registers a server URL with an optional description.
func (s *Spec) AddServer(url, description string) {
	entry := map[string]any{"url": url}
	if description != "" {
		entry["description"] = description
	}
	s.servers = append(s.servers, entry)
}

// AddPath registers a path + method with an Operation.
// Path parameters in GoFastr format (/users/:id) are automatically converted
// to OpenAPI format (/users/{id}) and path parameters are added to the
// operation.
func (s *Spec) AddPath(method, path string, op Operation) {
	openapiPath := toOpenAPIPath(path)
	pathParams := extractPathParams(path)

	// Add path params to the operation if not already present.
	existing := paramNames(op.Parameters)
	for _, p := range pathParams {
		if _, found := existing[p]; !found {
			op.Parameters = append(op.Parameters, map[string]any{
				"name":     p,
				"in":       "path",
				"required": true,
				"schema":   map[string]any{"type": "string"},
			})
		}
	}

	method = strings.ToLower(method)
	if s.paths[openapiPath] == nil {
		s.paths[openapiPath] = make(map[string]any)
	}
	s.paths[openapiPath][method] = op.ToMap()
}

// AddSchema registers a reusable schema component.
func (s *Spec) AddSchema(name string, schema map[string]any) {
	s.schemas[name] = schema
}

// AddTag registers a tag with a description.
func (s *Spec) AddTag(name, description string) {
	tag := map[string]any{"name": name}
	if description != "" {
		tag["description"] = description
	}
	s.tags = append(s.tags, tag)
}

// SetSecurityScheme registers a security scheme (e.g. bearerAuth).
func (s *Spec) SetSecurityScheme(name string, scheme map[string]any) {
	s.schemes[name] = scheme
}

// AddSecurityRequirement adds a global security requirement.
func (s *Spec) AddSecurityRequirement(name string, scopes []string) {
	s.security = append(s.security, map[string][]string{
		name: scopes,
	})
}

// Build produces the full OpenAPI 3.1 specification as a map.
func (s *Spec) Build() map[string]any {
	doc := map[string]any{
		"openapi": "3.1.0",
		"info":    s.info,
	}

	if len(s.servers) > 0 {
		doc["servers"] = s.servers
	}

	doc["paths"] = s.paths

	components := map[string]any{}
	if len(s.schemas) > 0 {
		components["schemas"] = s.schemas
	}
	if len(s.schemes) > 0 {
		components["securitySchemes"] = s.schemes
	}
	if len(components) > 0 {
		doc["components"] = components
	}

	if len(s.tags) > 0 {
		doc["tags"] = s.tags
	}

	if len(s.security) > 0 {
		doc["security"] = s.security
	}

	return doc
}

// toOpenAPIPath converts GoFastr path params (:name) to OpenAPI style ({name}).
func toOpenAPIPath(path string) string {
	parts := strings.Split(path, "/")
	for i, part := range parts {
		if strings.HasPrefix(part, ":") {
			parts[i] = "{" + part[1:] + "}"
		}
	}
	return strings.Join(parts, "/")
}

// extractPathParams returns the parameter names from GoFastr-style path
// segments (e.g. ":id" → "id").
func extractPathParams(path string) []string {
	var params []string
	for _, part := range strings.Split(path, "/") {
		if strings.HasPrefix(part, ":") {
			params = append(params, part[1:])
		}
	}
	return params
}

// paramNames returns a set of parameter names already present.
func paramNames(params []map[string]any) map[string]struct{} {
	names := make(map[string]struct{}, len(params))
	for _, p := range params {
		if name, ok := p["name"].(string); ok {
			names[name] = struct{}{}
		}
	}
	return names
}
