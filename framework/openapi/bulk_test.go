package openapi

import (
	"net/http/httptest"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// Only the entities bulkMounted names get the bulk and export routes, and
// a gated entity's pair carries both auth schemes like its CRUD routes.
func TestBulkPathsOnlyForMounted(t *testing.T) {
	notes := entity.Define("notes", entity.EntityConfig{Table: "notes", Scope: &entity.ScopeConfig{OwnerField: "user_id"}, Fields: []schema.Field{{Name: "title", Type: schema.String}}})
	posts := entity.Define("posts", entity.EntityConfig{Table: "posts", Exposure: &entity.ExposureConfig{Public: true}, Fields: []schema.Field{{Name: "title", Type: schema.String}}})
	mounted := func(e *entity.Entity) bool { return e.GetName() == "notes" }
	doc := EntityOpenAPIWithBulk(reg(notes, posts), "Test", "1.0.0", nil, mounted).Build()
	paths := getMap(t, doc, "paths")

	bulk := getMap(t, getMap(t, paths, "/notes/_bulk"), "post")
	if bulk["operationId"] != "bulk_notes" {
		t.Errorf("bulk operationId = %v, want bulk_notes", bulk["operationId"])
	}
	body := requestBodyProps(t, bulk)
	if got := propEnum(body["properties"].(map[string]any)["scope"]); len(got) != 4 || got[3] != "record" {
		t.Errorf("bulk scope enum = %v, want selected/page/every/record", got)
	}
	// A client built from the spec can encode every scope: every match
	// needs the digest the bar carries.
	if _, ok := body["properties"].(map[string]any)["match"]; !ok {
		t.Error("bulk body has no match property, so a typed client cannot send scope every")
	}
	then, _ := body["then"].(map[string]any)
	if req, _ := then["required"].([]string); len(req) != 1 || req[0] != "match" {
		t.Errorf("scope every does not require match: if/then = %v / %v", body["if"], body["then"])
	}
	responses, _ := bulk["responses"].(map[int]map[string]any)
	for _, code := range []int{409, 415} {
		if _, ok := responses[code]; !ok {
			t.Errorf("bulk responses lack %d: %v", code, responses)
		}
	}
	export := getMap(t, getMap(t, paths, "/notes/_export.csv"), "get")
	if export["operationId"] != "export_notes" {
		t.Errorf("export operationId = %v, want export_notes", export["operationId"])
	}
	for label, op := range map[string]map[string]any{"bulk": bulk, "export": export} {
		if !hasOpScheme(op, "bearerAuth") || !hasOpScheme(op, "cookieAuth") {
			t.Errorf("gated %s route is missing an auth scheme: %v", label, op["security"])
		}
	}
	for _, p := range []string{"/posts/_bulk", "/posts/_export.csv"} {
		if _, ok := paths[p]; ok {
			t.Errorf("%s documented for an entity EntityUI did not mount", p)
		}
	}
	if _, ok := paths["/posts"]; !ok {
		t.Error("posts lost its CRUD paths")
	}
}

// The per-request view keeps the bulk routes for an entity the caller
// may read and drops the gated one with the rest of its paths.
func TestBulkPathsFollowRequestView(t *testing.T) {
	notes := entity.Define("notes", entity.EntityConfig{Table: "notes", Scope: &entity.ScopeConfig{OwnerField: "user_id"}, Fields: []schema.Field{{Name: "title", Type: schema.String}}})
	posts := entity.Define("posts", entity.EntityConfig{Table: "posts", Exposure: &entity.ExposureConfig{Public: true}, Fields: []schema.Field{{Name: "title", Type: schema.String}}})
	all := func(*entity.Entity) bool { return true }
	s := EntityOpenAPIWithBulk(reg(notes, posts), "Test", "1.0.0", nil, all)
	paths := getMap(t, s.RequestView(httptest.NewRequest("GET", "/openapi.json", nil)).Build(), "paths")
	if _, ok := paths["/posts/_bulk"]; !ok {
		t.Error("an anonymous caller's view dropped the public entity's bulk route")
	}
	if _, ok := paths["/notes/_bulk"]; ok {
		t.Error("an anonymous caller's view kept the owner-scoped entity's bulk route")
	}
}
