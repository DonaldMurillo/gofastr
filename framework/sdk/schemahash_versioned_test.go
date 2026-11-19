package sdk

import (
	"fmt"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// versionedTestRegistry is a slice-backed entity.Registry that can hold
// multiple versions of the same name, something the map-backed
// fakeRegistry in sdk_test.go cannot do.
type versionedTestRegistry struct{ ents []*entity.Entity }

func (r *versionedTestRegistry) All() map[string]*entity.Entity {
	m := make(map[string]*entity.Entity, len(r.ents))
	for _, e := range r.ents {
		m[e.Config.Name] = e
	}
	return m
}
func (r *versionedTestRegistry) AllSorted() []*entity.Entity { return r.ents }
func (r *versionedTestRegistry) Get(name string) (*entity.Entity, error) {
	for _, e := range r.ents {
		if e.Config.Name == name {
			return e, nil
		}
	}
	return nil, fmt.Errorf("entity %q not found", name)
}

// TestRegistryNamedConfigsResolvesVersionedEntity pins that a name
// registered under multiple API versions is NOT silently dropped. Before
// the fix, RegistryNamedConfigs called reg.Get(name), which returns an
// ambiguity error for multi-version names, and the error was swallowed,
// so a versioned entity looked identical to a deleted one and the live
// hash reported false drift.
func TestRegistryNamedConfigsResolvesVersionedEntity(t *testing.T) {
	v1 := entity.Define("posts", postsConfig())
	v1.Version = "/api/v1"
	v2 := entity.Define("posts", postsConfig())
	v2.Version = "/api/v2"
	reg := &versionedTestRegistry{ents: []*entity.Entity{v1, v2}}

	named := RegistryNamedConfigs(reg, []string{"posts"})
	if len(named) != 2 {
		t.Fatalf("expected 2 versioned configs for 'posts', got %d: %+v", len(named), named)
	}
	seen := map[string]bool{}
	for _, n := range named {
		seen[n.Version] = true
	}
	if !seen["/api/v1"] || !seen["/api/v2"] {
		t.Errorf("expected both /api/v1 and /api/v2, got %v", seen)
	}
}

// The mount point is not part of the hash identity. It was, and that made
// the drift check fire forever on any app using a route group: the serving
// side reads Version off the entity (App.GroupEntity stamps the group
// prefix) while the generation side only ever sees a declaration, which has
// no mount. The two halves could not agree, so regenerating never cleared
// the warning.
func TestSchemaHashIgnoresMountPoint(t *testing.T) {
	base := hashOf([]NamedConfig{{Name: "posts", Config: postsConfig()}})
	versioned := hashOf([]NamedConfig{{Name: "posts", Config: postsConfig(), Version: "/api/v1"}})
	if base != versioned {
		t.Fatal("same schema hashed differently because of its mount point")
	}
}

// Versions that expose the SAME shape collapse to one entry, so a live
// registry holding two mounts still matches a manifest built from the one
// declaration behind them.
func TestSchemaHashCollapsesIdenticalVersions(t *testing.T) {
	cfg := postsConfig()
	one := hashOf([]NamedConfig{{Name: "posts", Config: cfg}})
	two := hashOf([]NamedConfig{
		{Name: "posts", Config: cfg, Version: "/api/v1"},
		{Name: "posts", Config: cfg, Version: "/api/v2"},
	})
	if one != two {
		t.Fatal("two mounts of one unchanged schema did not match the single declaration behind them")
	}
}

// TestSchemaHashVersionedDeterministic checks that sorting by name and canonical JSON orders divergent same-name versions consistently.
func TestSchemaHashVersionedDeterministic(t *testing.T) {
	v1 := NamedConfig{Name: "posts", Config: postsConfig(), Version: "/api/v1"}
	v2 := NamedConfig{Name: "posts", Config: postsConfig(), Version: "/api/v2"}
	v2.Config.Fields = append(v2.Config.Fields, schema.Field{Name: "subtitle", Type: schema.String})
	a := hashOf([]NamedConfig{v1, v2})
	b := hashOf([]NamedConfig{v2, v1})
	if a != b {
		t.Fatal("same versioned entities in different input order hashed differently")
	}
}
