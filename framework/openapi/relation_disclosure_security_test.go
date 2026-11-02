package openapi

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/access"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// Property: an entity the caller cannot read stays out of their view of
// the document entirely, its name included (EntityOpenAPIFor's
// RequestView contract). A readable entity's relation field named it
// anyway: core/openapi stamps x-relation with the target's name, and the
// include read view described "the related <target> record".
// Surfaces: typeRelationKey's x-relation, readSchema's descriptions.
func TestRequestViewDoesNotNameUnreadableRelationTargets(t *testing.T) {
	ledger := entity.Define("ledger_entries", entity.EntityConfig{
		Table:    "ledger_entries",
		Exposure: &entity.ExposureConfig{Public: true},
		Fields:   []schema.Field{{Name: "amount", Type: schema.Int}},
	})
	notes := entity.Define("notes", entity.EntityConfig{
		Table:    "notes",
		Exposure: &entity.ExposureConfig{Public: true},
		Fields: []schema.Field{
			{Name: "body", Type: schema.String},
			{Name: "entry_id", Type: schema.Relation, To: "ledger_entries"},
		},
		Relations: []entity.Relation{entity.HasMany("audits", "ledger_entries", "note_id")},
	})
	s := EntityOpenAPI(reg(ledger, notes), "Test", "1.0.0", nil)
	req := httptest.NewRequest("GET", "/openapi.json", nil)
	scoped := req.WithContext(access.WithHeldScopes(req.Context(), []string{"notes:read"}))
	raw, err := json.Marshal(s.RequestView(scoped).Build())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"entryId"`) {
		t.Fatalf("the fixture's readable entity is missing from the view: %s", raw)
	}
	if strings.Contains(string(raw), "ledger_entries") {
		t.Errorf("a notes:read token's view names the unreadable ledger_entries entity: %s", raw)
	}

	// The full document still says where the relation points.
	full, _ := json.Marshal(s.Build())
	if !strings.Contains(string(full), `"x-relation":"ledger_entries"`) {
		t.Errorf("the unfiltered document lost the relation's target: %s", full)
	}
}
