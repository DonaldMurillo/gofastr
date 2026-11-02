package framework

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// A Relation column holds its target's key: an integer when the target
// is keyed by an AutoIncrement int (crud stores and answers it as a JSON
// number), text when the target is keyed by a code. The spec typed every
// Relation as a uuid string, so a strict client refused every row of an
// entity pointing at an integer-keyed table, and a generated client sent
// "7" where the route reads 7.
//
// The property: the row a route answers, and the body it accepts, pass the
// spec's schema for that entity, on both dialects.
func TestSpecTypesRelationByTargetKey(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		app := NewApp(WithDB(db), WithPublicOpenAPI())
		app.Entity("rk_team", entity.EntityConfig{
			Table: "rk_teams",
			Fields: []schema.Field{
				{Name: "id", Type: schema.Int, AutoGenerate: schema.AutoIncrement},
				{Name: "name", Type: schema.String},
			},
			Exposure: &entity.ExposureConfig{Public: true},
		}.WithTimestamps(false))
		app.Entity("rk_currency", entity.EntityConfig{
			Table:      "rk_currencies",
			Fields:     []schema.Field{{Name: "code", Type: schema.String, Required: true}},
			PrimaryKey: []string{"code"},
			Exposure:   &entity.ExposureConfig{Public: true},
		}.WithTimestamps(false))
		app.Entity("rk_player", entity.EntityConfig{
			Table: "rk_players",
			Fields: []schema.Field{
				{Name: "name", Type: schema.String},
				{Name: "team_id", Type: schema.Relation, To: "rk_team"},
				{Name: "currency_code", Type: schema.Relation, To: "rk_currency"},
			},
			Exposure: &entity.ExposureConfig{Public: true},
		}.WithTimestamps(false))
		app, cleanup := startApp(t, app)
		defer cleanup()
		ta := TestHarness(t, app)
		defer ta.Close()

		rec := httptest.NewRecorder()
		app.router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/openapi.json", nil))
		var doc map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil {
			t.Fatal(err)
		}
		comp := doc["components"].(map[string]any)["schemas"].(map[string]any)["rk_player"].(map[string]any)
		create := doc["paths"].(map[string]any)["/rk_players"].(map[string]any)["post"].(map[string]any)
		reqSchema := create["requestBody"].(map[string]any)["content"].(map[string]any)["application/json"].(map[string]any)["schema"].(map[string]any)

		var team struct{ Data map[string]any }
		ta.Post("/rk_teams", map[string]any{"name": "Owls"}).AssertStatus(t, http.StatusCreated).JSON(&team)
		ta.Post("/rk_currencies", map[string]any{"code": "EUR"}).AssertStatus(t, http.StatusCreated)

		body := map[string]any{"name": "Ann", "teamId": team.Data["id"], "currencyCode": "EUR"}
		if !schemaAccepts(t, doc, reqSchema, body) {
			t.Errorf("the create schema refuses the body the route takes: %v (schema %v)", body, reqSchema["properties"])
		}
		var made struct{ Data map[string]any }
		ta.Post("/rk_players", body).AssertStatus(t, http.StatusCreated).JSON(&made)
		if _, isNum := made.Data["teamId"].(float64); !isNum {
			t.Fatalf("route answered teamId %T %v; this test assumes a JSON number", made.Data["teamId"], made.Data["teamId"])
		}
		if !schemaAccepts(t, doc, comp, made.Data) {
			t.Errorf("the rk_player schema refuses the row the route returned: %v (schema %v)", made.Data, comp["properties"])
		}
		// The filter parser checks ?team_id= as an integer (abc is a 400).
		list := doc["paths"].(map[string]any)["/rk_players"].(map[string]any)["get"].(map[string]any)
		for _, p := range list["parameters"].([]any) {
			pm := p.(map[string]any)
			if pm["name"] == "team_id" {
				if typ := pm["schema"].(map[string]any)["type"]; typ != "integer" {
					t.Errorf("?team_id= is typed %v, the parser reads an integer", typ)
				}
			}
		}
		props := comp["properties"].(map[string]any)
		if f, _ := props["currencyCode"].(map[string]any)["format"].(string); f == "uuid" {
			t.Errorf("currencyCode holds a currency code, spec says format uuid")
		}
	})
}
