package framework

import (
	"database/sql"
	"net/http"
	"testing"
)

// The list route's 200 is oneOf [ListResponse, CursorPage]. oneOf means
// exactly one variant matches, and with no required keys an offset body
// also satisfied CursorPage, so a strict validator refused every list
// response. Each real body must satisfy exactly one variant.
func TestListBodiesMatchOneVariant(t *testing.T) {
	forEachDialect(t, func(t *testing.T, _ *sql.DB, dialect Dialect) {
		app, specDoc, cleanup := setupOpenAPIServer(t, dialect)
		defer cleanup()
		ta := TestHarness(t, app).AsUser(struct{ ID string }{ID: "u1"})
		defer ta.Close()

		oneOf, ok := getSpecResponseSchema(t, specDoc, "/posts", "get", "200")["oneOf"].([]any)
		if !ok {
			t.Fatal("GET /posts 200 is not a oneOf")
		}
		for _, url := range []string{
			"/posts",
			"/posts?title=nobody",
			"/posts?cursor=",
			"/posts?cursor=&limit=1",
			"/posts?cursor=&title=nobody",
		} {
			resp := ta.Get(url)
			resp.AssertStatus(t, http.StatusOK)
			var body any
			if err := resp.JSON(&body); err != nil {
				t.Fatalf("%s: %v", url, err)
			}
			matched := 0
			for _, v := range oneOf {
				if schemaAccepts(t, specDoc, v.(map[string]any), body) {
					matched++
				}
			}
			if matched != 1 {
				t.Errorf("%s: body matches %d of the oneOf variants, want 1: %v", url, matched, body)
			}
			// The server always writes every key it wrote here, so a
			// body without one of them is not a list response.
			for key := range body.(map[string]any) {
				cut := map[string]any{}
				for k, v := range body.(map[string]any) {
					if k != key {
						cut[k] = v
					}
				}
				for _, v := range oneOf {
					if schemaAccepts(t, specDoc, v.(map[string]any), cut) {
						t.Errorf("%s: a body without %q still matches %v", url, key, v)
					}
				}
			}
		}
	})
}

// schemaAccepts is the JSON Schema subset the envelopes use: $ref, type,
// required, properties and items. Extra keys are allowed, as the spec
// does not close the envelopes.
func schemaAccepts(t *testing.T, specDoc, s map[string]any, v any) bool {
	t.Helper()
	if _, ok := s["$ref"]; ok {
		s = resolveSchemaRef(t, specDoc, s)
	}
	if typ, ok := s["type"]; ok && !jsonTypeIs(typ, v) {
		return false
	}
	obj, isObj := v.(map[string]any)
	if req, ok := s["required"].([]any); ok && isObj {
		for _, k := range req {
			if _, ok := obj[k.(string)]; !ok {
				return false
			}
		}
	}
	if props, ok := s["properties"].(map[string]any); ok && isObj {
		for k, ps := range props {
			if pv, ok := obj[k]; ok && !schemaAccepts(t, specDoc, ps.(map[string]any), pv) {
				return false
			}
		}
	}
	if items, ok := s["items"].(map[string]any); ok {
		if arr, ok := v.([]any); ok {
			for _, it := range arr {
				if !schemaAccepts(t, specDoc, items, it) {
					return false
				}
			}
		}
	}
	return true
}

func jsonTypeIs(typ any, v any) bool {
	if list, ok := typ.([]any); ok {
		for _, one := range list {
			if jsonTypeIs(one, v) {
				return true
			}
		}
		return false
	}
	switch typ {
	case "object":
		_, ok := v.(map[string]any)
		return ok
	case "array":
		_, ok := v.([]any)
		return ok
	case "string":
		_, ok := v.(string)
		return ok
	case "boolean":
		_, ok := v.(bool)
		return ok
	case "integer":
		f, ok := v.(float64)
		return ok && f == float64(int64(f))
	case "number":
		_, ok := v.(float64)
		return ok
	case "null":
		return v == nil
	}
	return false
}
