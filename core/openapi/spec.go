package openapi

import (
	"fmt"
	"net/http"
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

	// RequestView, when set, returns the spec the gated handlers serve to
	// one request; PublicHandler ignores it.
	RequestView func(*http.Request) *Spec
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

// SetDescription sets info.description; an empty string removes it.
func (s *Spec) SetDescription(description string) {
	if description == "" {
		delete(s.info, "description")
		return
	}
	s.info["description"] = description
}

func (s *Spec) viewFor(r *http.Request) *Spec {
	if s.RequestView == nil {
		return s
	}
	if v := s.RequestView(r); v != nil {
		return v.withInheritedInfo(s)
	}
	return s
}

func (s *Spec) withInheritedInfo(base *Spec) *Spec {
	if s == base || s.info == nil {
		return s
	}
	c := *s
	c.info = make(map[string]any, len(s.info)+len(base.info))
	for k, v := range base.info {
		c.info[k] = v
	}
	for k, v := range s.info {
		c.info[k] = v
	}
	return &c
}

// AddServer registers a server URL, dropping one that is neither relative
// nor http, https, ws or wss.
func (s *Spec) AddServer(url, description string) {
	if !isSafeServerURL(url) {
		return
	}
	entry := map[string]any{"url": url}
	if description != "" {
		entry["description"] = description
	}
	s.servers = append(s.servers, entry)
}

func isSafeServerURL(u string) bool {
	if u == "" {
		return false
	}
	if strings.HasPrefix(u, "/") || strings.HasPrefix(u, "./") || strings.HasPrefix(u, "../") {
		return true
	}
	for i := 0; i < len(u); i++ {
		c := u[i]
		if c == ':' {
			scheme := strings.ToLower(u[:i])
			switch scheme {
			case "http", "https", "ws", "wss":
				return true
			default:
				return false
			}
		}
		if c == '/' || c == '?' || c == '#' {
			return true
		}
	}
	return true
}

// AddPath registers op at method and path, writing :id segments as {id}
// and declaring each path parameter op lacks. It panics when the method
// and path are already registered.
func (s *Spec) AddPath(method, path string, op Operation) {
	openapiPath := toOpenAPIPath(path)
	pathParams := extractPathParams(path)

	existing := pathParamNames(op.Parameters)
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
	if _, exists := s.paths[openapiPath][method]; exists {
		panic(fmt.Sprintf("openapi: AddPath: %s %s is already registered — two entities or endpoints resolve to the same operation; use distinct paths or versions",
			strings.ToUpper(method), openapiPath))
	}
	s.paths[openapiPath][method] = op.ToMap()
}

// AddSchema registers a reusable schema component and panics on a name
// already registered.
func (s *Spec) AddSchema(name string, schema map[string]any) {
	if _, exists := s.schemas[name]; exists {
		panic(fmt.Sprintf("openapi: AddSchema: component %q is already registered — schema component names must be unique across entities and versions", name))
	}
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

func toOpenAPIPath(p string) string {
	parts := strings.Split(p, "/")
	for i, part := range parts {
		if strings.HasPrefix(part, ":") {
			parts[i] = "{" + part[1:] + "}"
		}
	}
	return strings.Join(parts, "/")
}

func extractPathParams(path string) []string {
	var params []string
	for part := range strings.SplitSeq(path, "/") {
		if strings.HasPrefix(part, ":") {
			params = append(params, part[1:])
		} else if strings.HasPrefix(part, "{") && strings.HasSuffix(part, "}") {
			params = append(params, part[1:len(part)-1])
		}
	}
	return params
}

func pathParamNames(params []map[string]any) map[string]struct{} {
	names := make(map[string]struct{}, len(params))
	for _, p := range params {
		if p["in"] != "path" {
			continue
		}
		if name, ok := p["name"].(string); ok {
			names[name] = struct{}{}
		}
	}
	return names
}
