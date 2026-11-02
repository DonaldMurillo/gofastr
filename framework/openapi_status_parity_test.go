package framework

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/internal/asciicase"
)

// The write routes answer 409 for a write that collides with a unique
// field or a client-supplied key, and 415 for a body without the JSON
// content type; neither was in the spec, so a generated client had no
// type for the field-naming 409 body. The list clamps ?limit to the
// entity's Pagination.MaxListLimit and defaults to the smaller of 20 and
// that cap, while the spec (and llm.md) said "default 20, max 100".
//
// The property: each status a route answers below is one the spec lists
// for that operation, and the documented limit is the one the list
// applies, on both dialects.
func TestSpecListsTheStatusesWritesAnswer(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		app := NewApp(WithDB(db), WithPublicOpenAPI())
		app.Entity("sp_items", entity.EntityConfig{
			Table: "sp_items",
			Fields: []schema.Field{
				{Name: "code", Type: schema.String, Required: true, Unique: true},
			},
			Pagination: &entity.PaginationConfig{MaxListLimit: 5},
			Exposure:   &entity.ExposureConfig{Public: true},
		})
		app.Entity("sp_codes", entity.EntityConfig{
			Table:      "sp_codes",
			Fields:     []schema.Field{{Name: "code", Type: schema.String, Required: true}},
			PrimaryKey: []string{"code"},
			Exposure:   &entity.ExposureConfig{Public: true},
		}.WithTimestamps(false))
		app, cleanup := startApp(t, app)
		defer cleanup()

		rec := httptest.NewRecorder()
		app.router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/openapi.json", nil))
		var doc struct {
			Paths map[string]map[string]struct {
				Parameters []struct {
					Name        string         `json:"name"`
					Description string         `json:"description"`
					Schema      map[string]any `json:"schema"`
				} `json:"parameters"`
				Responses map[string]any `json:"responses"`
			} `json:"paths"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil {
			t.Fatal(err)
		}
		do := func(method, path, ct, body string) (int, string) {
			req := httptest.NewRequest(method, path, strings.NewReader(body))
			if ct != "" {
				req.Header.Set("Content-Type", ct)
			}
			req = req.WithContext(handler.SetUser(req.Context(), struct{ ID string }{ID: "u1"}))
			w := httptest.NewRecorder()
			app.router.ServeHTTP(w, req)
			return w.Code, w.Body.String()
		}
		listed := func(specPath, method string, code int) bool {
			_, ok := doc.Paths[specPath][asciicase.Lower(method)].Responses[fmt.Sprint(code)]
			return ok
		}
		expect := func(method, path, specPath, ct, body string, want int) {
			t.Helper()
			got, b := do(method, path, ct, body)
			if got != want {
				t.Fatalf("%s %s = %d %s, want %d", method, path, got, b, want)
			}
			if !listed(specPath, method, got) {
				t.Errorf("%s %s answers %d; the spec does not list it", method, specPath, got)
			}
		}

		_, made := do("POST", "/sp_items", "application/json", `{"code":"a"}`)
		var env struct{ Data map[string]any }
		_ = json.Unmarshal([]byte(made), &env)
		id := fmt.Sprint(env.Data["id"])
		do("POST", "/sp_items", "application/json", `{"code":"b"}`)

		expect("POST", "/sp_items", "/sp_items", "application/json", `{"code":"a"}`, http.StatusConflict)
		expect("PATCH", "/sp_items/"+id, "/sp_items/{id}", "application/json", `{"code":"b"}`, http.StatusConflict)
		expect("PUT", "/sp_items/"+id, "/sp_items/{id}", "application/json", `{"code":"b"}`, http.StatusConflict)
		do("POST", "/sp_codes", "application/json", `{"code":"EUR"}`)
		expect("POST", "/sp_codes", "/sp_codes", "application/json", `{"code":"EUR"}`, http.StatusConflict)

		for _, c := range []struct{ method, path, spec string }{
			{"POST", "/sp_items", "/sp_items"},
			{"PUT", "/sp_items/" + id, "/sp_items/{id}"},
			{"PATCH", "/sp_items/" + id, "/sp_items/{id}"},
			{"POST", "/sp_items/_batch", "/sp_items/_batch"},
			{"PATCH", "/sp_items/_batch", "/sp_items/_batch"},
			{"DELETE", "/sp_items/_batch", "/sp_items/_batch"},
		} {
			expect(c.method, c.path, c.spec, "text/plain", `{"code":"z"}`, http.StatusUnsupportedMediaType)
		}

		for i := range 6 {
			do("POST", "/sp_items", "application/json", fmt.Sprintf(`{"code":"x%d"}`, i))
		}
		_, page := do("GET", "/sp_items?limit=50", "", "")
		var list struct {
			PerPage int `json:"perPage"`
		}
		_ = json.Unmarshal([]byte(page), &list)
		for _, p := range doc.Paths["/sp_items"]["get"].Parameters {
			if p.Name != "limit" {
				continue
			}
			if d, _ := p.Schema["default"].(float64); int(d) != 5 {
				t.Errorf("spec limit default %v, the list's default is 5", p.Schema["default"])
			}
			if !strings.Contains(p.Description, fmt.Sprintf("max %d", list.PerPage)) {
				t.Errorf("spec limit says %q; ?limit=50 answered perPage %d", p.Description, list.PerPage)
			}
		}
		if _, body := do("GET", "/sp_items/llm.md", "", ""); !strings.Contains(body, "default 5, max 5") {
			t.Errorf("llm.md does not state the list's limit (default 5, max 5)")
		}
	})
}
