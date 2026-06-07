package openapi

import (
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// testRegistry is a minimal entity.Registry for driving EntityOpenAPI
// without pulling in the full framework App.
type testRegistry struct {
	ents []*entity.Entity
}

func (r *testRegistry) All() map[string]*entity.Entity {
	m := make(map[string]*entity.Entity, len(r.ents))
	for _, e := range r.ents {
		m[e.GetName()] = e
	}
	return m
}

func (r *testRegistry) AllSorted() []*entity.Entity { return r.ents }

func (r *testRegistry) Get(name string) (*entity.Entity, error) {
	for _, e := range r.ents {
		if e.GetName() == name {
			return e, nil
		}
	}
	return nil, entityNotFound(name)
}

type entityNotFound string

func (e entityNotFound) Error() string { return "entity not found: " + string(e) }

func reg(ents ...*entity.Entity) *testRegistry { return &testRegistry{ents: ents} }

// asMap coerces the various map shapes the spec builder emits
// (map[string]any, map[string]map[string]any) into map[string]any.
func asMap(v any) (map[string]any, bool) {
	switch m := v.(type) {
	case map[string]any:
		return m, true
	case map[string]map[string]any:
		out := make(map[string]any, len(m))
		for k, vv := range m {
			out[k] = vv
		}
		return out, true
	default:
		return nil, false
	}
}

// getMap is a small helper to descend into the built spec map.
func getMap(t *testing.T, m map[string]any, key string) map[string]any {
	t.Helper()
	v, ok := m[key]
	if !ok {
		t.Fatalf("missing key %q in %v", key, mapKeys(m))
	}
	mm, ok := asMap(v)
	if !ok {
		t.Fatalf("key %q is %T, not map", key, v)
	}
	return mm
}

func mapKeys(m map[string]any) []string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	return ks
}

// findParam returns the parameter named name from a []any parameter list.
func findParam(params any, name string) map[string]any {
	list, ok := params.([]map[string]any)
	if ok {
		for _, p := range list {
			if p["name"] == name {
				return p
			}
		}
		return nil
	}
	anyList, ok := params.([]any)
	if !ok {
		return nil
	}
	for _, p := range anyList {
		if pm, ok := p.(map[string]any); ok && pm["name"] == name {
			return pm
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// oa-1: gated (OwnerField / MultiTenant) operations must declare a 401.
// ---------------------------------------------------------------------------

// gatedOps returns the operation maps for an entity that auth/owner scoping
// can reject anonymously: list, get, create, update, delete.
func gatedOpsFor(t *testing.T, doc map[string]any, table string) map[string]map[string]any {
	t.Helper()
	paths := getMap(t, doc, "paths")
	base := getMap(t, paths, "/"+table)
	byID := getMap(t, paths, "/"+table+"/{id}")
	return map[string]map[string]any{
		"GET " + table:        getMap(t, base, "get"),
		"POST " + table:       getMap(t, base, "post"),
		"GET " + table + "/id": getMap(t, byID, "get"),
		"PUT " + table + "/id": getMap(t, byID, "put"),
		"DELETE " + table:     getMap(t, byID, "delete"),
	}
}

func hasResponse(op map[string]any, status string) bool {
	resps, ok := op["responses"].(map[int]map[string]any)
	if ok {
		// pre-Build form: responses keyed by int
		for code := range resps {
			if itoa(code) == status {
				return true
			}
		}
		return false
	}
	rm, ok := op["responses"].(map[string]any)
	if !ok {
		return false
	}
	_, ok = rm[status]
	return ok
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	neg := i < 0
	if neg {
		i = -i
	}
	var b [20]byte
	pos := len(b)
	for i > 0 {
		pos--
		b[pos] = byte('0' + i%10)
		i /= 10
	}
	if neg {
		pos--
		b[pos] = '-'
	}
	return string(b[pos:])
}

func TestOwnerScopedOpsDeclare401(t *testing.T) {
	e := entity.Define("notes", entity.EntityConfig{
		Table:      "notes",
		OwnerField: "user_id",
		Fields: []schema.Field{
			{Name: "title", Type: schema.String, Required: true},
		},
	})
	doc := EntityOpenAPI(reg(e), "Test", "1.0.0").Build()
	for label, op := range gatedOpsFor(t, doc, "notes") {
		if !hasResponse(op, "401") {
			t.Errorf("owner-scoped op %q missing 401 response", label)
		}
	}
}

func TestMultiTenantOpsDeclare401(t *testing.T) {
	e := entity.Define("invoices", entity.EntityConfig{
		Table:       "invoices",
		MultiTenant: true,
		Fields: []schema.Field{
			{Name: "amount", Type: schema.Int, Required: true},
		},
	})
	doc := EntityOpenAPI(reg(e), "Test", "1.0.0").Build()
	for label, op := range gatedOpsFor(t, doc, "invoices") {
		if !hasResponse(op, "401") {
			t.Errorf("multi-tenant op %q missing 401 response", label)
		}
	}
}

// An unguarded entity (no OwnerField, not MultiTenant) must NOT gain a
// spurious 401 — the fix must be scoped to gated entities only.
func TestUnguardedOpsHaveNo401(t *testing.T) {
	e := entity.Define("public_posts", entity.EntityConfig{
		Table: "public_posts",
		Fields: []schema.Field{
			{Name: "title", Type: schema.String},
		},
	})
	doc := EntityOpenAPI(reg(e), "Test", "1.0.0").Build()
	paths := getMap(t, doc, "paths")
	base := getMap(t, paths, "/public_posts")
	get := getMap(t, base, "get")
	if hasResponse(get, "401") {
		t.Error("unguarded entity GET should not declare a 401")
	}
}

// ---------------------------------------------------------------------------
// oa-2: range/_like filter params must not be emitted for bool/JSON fields.
// ---------------------------------------------------------------------------

func TestBoolFieldOmitsRangeFilters(t *testing.T) {
	e := entity.Define("flags", entity.EntityConfig{
		Table: "flags",
		Fields: []schema.Field{
			{Name: "active", Type: schema.Bool},
		},
	}.WithTimestamps(false))
	doc := EntityOpenAPI(reg(e), "Test", "1.0.0").Build()
	paths := getMap(t, doc, "paths")
	base := getMap(t, paths, "/flags")
	get := getMap(t, base, "get")
	params := get["parameters"]

	// Exact match and _in are fine for booleans; range/text ones are not.
	if findParam(params, "active") == nil {
		t.Error("expected exact-match param 'active'")
	}
	for _, suffix := range []string{"_gt", "_gte", "_lt", "_lte", "_like"} {
		if findParam(params, "active"+suffix) != nil {
			t.Errorf("bool field must not advertise %q filter param", "active"+suffix)
		}
	}
}

func TestJSONFieldOmitsRangeFilters(t *testing.T) {
	e := entity.Define("blobs", entity.EntityConfig{
		Table: "blobs",
		Fields: []schema.Field{
			{Name: "payload", Type: schema.JSON},
		},
	}.WithTimestamps(false))
	doc := EntityOpenAPI(reg(e), "Test", "1.0.0").Build()
	paths := getMap(t, doc, "paths")
	base := getMap(t, paths, "/blobs")
	get := getMap(t, base, "get")
	params := get["parameters"]

	for _, suffix := range []string{"_gt", "_gte", "_lt", "_lte", "_like"} {
		if findParam(params, "payload"+suffix) != nil {
			t.Errorf("JSON field must not advertise %q filter param", "payload"+suffix)
		}
	}
}

// Comparable fields (Int/Timestamp) must KEEP their range filters, and
// text fields must keep _like — the gate must not over-strip.
func TestComparableFieldsKeepRangeFilters(t *testing.T) {
	e := entity.Define("metrics", entity.EntityConfig{
		Table: "metrics",
		Fields: []schema.Field{
			{Name: "count", Type: schema.Int},
			{Name: "label", Type: schema.String},
		},
	}.WithTimestamps(false))
	doc := EntityOpenAPI(reg(e), "Test", "1.0.0").Build()
	paths := getMap(t, doc, "paths")
	base := getMap(t, paths, "/metrics")
	get := getMap(t, base, "get")
	params := get["parameters"]

	for _, suffix := range []string{"_gt", "_gte", "_lt", "_lte"} {
		if findParam(params, "count"+suffix) == nil {
			t.Errorf("int field must keep %q filter param", "count"+suffix)
		}
	}
	if findParam(params, "label_like") == nil {
		t.Error("string field must keep _like filter param")
	}
}
