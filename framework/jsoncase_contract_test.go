package framework

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/crud"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// AppConfig.JSONCase = snake_case is the documented switch for a
// snake_case wire format (entity-declarations.md, auth.md). App wired it
// by assigning CrudHandler.JSONCase after NewCrudHandler had already
// keyed its field cache in camelCase, so every auto-CRUD route kept
// answering `authorName`; and the OpenAPI document assumed camelCase
// outright. A client built from either contract read nothing back.
//
// The property: under snake_case, the keys a route answers with are the
// keys the spec's component and request bodies name, on both dialects.
func TestSnakeCaseAppAnswersAndDocumentsSnakeKeys(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		app := NewApp(WithConfig(AppConfig{JSONCase: crud.CaseSnake}), WithDB(db), WithPublicOpenAPI())
		app.Entity("jc_orders", entity.EntityConfig{
			Table: "jc_orders",
			Fields: []schema.Field{
				{Name: "author_name", Type: schema.String, Required: true},
				{Name: "unit_count", Type: schema.Int},
			},
			Exposure: &entity.ExposureConfig{Public: true},
		})
		app, cleanup := startApp(t, app)
		defer cleanup()
		ta := TestHarness(t, app).AsUser(struct{ ID string }{ID: "u1"})
		defer ta.Close()

		created := ta.Post("/jc_orders", map[string]any{"author_name": "Ada", "unit_count": 3})
		created.AssertStatus(t, http.StatusCreated)
		var body struct {
			Data map[string]any `json:"data"`
		}
		if err := created.JSON(&body); err != nil {
			t.Fatal(err)
		}
		for _, k := range []string{"author_name", "unit_count", "created_at"} {
			if _, ok := body.Data[k]; !ok {
				t.Errorf("snake_case app answered without %q: %v", k, body.Data)
			}
		}
		for _, k := range []string{"authorName", "unitCount", "createdAt"} {
			if _, ok := body.Data[k]; ok {
				t.Errorf("snake_case app answered the camelCase key %q", k)
			}
		}

		rec := httptest.NewRecorder()
		app.router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/openapi.json", nil))
		var doc map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil {
			t.Fatal(err)
		}
		comp := doc["components"].(map[string]any)["schemas"].(map[string]any)["jc_orders"].(map[string]any)
		props := comp["properties"].(map[string]any)
		var specKeys, wireKeys []string
		for k := range props {
			specKeys = append(specKeys, k)
		}
		for k := range body.Data {
			wireKeys = append(wireKeys, k)
		}
		slices.Sort(specKeys)
		slices.Sort(wireKeys)
		if !slices.Equal(specKeys, wireKeys) {
			t.Errorf("spec component names %v, the route answers %v", specKeys, wireKeys)
		}
		if !schemaAccepts(t, doc, comp, body.Data) {
			t.Errorf("spec component refuses the row the route returned: %v", body.Data)
		}
		create := doc["paths"].(map[string]any)["/jc_orders"].(map[string]any)["post"].(map[string]any)
		reqSchema := create["requestBody"].(map[string]any)["content"].(map[string]any)["application/json"].(map[string]any)["schema"].(map[string]any)
		if req, _ := reqSchema["required"].([]any); !slices.Contains(req, any("author_name")) {
			t.Errorf("create body requires %v, want author_name", req)
		}

		list := ta.Get("/jc_orders")
		list.AssertStatus(t, http.StatusOK)
		if b := list.Body(); !strings.Contains(b, `"author_name":"Ada"`) || strings.Contains(b, "authorName") {
			t.Errorf("list answered other keys than the spec names: %s", b)
		}
	})
}
