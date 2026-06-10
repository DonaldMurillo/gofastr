package entity_test

import (
	"encoding/json"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/entity"
)

func TestDeclarationAccessToConfig(t *testing.T) {
	raw := []byte(`{
		"name": "posts",
		"access": {
			"read": "posts:read",
			"create": "posts:write",
			"update": "posts:write",
			"delete": "posts:admin"
		},
		"fields": [
			{"name": "title", "type": "string"}
		]
	}`)
	var d entity.EntityDeclaration
	if err := json.Unmarshal(raw, &d); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if d.Access == nil || d.Access.Delete != "posts:admin" {
		t.Fatalf("decl Access = %#v", d.Access)
	}
	cfg, err := d.Config()
	if err != nil {
		t.Fatalf("Config: %v", err)
	}
	want := entity.AccessControl{
		Read:   "posts:read",
		Create: "posts:write",
		Update: "posts:write",
		Delete: "posts:admin",
	}
	if cfg.Access != want {
		t.Fatalf("cfg.Access = %#v, want %#v", cfg.Access, want)
	}
}

func TestDeclarationAccessOmittedWhenNil(t *testing.T) {
	d := entity.EntityDeclaration{
		Name:   "posts",
		Fields: []entity.FieldDeclaration{{Name: "title", Type: "string"}},
	}
	out, err := json.Marshal(d)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if contains(string(out), "access") {
		t.Fatalf("access present in zero-value JSON: %s", out)
	}
	cfg, err := d.Config()
	if err != nil {
		t.Fatalf("Config: %v", err)
	}
	if cfg.Access != (entity.AccessControl{}) {
		t.Fatalf("cfg.Access = %#v, want zero", cfg.Access)
	}
}
