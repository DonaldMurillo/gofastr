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
	if d.Exposure == nil || d.Exposure.Access == nil || d.Exposure.Access.Delete != "posts:admin" {
		t.Fatalf("decl Exposure.Access = %#v", d.Exposure)
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
	if cfg.Exposure.Access != want {
		t.Fatalf("cfg.Exposure.Access = %#v, want %#v", cfg.Exposure.Access, want)
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
	if cfg.Exposure.Access != (entity.AccessControl{}) {
		t.Fatalf("cfg.Exposure.Access = %#v, want zero", cfg.Exposure.Access)
	}
}
