package crud

import (
	"net/url"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// versionedReg implements entity.Registry AND entity.VersionedRegistry, so it
// exercises the version-aware resolution path.
type versionedReg struct{ m map[string]*entity.Entity } // key: name|version

func (r versionedReg) All() map[string]*entity.Entity {
	out := map[string]*entity.Entity{}
	for k, e := range r.m {
		out[strings.SplitN(k, "|", 2)[0]] = e
	}
	return out
}

func (r versionedReg) AllSorted() []*entity.Entity {
	out := make([]*entity.Entity, 0, len(r.m))
	for _, e := range r.m {
		out = append(out, e)
	}
	return out
}

// Get mirrors the real registry: unversioned wins, sole version otherwise,
// ambiguity is an error.
func (r versionedReg) Get(name string) (*entity.Entity, error) {
	if e, ok := r.m[name+"|"]; ok {
		return e, nil
	}
	var found []*entity.Entity
	for k, e := range r.m {
		if strings.SplitN(k, "|", 2)[0] == name {
			found = append(found, e)
		}
	}
	if len(found) == 1 {
		return found[0], nil
	}
	if len(found) == 0 {
		return nil, errVerNotFound
	}
	return nil, errVerAmbiguous
}

func (r versionedReg) GetVersioned(name, version string) (*entity.Entity, error) {
	if e, ok := r.m[name+"|"+version]; ok {
		return e, nil
	}
	return nil, errVerNotFound
}

type constErr string

func (e constErr) Error() string { return string(e) }

const (
	errVerNotFound  = constErr("not found")
	errVerAmbiguous = constErr("ambiguous: multiple versions")
)

// mkRelEntity builds an entity with a real declared Relation named "author"
// pointing at targetName. A Relation-TYPED FIELD is not the same thing:
// nested filters look the relation up in Config.Relations, so a field-only
// setup yields "unknown relation" and any test merely asserting "an error
// occurred" would pass without ever exercising the path under test.
func mkRelEntity(name, version, targetName string) *entity.Entity {
	e := entity.Define(name, entity.EntityConfig{
		Table:     name,
		Fields:    []schema.Field{{Name: "id", Type: schema.Int}, {Name: "author_id", Type: schema.Int}},
		Relations: []entity.Relation{entity.BelongsTo("author", targetName, "author_id")},
	}.WithTimestamps(false))
	e.Version = version
	return e
}

func mkEntity(name, version string, fields []schema.Field) *entity.Entity {
	e := entity.Define(name, entity.EntityConfig{Table: name, Fields: fields}.WithTimestamps(false))
	e.Version = version
	return e
}

// Sol review #1, part 2: the value-disclosure oracle.
//
// parseNestedFiltersValues used `if target, err := registry.Get(...); err == nil`,
// which SKIPPED the whole Hidden-field check whenever resolution failed, and
// resolution fails exactly when a name has several versions. Two versions of
// "users" therefore disabled the check, letting ?author.password_hash_like=…
// reach SQL and act as a substring oracle over a hidden column.
func TestNestedFilter_FailsClosedOnAmbiguousTarget(t *testing.T) {
	hidden := []schema.Field{
		{Name: "id", Type: schema.Int},
		{Name: "password_hash", Type: schema.String, Hidden: true},
	}
	// Two versions, NO unversioned → Get is ambiguous.
	reg := versionedReg{m: map[string]*entity.Entity{
		"users|/api/v1": mkEntity("users", "/api/v1", hidden),
		"users|/api/v2": mkEntity("users", "/api/v2", hidden),
	}}

	// posts is at /api/v3: there is no users@/api/v3 and no unversioned
	// users, so ResolveTarget falls through to Get, which is ambiguous
	// across v1/v2. That ambiguity is what used to disable the whole check.
	posts := mkRelEntity("posts", "/api/v3", "users")

	q := url.Values{"author.password_hash_like": []string{"$2a$"}}
	_, err := parseNestedFiltersValues(q, posts, reg)
	if err == nil {
		t.Fatal("ambiguous relation target did not fail closed — a hidden-column " +
			"substring oracle reached SQL")
	}
	// Assert it failed for the RIGHT reason. With a Relation-typed field
	// instead of a declared Relation the lookup dies at "unknown relation"
	// before resolution runs, and a bare err != nil check would pass without
	// ever exercising the path under test.
	if !strings.Contains(err.Error(), "resolve relation target") {
		t.Fatalf("failed for the wrong reason (test does not exercise resolution): %v", err)
	}
}

// Resolution must prefer the SOURCE's own version. registry.Get prefers the
// UNVERSIONED entity, so a v1 request whose relation target also exists
// unversioned would inherit the unversioned Hidden set, disclosing a column
// v1 declares hidden.
func TestResolveTarget_PrefersSourceVersionOverUnversioned(t *testing.T) {
	exposed := []schema.Field{
		{Name: "id", Type: schema.Int},
		{Name: "password_hash", Type: schema.String}, // NOT hidden
	}
	scrubbed := []schema.Field{
		{Name: "id", Type: schema.Int},
		{Name: "password_hash", Type: schema.String, Hidden: true},
	}
	reg := versionedReg{m: map[string]*entity.Entity{
		"users|":        mkEntity("users", "", exposed),         // internal, exposes it
		"users|/api/v1": mkEntity("users", "/api/v1", scrubbed), // public, hides it
	}}

	v1Posts := mkEntity("posts", "/api/v1", []schema.Field{{Name: "id", Type: schema.Int}})

	target, err := entity.ResolveTarget(reg, v1Posts, "users")
	if err != nil {
		t.Fatalf("resolve failed: %v", err)
	}
	if target.Version != "/api/v1" {
		t.Fatalf("resolved version %q, want /api/v1 — a v1 request must not adopt "+
			"the unversioned entity's visibility rules", target.Version)
	}
	for _, f := range target.GetFields() {
		if f.Name == "password_hash" && !f.Hidden {
			t.Error("resolved the entity that EXPOSES password_hash")
		}
	}

	// And a nested filter on the hidden column must be refused for v1.
	q := url.Values{"author.password_hash_like": []string{"x"}}
	posts := mkRelEntity("posts", "/api/v1", "users")
	if _, err := parseNestedFiltersValues(q, posts, reg); err == nil {
		t.Error("v1 nested filter on a v1-hidden column was allowed")
	}
}

// An unversioned source still resolves the unversioned target, the historical
// single-version path must be untouched.
func TestResolveTarget_UnversionedSourceUnchanged(t *testing.T) {
	users := mkEntity("users", "", []schema.Field{{Name: "id", Type: schema.Int}})
	reg := versionedReg{m: map[string]*entity.Entity{"users|": users}}
	posts := mkEntity("posts", "", []schema.Field{{Name: "id", Type: schema.Int}})

	got, err := entity.ResolveTarget(reg, posts, "users")
	if err != nil {
		t.Fatalf("resolve failed: %v", err)
	}
	if got != users {
		t.Error("unversioned resolution changed")
	}
}
