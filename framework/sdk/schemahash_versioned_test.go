package sdk

import (
	"fmt"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// versionedTestRegistry is a slice-backed entity.Registry that can hold
// multiple versions of the same name — something the map-backed
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
// ambiguity error for multi-version names, and the error was swallowed —
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

// TestSchemaHashIncludesVersion pins that Version is part of the hash
// identity: two entities with the same fields but different route prefixes
// produce different hashes. Without this, a version rename (same fields,
// new mount point) would not signal drift even though every generated
// client URL changed.
func TestSchemaHashIncludesVersion(t *testing.T) {
	base := SchemaHash([]NamedConfig{{Name: "posts", Config: postsConfig()}})
	versioned := SchemaHash([]NamedConfig{{Name: "posts", Config: postsConfig(), Version: "/api/v1"}})
	if base == versioned {
		t.Fatal("versioned entity hashed same as unversioned — Version not in hash identity")
	}
}

// TestSchemaHashVersionedDeterministic pins that the (name, version) sort
// is stable: swapping the input order of two same-name versions does not
// change the hash. Before the fix the sort key was Name-only, so two
// entries sharing a name could land in either order.
func TestSchemaHashVersionedDeterministic(t *testing.T) {
	cfg := postsConfig()
	a := SchemaHash([]NamedConfig{
		{Name: "posts", Config: cfg, Version: "/api/v1"},
		{Name: "posts", Config: cfg, Version: "/api/v2"},
	})
	b := SchemaHash([]NamedConfig{
		{Name: "posts", Config: cfg, Version: "/api/v2"},
		{Name: "posts", Config: cfg, Version: "/api/v1"},
	})
	if a != b {
		t.Fatal("same versioned entities in different input order hashed differently")
	}
}
