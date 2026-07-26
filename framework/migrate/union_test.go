package migrate

import (
	"errors"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// TestUnionEntities_SingleVersionUnchanged proves the core invariant: a
// registry where every name has exactly one version produces the exact same
// map as Registry.All() — unionEntities returns the registered pointer
// directly, not a clone, so the single-version migration path is
// byte-for-byte identical to the pre-union behaviour.
func TestUnionEntities_SingleVersionUnchanged(t *testing.T) {
	e1 := rawEnt("posts", "posts", []schema.Field{{Name: "title", Type: schema.String}}, nil, "")
	e2 := rawEnt("users", "users", []schema.Field{{Name: "email", Type: schema.String}}, nil, "")
	reg := testReg{"posts": e1, "users": e2}

	got := UnionEntities(reg)
	if len(got) != 2 {
		t.Fatalf("unionEntities returned %d entries, want 2", len(got))
	}
	// Pointers must be identical — no cloning for single-version names.
	if got["posts"] != e1 {
		t.Error("unionEntities cloned a single-version entity (posts) — must return the registered pointer")
	}
	if got["users"] != e2 {
		t.Error("unionEntities cloned a single-version entity (users) — must return the registered pointer")
	}
}

// TestUnionEntities_MultiVersionMergesFields proves the union: two versions
// of one name produce a single merged entity whose field set is the union,
// and the table name comes from the representative.
func TestUnionEntities_MultiVersionMergesFields(t *testing.T) {
	v1 := &entity.Entity{
		Config: entity.EntityConfig{
			Name:  "posts",
			Table: "posts",
			Fields: []schema.Field{
				{Name: "title", Type: schema.String},
			},
		},
		Version: "/api/v1",
	}
	v2 := &entity.Entity{
		Config: entity.EntityConfig{
			Name:  "posts",
			Table: "posts",
			Fields: []schema.Field{
				{Name: "title", Type: schema.String},
				{Name: "summary", Type: schema.Text},
			},
		},
		Version: "/api/v2",
	}
	// AllSorted returns alphabetical by (name, version); "/api/v1" < "/api/v2".
	reg := multiVersionRegistry{v1, v2}

	got := UnionEntities(reg)
	if len(got) != 1 {
		t.Fatalf("unionEntities returned %d entries, want 1 (one per name)", len(got))
	}
	merged, ok := got["posts"]
	if !ok {
		t.Fatal("missing 'posts' in union result")
	}
	if merged.GetTable() != "posts" {
		t.Errorf("merged table = %q, want posts", merged.GetTable())
	}
	// The merged field set must contain both title (shared) and summary (v2-only).
	fieldNames := map[string]bool{}
	for _, f := range merged.GetFields() {
		fieldNames[f.Name] = true
	}
	if !fieldNames["title"] {
		t.Error("union field set missing 'title' (shared across versions)")
	}
	if !fieldNames["summary"] {
		t.Error("union field set missing 'summary' (v2-only column that must be created)")
	}
	// The merged entity must NOT be the same pointer as v1 or v2 — it is a clone
	// so the registered entities are never mutated.
	if merged == v1 || merged == v2 {
		t.Error("unionEntities returned a registered pointer for a multi-version name — must clone")
	}
}

// TestUnionEntities_PrefersUnversionedAsBase proves that when an unversioned
// entity coexists with versioned ones, the unversioned entity is the base
// (its Table, PrimaryKey, scope flags survive into the merged entity).
func TestUnionEntities_PrefersUnversionedAsBase(t *testing.T) {
	unversioned := &entity.Entity{
		Config: entity.EntityConfig{
			Name:  "posts",
			Table: "posts",
			Fields: []schema.Field{
				{Name: "title", Type: schema.String},
			},
		},
	}
	versioned := &entity.Entity{
		Config: entity.EntityConfig{
			Name:  "posts",
			Table: "posts",
			Fields: []schema.Field{
				{Name: "title", Type: schema.String},
				{Name: "summary", Type: schema.Text},
			},
		},
		Version: "/api/v2",
	}
	// AllSorted orders "" before "/api/v2".
	reg := multiVersionRegistry{unversioned, versioned}

	got := UnionEntities(reg)
	merged := got["posts"]
	// The merged entity's base is the unversioned one, so it carries the
	// unversioned Version ("") — even though the clone itself is a new pointer.
	if merged.Version != "" {
		t.Errorf("merged base should be the unversioned entity (Version=''), got %q", merged.Version)
	}
	// But the field union includes the versioned-only column.
	hasSummary := false
	for _, f := range merged.GetFields() {
		if f.Name == "summary" {
			hasSummary = true
		}
	}
	if !hasSummary {
		t.Error("unversioned-base merge should still include versioned-only 'summary' in the union")
	}
}

// multiVersionRegistry is a minimal entity.Registry whose AllSorted returns
// every version (not deduped by name), so unionEntities can be exercised
// against the multi-version case from within the migrate package.
type multiVersionRegistry []*entity.Entity

func (m multiVersionRegistry) All() map[string]*entity.Entity {
	out := make(map[string]*entity.Entity, len(m))
	for _, e := range m {
		out[e.Config.Name] = e // last wins; not used by unionEntities
	}
	return out
}

func (m multiVersionRegistry) AllSorted() []*entity.Entity {
	// Already sorted by (name, version) at construction time in the tests above.
	return m
}

func (m multiVersionRegistry) Get(name string) (*entity.Entity, error) {
	for _, e := range m {
		if e.Config.Name == name {
			return e, nil
		}
	}
	return nil, errors.New("not found")
}
