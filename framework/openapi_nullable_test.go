package framework

import (
	"database/sql"
	"net/http"
	"testing"
)

// The route takes null for an optional field and stores it, so reads
// return null. The spec typed those fields without null, so a strict
// validator refused the request that set one and every read after it.
func TestSpecAcceptsNullOptionalFields(t *testing.T) {
	forEachDialect(t, func(t *testing.T, _ *sql.DB, dialect Dialect) {
		app, specDoc, cleanup := setupOpenAPIServer(t, dialect)
		defer cleanup()
		ta := TestHarness(t, app).AsUser(struct{ ID string }{ID: "u1"})
		defer ta.Close()

		bodySchema := func(path, method string) map[string]any {
			op := specDoc["paths"].(map[string]any)[path].(map[string]any)[method].(map[string]any)
			return op["requestBody"].(map[string]any)["content"].(map[string]any)["application/json"].(map[string]any)["schema"].(map[string]any)
		}
		nulls := map[string]any{"title": "Hello", "body": nil, "status": nil, "authorId": nil}

		if !schemaAccepts(t, specDoc, bodySchema("/posts", "post"), nulls) {
			t.Error("the create schema refuses null optional fields")
		}
		for _, method := range []string{"put", "patch"} {
			if !schemaAccepts(t, specDoc, bodySchema("/posts/{id}", method), nulls) {
				t.Errorf("the %s schema refuses null optional fields", method)
			}
		}

		ta.Put("/posts/p1", nulls).AssertStatus(t, http.StatusOK)
		created := ta.Post("/posts", nulls)
		created.AssertStatus(t, http.StatusCreated)
		var made map[string]any
		if err := created.JSON(&made); err != nil {
			t.Fatal(err)
		}
		single := getSpecResponseSchema(t, specDoc, "/posts/{id}", "get", "200")
		for _, url := range []string{"/posts/p1", "/posts/" + made["data"].(map[string]any)["id"].(string)} {
			var got map[string]any
			if err := ta.Get(url).JSON(&got); err != nil {
				t.Fatal(err)
			}
			row := got["data"].(map[string]any)
			for _, k := range []string{"body", "status", "authorId"} {
				if v, ok := row[k]; !ok || v != nil {
					t.Fatalf("%s: %s = %v, want a stored null", url, k, v)
				}
			}
			if !schemaAccepts(t, specDoc, single, got) {
				t.Errorf("%s: the read schema refuses the stored row %v", url, row)
			}
		}

		// A required field, the key and the timestamps are never null.
		var got map[string]any
		if err := ta.Get("/posts/p1").JSON(&got); err != nil {
			t.Fatal(err)
		}
		for _, k := range []string{"title", "id", "createdAt"} {
			row := map[string]any{}
			for rk, rv := range got["data"].(map[string]any) {
				row[rk] = rv
			}
			row[k] = nil
			if schemaAccepts(t, specDoc, single, map[string]any{"data": row}) {
				t.Errorf("the read schema accepts a null %s", k)
			}
		}
	})
}
