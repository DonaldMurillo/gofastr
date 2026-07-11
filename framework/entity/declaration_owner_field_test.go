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

func TestEntityDeclaration_CrossOwnerReadRoundTrip(t *testing.T) {
	raw := []byte(`{
		"name": "tickets",
		"owner_field": "user_id",
		"cross_owner_read": "tickets:read:all",
		"fields": [
			{"name": "subject", "type": "text"}
		]
	}`)
	var d entity.EntityDeclaration
	if err := json.Unmarshal(raw, &d); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if d.CrossOwnerRead != "tickets:read:all" {
		t.Fatalf("decl CrossOwnerRead = %q, want tickets:read:all", d.CrossOwnerRead)
	}
	cfg, err := d.Config()
	if err != nil {
		t.Fatalf("Config: %v", err)
	}
	if cfg.CrossOwnerRead != "tickets:read:all" {
		t.Fatalf("cfg.CrossOwnerRead = %q, want tickets:read:all", cfg.CrossOwnerRead)
	}

	// omitempty: zero-value declaration must not emit the key.
	zero := entity.EntityDeclaration{
		Name:   "logs",
		Fields: []entity.FieldDeclaration{{Name: "note", Type: "text"}},
	}
	out, err := json.Marshal(zero)
	if err != nil {
		t.Fatalf("marshal zero: %v", err)
	}
	if contains(string(out), "cross_owner_read") {
		t.Fatalf("cross_owner_read present in zero-value JSON: %s", out)
	}
}

func TestEntityDeclaration_SearchFieldsRoundTrip(t *testing.T) {
	raw := []byte(`{
		"name": "articles",
		"search_fields": ["title", "body"],
		"fields": [
			{"name": "title", "type": "string"},
			{"name": "body", "type": "text"}
		]
	}`)
	var d entity.EntityDeclaration
	if err := json.Unmarshal(raw, &d); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(d.SearchFields) != 2 || d.SearchFields[0] != "title" || d.SearchFields[1] != "body" {
		t.Fatalf("decl SearchFields = %+v, want [title body]", d.SearchFields)
	}
	cfg, err := d.Config()
	if err != nil {
		t.Fatalf("Config: %v", err)
	}
	if len(cfg.SearchFields) != 2 || cfg.SearchFields[0] != "title" {
		t.Fatalf("cfg.SearchFields = %+v, want [title body]", cfg.SearchFields)
	}

	// omitempty: zero-value declaration must not emit the key.
	zero := entity.EntityDeclaration{
		Name:   "logs",
		Fields: []entity.FieldDeclaration{{Name: "note", Type: "text"}},
	}
	out, err := json.Marshal(zero)
	if err != nil {
		t.Fatalf("marshal zero: %v", err)
	}
	if contains(string(out), "search_fields") {
		t.Fatalf("search_fields present in zero-value JSON: %s", out)
	}
}
