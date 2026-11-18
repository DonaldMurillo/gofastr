package apiversions_test

import (
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/experimental/apiversions"
)

func TestProjectionKeepsColumnsAndBase(t *testing.T) {
	base := entity.EntityConfig{Fields: []schema.Field{
		{Name: "title", Type: schema.String},
		{Name: "internal_notes", Type: schema.String},
		{Name: "body", Type: schema.String},
	}}
	ps := apiversions.NewProjectionSet(&apiversions.Projection{
		Version: "v1",
		Include: []string{"title", "internal_notes"},
		Exclude: []string{"internal_notes"},
		Rename:  map[string]string{"title": "headline"},
	})
	cfg := apiversions.ApplyToEntityConfig(base, ps, "v1")
	if len(cfg.Fields) != len(base.Fields) {
		t.Fatalf("projection changed the field set: %d fields, want %d", len(cfg.Fields), len(base.Fields))
	}
	for i, f := range cfg.Fields {
		if f.Name != base.Fields[i].Name {
			t.Errorf("field %d Name = %q, want %q", i, f.Name, base.Fields[i].Name)
		}
	}
	for _, f := range base.Fields {
		if f.Hidden || f.WireName != "" {
			t.Errorf("projection mutated the base config's %s: %+v", f.Name, f)
		}
	}
	want := map[string]bool{"title": false, "internal_notes": true, "body": true}
	for _, f := range cfg.Fields {
		if f.Hidden != want[f.Name] {
			t.Errorf("%s Hidden = %v, want %v", f.Name, f.Hidden, want[f.Name])
		}
	}
	if cfg.Fields[0].WireName != "headline" {
		t.Errorf("title WireName = %q, want headline", cfg.Fields[0].WireName)
	}
	if got := apiversions.ApplyToEntityConfig(base, ps, "v9"); len(got.Fields) != len(base.Fields) || got.Fields[0].Hidden {
		t.Errorf("a version with no projection and no default changed the config: %+v", got.Fields)
	}
}
