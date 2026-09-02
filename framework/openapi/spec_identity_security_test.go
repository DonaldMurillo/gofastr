package openapi

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// This file pins the identity surfaces of the published spec: query
// parameter names, operation IDs, and path keys. The spec is what SDK
// generators and agents build clients from, so two things claiming the
// same identifier inside one document is not cosmetic: a generator must
// then drop or silently repurpose one of them.

// ---------------------------------------------------------------------------
// Query-parameter namespace: a name is declared at most once per operation.
// ---------------------------------------------------------------------------

// countQueryParams counts how many entries the operation's parameter list
// declares under one name at one location.
func countQueryParams(op map[string]any, name string) int {
	n := 0
	if list, ok := op["parameters"].([]map[string]any); ok {
		for _, p := range list {
			if p["name"] == name && p["in"] == "query" {
				n++
			}
		}
		return n
	}
	anyList, ok := op["parameters"].([]any)
	if !ok {
		return 0
	}
	for _, p := range anyList {
		if pm, ok := p.(map[string]any); ok && pm["name"] == name && pm["in"] == "query" {
			n++
		}
	}
	return n
}

// Property: within the list operation, every query parameter name is
// declared exactly once. EntityOpenAPI emits a fixed set of list controls
// (page, limit, sort, cursor, direction, include, fields, trashed, q) and
// then appends one filter parameter per visible field using the field's
// raw name or WireName. A column whose name collides with a control word
// ("sort" for a sort-order column, "page" for a page number, "q" for a
// search ranking column are all ordinary schema choices; framework/filter
// explicitly documents "field wins over reserved") produces TWO parameters
// with the same name and conflicting schemas in one operation, which
// OpenAPI forbids and every generator resolves arbitrarily.
func TestFilterParamCollidesWithListControl(t *testing.T) {
	e := entity.Define("listopts", entity.EntityConfig{
		Table:        "listopts",
		Exposure:     &entity.ExposureConfig{Public: true},
		SearchFields: []string{"title"},
		Scope:        &entity.ScopeConfig{SoftDelete: true},
		Fields: []schema.Field{
			{Name: "title", Type: schema.String, Required: true},
			{Name: "sort", Type: schema.String},
			{Name: "page", Type: schema.Int},
			{Name: "cursor", Type: schema.String},
			{Name: "direction", Type: schema.String},
			{Name: "rank", Type: schema.String, WireName: "q"},
			{Name: "trashed", Type: schema.Bool},
		},
	})
	doc := EntityOpenAPI(reg(e), "Test", "1.0.0", nil).Build()
	op := getMap(t, getMap(t, getMap(t, doc, "paths"), "/listopts"), "get")
	for _, control := range []string{"sort", "page", "cursor", "direction", "q", "trashed"} {
		if n := countQueryParams(op, control); n != 1 {
			t.Errorf("query parameter %q declared %d times in the list operation (want exactly 1: the fixed control or the field filter, never both)", control, n)
		}
	}

	// Same property, suffix-folding shape: the range/_in suffixes emitted
	// for field "x" land in the same namespace as field "x_in"'s exact
	// match parameter.
	suffix := entity.Define("suffixfold", entity.EntityConfig{
		Table:    "suffixfold",
		Exposure: &entity.ExposureConfig{Public: true},
		Fields: []schema.Field{
			{Name: "x", Type: schema.Int},
			{Name: "x_in", Type: schema.String},
		},
	})
	doc = EntityOpenAPI(reg(suffix), "Test", "1.0.0", nil).Build()
	op = getMap(t, getMap(t, getMap(t, doc, "paths"), "/suffixfold"), "get")
	if n := countQueryParams(op, "x_in"); n != 1 {
		t.Errorf("query parameter %q declared %d times (field x's comma-list operator collides with field x_in's exact match; want exactly 1)", "x_in", n)
	}
}

// ---------------------------------------------------------------------------
// Operation IDs / MCP tool names derived from endpoint paths.
// ---------------------------------------------------------------------------

// collectOperationIDs returns every operationId in the built document with
// the number of operations claiming it.
func collectOperationIDs(doc map[string]any) map[string]int {
	ids := map[string]int{}
	// Build() stores paths as map[path]map[method]op; accept the
	// map[string]any shape too in case that changes.
	var pathEntries []map[string]any
	switch paths := doc["paths"].(type) {
	case map[string]map[string]any:
		for _, methods := range paths {
			pathEntries = append(pathEntries, methods)
		}
	case map[string]any:
		for _, v := range paths {
			if m, ok := v.(map[string]any); ok {
				pathEntries = append(pathEntries, m)
			}
		}
	default:
		return ids
	}
	for _, methods := range pathEntries {
		for _, opv := range methods {
			op, ok := opv.(map[string]any)
			if !ok {
				continue
			}
			if id, ok := op["operationId"].(string); ok {
				ids[id]++
			}
		}
	}
	return ids
}

func TestEndpointToolNameFoldCollides(t *testing.T) {
	pairs := [][2]string{
		{"/feed/items", "/feed-items"},
		{"/items/{id}", "/items/id"},
	}
	for _, p := range pairs {
		a := DefaultEndpointToolName("posts", "GET", p[0])
		b := DefaultEndpointToolName("posts", "GET", p[1])
		if a == b {
			t.Errorf("DefaultEndpointToolName folds %q and %q onto the same identifier %q; distinct routes must keep distinct tool names", p[0], p[1], a)
		}
	}
	if a, b := DefaultEndpointToolName("my-posts", "GET", "/x"), DefaultEndpointToolName("my_posts", "GET", "/x"); a == b {
		t.Errorf("entity names %q and %q fold onto the same tool-name prefix %q; two entities' tools would shadow each other", "my-posts", "my_posts", a)
	}
	if a, b := DefaultEndpointToolName("Posts", "GET", "/x"), DefaultEndpointToolName("posts", "GET", "/x"); a == b {
		t.Errorf("entity names %q and %q case-fold onto the same tool name %q", "Posts", "posts", a)
	}

	// Document surface: the same folding reaches the published spec as
	// duplicate operationIds (distinct path keys, so AddPath's duplicate
	// guard never fires).
	e := entity.Define("feed", entity.EntityConfig{
		Table:    "feed",
		Exposure: &entity.ExposureConfig{Public: true},
		Fields:   []schema.Field{{Name: "title", Type: schema.String}},
		Endpoints: []entity.Endpoint{
			{Method: "GET", Path: "feed-items"},
			{Method: "GET", Path: "feed/items"},
		},
	})
	doc := EntityOpenAPI(reg(e), "Test", "1.0.0", nil).Build()
	for id, n := range collectOperationIDs(doc) {
		if n > 1 {
			t.Errorf("operationId %q claimed by %d operations; identifiers in the published spec must be unique", id, n)
		}
	}
}

// ---------------------------------------------------------------------------
// Path keys: the documented path must be the path the router mounts.
// ---------------------------------------------------------------------------

// Property: a documented path key is a path the server actually serves.
// The router mounts grouped entities at their group prefix only
// (App.GroupEntity registers on the group's sub-router; the WithAPIPrefix
// doc states "GroupEntity routes are unaffected — a group owns its own
// prefix"), but EntityOpenAPI composes apiPrefix + ent.Version
// unconditionally. An app combining WithAPIPrefix("/api") with a group at
// "/api/v1" — the composition every multiversion example uses — gets a
// spec whose every versioned path is double-prefixed: /api/api/v1/posts
// where the route answers at /api/v1/posts. Generated SDKs and agents
// then call paths that 404.
func TestVersionedSpecDoubleAPIPrefix(t *testing.T) {
	versioned := entity.Define("posts", entity.EntityConfig{
		Table:    "posts",
		Exposure: &entity.ExposureConfig{Public: true},
		Fields:   []schema.Field{{Name: "title", Type: schema.String}},
		Endpoints: []entity.Endpoint{
			{Method: "POST", Path: "revoke"},
		},
	})
	versioned.Version = "/api/v1"

	doc := EntityOpenAPI(reg(versioned), "Test", "1.0.0", nil, "api").Build()
	paths := getMap(t, doc, "paths")
	sawVersioned := false
	for key := range paths {
		if strings.Contains(key, "/api/api/") {
			t.Errorf("path key %q double-applies the API prefix; the group prefix is absolute and the router mounts it once", key)
		}
		if strings.HasPrefix(key, "/api/v1/") {
			sawVersioned = true
		}
	}
	if !sawVersioned {
		t.Errorf("no path key under the group prefix /api/v1 (got %v); documented paths must match the mounted routes", mapKeys(paths))
	}
	// The custom endpoint surface composes the same two prefixes via
	// EntityEndpointRoutePath and double-prefixes identically.
	if _, ok := paths["/api/v1/posts/revoke"]; !ok {
		t.Errorf("custom endpoint path key missing or mis-prefixed (got %v); the endpoint is mounted at /api/v1/posts/revoke", mapKeys(paths))
	}
}

// ---------------------------------------------------------------------------
// Endpoint path keys: no dot-segment traversal.
// ---------------------------------------------------------------------------

// Property: a documented endpoint path is a literal route, so it must not
// carry dot segments. EntityEndpointPath joins a relative Endpoint.Path
// under the entity's table and canonicalizes only trailing slashes, so a
// "../admin" Path (endpoint declarations are blueprint/agent-authorable
// text, not just hand-written Go) surfaces as "/posts/../admin" in the
// spec — a path key no client can send literally. Whatever the router
// does with the pattern, the published contract is wrong, and a router
// that honors dot segments mounts the endpoint outside the entity's
// namespace entirely.
func TestEndpointPathRejectsDotSegments(t *testing.T) {
	e := entity.Define("posts", entity.EntityConfig{
		Table:    "posts",
		Exposure: &entity.ExposureConfig{Public: true},
		Fields:   []schema.Field{{Name: "title", Type: schema.String}},
	})
	for _, ep := range []string{"../admin", "items/../../widgets", "/../root"} {
		for label, got := range map[string]string{
			"EntityEndpointPath":      EntityEndpointPath(e, ep),
			"EntityEndpointRoutePath": EntityEndpointRoutePath(e, ep, "/api"),
		} {
			for _, seg := range strings.Split(strings.Trim(got, "/"), "/") {
				if seg == "." || seg == ".." {
					t.Errorf("%s(%q) = %q keeps a dot segment; documented endpoint paths must be literal (no client can send %q as-is)", label, ep, got, got)
					break
				}
			}
		}
	}
}

// ---------------------------------------------------------------------------
// NoQuery fields: not advertised as filter parameters.
// ---------------------------------------------------------------------------

// Property: a parameter the spec advertises is a parameter the server
// answers. The filter parser rejects NoQuery columns with a 400 under
// both their column name and wire name, so emitting an exact-match or
// range parameter for one generates SDK methods and agent calls that can
// only ever fail.
func TestNoQueryFieldOmitsFilterParams(t *testing.T) {
	e := entity.Define("cards", entity.EntityConfig{
		Table:    "cards",
		Exposure: &entity.ExposureConfig{Public: true},
		Fields: []schema.Field{
			{Name: "holder", Type: schema.String},
			{Name: "card_number", Type: schema.String, NoQuery: true},
		},
	})
	doc := EntityOpenAPI(reg(e), "Test", "1.0.0", nil).Build()
	op := getMap(t, getMap(t, getMap(t, doc, "paths"), "/cards"), "get")
	for _, name := range []string{"card_number", "card_number_like", "card_number_in", "cardNumber"} {
		if findParam(op["parameters"], name) != nil {
			t.Errorf("NoQuery column advertised as filter parameter %q; the runtime answers it with a 400", name)
		}
	}
	if findParam(op["parameters"], "holder") == nil {
		t.Errorf("queryable column %q missing its exact-match filter parameter (the guard must not over-strip)", "holder")
	}
}
