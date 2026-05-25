package entity_test

import (
	"encoding/json"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/entity"
)

func TestEntityDeclaration_OwnerFieldRoundTrip(t *testing.T) {
	raw := []byte(`{
		"name": "logs",
		"owner_field": "user_id",
		"fields": [
			{"name": "note", "type": "text"}
		]
	}`)
	var d entity.EntityDeclaration
	if err := json.Unmarshal(raw, &d); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if d.OwnerField != "user_id" {
		t.Fatalf("decl OwnerField = %q, want user_id", d.OwnerField)
	}
	cfg, err := d.Config()
	if err != nil {
		t.Fatalf("Config: %v", err)
	}
	if cfg.OwnerField != "user_id" {
		t.Fatalf("cfg.OwnerField = %q, want user_id", cfg.OwnerField)
	}
}

func TestEntityDeclaration_OmitsEmptyOwnerField(t *testing.T) {
	d := entity.EntityDeclaration{
		Name:   "logs",
		Fields: []entity.FieldDeclaration{{Name: "note", Type: "text"}},
	}
	out, err := json.Marshal(d)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if string(out) == "" {
		t.Fatal("empty marshal output")
	}
	// omitempty: owner_field key should be absent when zero.
	if got := string(out); contains(got, "owner_field") {
		t.Fatalf("owner_field present in zero-value JSON: %s", got)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
