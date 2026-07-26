package framework

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// The cross-version conflict guards refuse a config whose two versions would
// emit different DDL for one shared table. These exercise the shapes the
// happy-path tests never reach: an index identified by its synthesised name,
// each way an index definition can diverge, each relation-type mismatch, and
// each AutoGenerate mismatch — every one of which reaches an error message
// that has to name the difference for the author to act on it.

func registerTwoVersions(t *testing.T, v1Cfg, v2Cfg entity.EntityConfig) (msg string, panicked bool) {
	t.Helper()
	defer func() {
		if r := recover(); r != nil {
			panicked = true
			if s, ok := r.(string); ok {
				msg = s
			} else if e, ok := r.(error); ok {
				msg = e.Error()
			}
		}
	}()
	app := NewApp(WithoutDefaultMiddleware())
	v1 := app.Group("/api/v1")
	v2 := app.Group("/api/v2")
	app.GroupEntity(v1, "posts", v1Cfg)
	app.GroupEntity(v2, "posts", v2Cfg)
	return "", false
}

func baseFields() []schema.Field {
	return []schema.Field{{Name: "title", Type: schema.String}}
}

// An index with no explicit Name is identified by its synthesised
// idx_<table>_<cols> name, so a divergence still has to be caught.
func TestVersionConflict_UnnamedIndexDiverges(t *testing.T) {
	msg, panicked := registerTwoVersions(t,
		entity.EntityConfig{Table: "posts", Fields: baseFields(),
			Indices: []entity.Index{{Columns: []string{"title"}}}}.WithTimestamps(false),
		entity.EntityConfig{Table: "posts", Fields: baseFields(),
			Indices: []entity.Index{{Columns: []string{"title"}, Unique: true}}}.WithTimestamps(false),
	)
	if !panicked {
		t.Fatal("unique/non-unique divergence on a synthesised index name was accepted")
	}
	if !strings.Contains(msg, "title") {
		t.Errorf("error should name the index or its columns: %s", msg)
	}
}

func TestVersionConflict_IndexColumnsAndOrderDiverge(t *testing.T) {
	for _, tc := range []struct {
		name   string
		v1, v2 entity.Index
	}{
		{"different columns",
			entity.Index{Name: "idx_a", Columns: []string{"title"}},
			entity.Index{Name: "idx_a", Columns: []string{"body"}}},
		{"different column ORDER",
			entity.Index{Name: "idx_a", Columns: []string{"title", "body"}},
			entity.Index{Name: "idx_a", Columns: []string{"body", "title"}}},
		{"different column count",
			entity.Index{Name: "idx_a", Columns: []string{"title"}},
			entity.Index{Name: "idx_a", Columns: []string{"title", "body"}}},
		{"different expression",
			entity.Index{Name: "idx_a", Expression: "lower(title)"},
			entity.Index{Name: "idx_a", Expression: "upper(title)"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fields := []schema.Field{
				{Name: "title", Type: schema.String},
				{Name: "body", Type: schema.String},
			}
			_, panicked := registerTwoVersions(t,
				entity.EntityConfig{Table: "posts", Fields: fields,
					Indices: []entity.Index{tc.v1}}.WithTimestamps(false),
				entity.EntityConfig{Table: "posts", Fields: fields,
					Indices: []entity.Index{tc.v2}}.WithTimestamps(false),
			)
			if !panicked {
				t.Errorf("%s was accepted — the emitted index would depend on registration order", tc.name)
			}
		})
	}
}

// An identical index in both versions is not a conflict — the guard must not
// reject the ordinary case of two versions declaring the same schema.
func TestVersionConflict_IdenticalIndexAccepted(t *testing.T) {
	idx := entity.Index{Name: "idx_a", Columns: []string{"title"}, Unique: true}
	if _, panicked := registerTwoVersions(t,
		entity.EntityConfig{Table: "posts", Fields: baseFields(),
			Indices: []entity.Index{idx}}.WithTimestamps(false),
		entity.EntityConfig{Table: "posts", Fields: baseFields(),
			Indices: []entity.Index{idx}}.WithTimestamps(false),
	); panicked {
		t.Error("identical index declarations were rejected")
	}
}

// A foreign-key column pointing at two different targets emits one REFERENCES
// clause while the other version's runtime treats it as the other entity.
func TestVersionConflict_RelationTargetAndTypeDiverge(t *testing.T) {
	fk := []schema.Field{
		{Name: "title", Type: schema.String},
		{Name: "owner_id", Type: schema.Int},
	}
	for _, tc := range []struct {
		name   string
		v1, v2 entity.Relation
	}{
		{"different target entity",
			entity.BelongsTo("owner", "users", "owner_id"),
			entity.BelongsTo("owner", "teams", "owner_id")},
		{"different relation type",
			entity.BelongsTo("owner", "users", "owner_id"),
			entity.HasMany("owner", "users", "owner_id")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, panicked := registerTwoVersions(t,
				entity.EntityConfig{Table: "posts", Fields: fk,
					Relations: []entity.Relation{tc.v1}}.WithTimestamps(false),
				entity.EntityConfig{Table: "posts", Fields: fk,
					Relations: []entity.Relation{tc.v2}}.WithTimestamps(false),
			)
			if !panicked {
				t.Errorf("%s was accepted on a shared FK column", tc.name)
			}
		})
	}
}

// AutoGenerate reaches the column default, so a mismatch changes DDL.
func TestVersionConflict_AutoGenerateDiverges(t *testing.T) {
	for _, tc := range []struct {
		name   string
		a, b   schema.AutoGenerate
		fldTyp schema.FieldType
	}{
		{"none vs uuid", schema.AutoNone, schema.AutoUUID, schema.String},
		{"none vs timestamp", schema.AutoNone, schema.AutoTimestamp, schema.Timestamp},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, panicked := registerTwoVersions(t,
				entity.EntityConfig{Table: "posts", Fields: []schema.Field{
					{Name: "marker", Type: tc.fldTyp, AutoGenerate: tc.a},
				}}.WithTimestamps(false),
				entity.EntityConfig{Table: "posts", Fields: []schema.Field{
					{Name: "marker", Type: tc.fldTyp, AutoGenerate: tc.b},
				}}.WithTimestamps(false),
			)
			if !panicked {
				t.Errorf("%s was accepted — the column default would depend on registration order", tc.name)
			}
		})
	}
}

// GetVersioned / CrudHandlerForEntity are the accessors that exist BECAUSE
// Get is ambiguous under multiple versions. They are how a caller addresses
// one specific version, so their error paths matter as much as their happy
// paths — a caller that cannot distinguish "no such entity" from "no such
// version" will paper over a config mistake.
func TestRegistry_GetVersionedAndHandlerLookup(t *testing.T) {
	app := NewApp(WithoutDefaultMiddleware())
	v1 := app.Group("/api/v1")
	v2 := app.Group("/api/v2")
	cfg := entity.EntityConfig{Table: "posts", Fields: baseFields()}.WithTimestamps(false)
	app.GroupEntity(v1, "posts", cfg)
	app.GroupEntity(v2, "posts", cfg)

	for _, ver := range []string{"/api/v1", "/api/v2"} {
		got, err := app.Registry.GetVersioned("posts", ver)
		if err != nil {
			t.Fatalf("GetVersioned(posts, %s): %v", ver, err)
		}
		if got.Version != ver {
			t.Errorf("got version %q, want %q", got.Version, ver)
		}
		// No DB is configured here, so this exercises the error path — the
		// point is that lookup is by ENTITY (which carries its version), not
		// by the now-ambiguous name.
		if _, err := app.CrudHandlerForEntity(got); err == nil {
			t.Errorf("expected the no-DB error for the %s entity", ver)
		}
	}

	// An unregistered version is not the same failure as an unknown name.
	if _, err := app.Registry.GetVersioned("posts", "/api/v9"); err == nil {
		t.Error("GetVersioned accepted an unregistered version")
	}
	if _, err := app.Registry.GetVersioned("posts", ""); err == nil {
		t.Error("GetVersioned returned an unversioned entity that was never registered")
	}
	if _, err := app.Registry.GetVersioned("nope", "/api/v1"); err == nil {
		t.Error("GetVersioned accepted an unknown entity name")
	}

	// Get stays ambiguous — picking one silently is the trap the split avoids.
	if _, err := app.Registry.Get("posts"); err == nil {
		t.Error("Get silently resolved an ambiguous name")
	}

	// All() yields one deterministic representative; AllSorted() yields every
	// version. Anything migrating or exporting needs the latter.
	if n := len(app.Registry.All()); n != 1 {
		t.Errorf("All() returned %d entries for one name, want 1", n)
	}
	versions := 0
	for _, e := range app.Registry.AllSorted() {
		if e.Config.Name == "posts" {
			versions++
		}
	}
	if versions != 2 {
		t.Errorf("AllSorted() saw %d versions of posts, want 2", versions)
	}
}

// A column that is mandatory in one version and absent from the other cannot
// be satisfied through the older version's API, so registration refuses it.
// The escape hatch is a default or an auto-generated value.
func TestVersionConflict_MandatoryColumnExclusiveToOneVersion(t *testing.T) {
	_, panicked := registerTwoVersions(t,
		entity.EntityConfig{Table: "posts", Fields: baseFields()}.WithTimestamps(false),
		entity.EntityConfig{Table: "posts", Fields: []schema.Field{
			{Name: "title", Type: schema.String},
			{Name: "summary", Type: schema.String, Required: true},
		}}.WithTimestamps(false),
	)
	if !panicked {
		t.Error("a NOT NULL column only v2 declares was accepted — a complete, " +
			"valid v1 write would be rejected by the database")
	}

	// With an auto-generated value the older version can satisfy it, so the
	// same shape must be allowed.
	if _, panicked := registerTwoVersions(t,
		entity.EntityConfig{Table: "posts", Fields: baseFields()}.WithTimestamps(false),
		entity.EntityConfig{Table: "posts", Fields: []schema.Field{
			{Name: "title", Type: schema.String},
			{Name: "uid", Type: schema.UUID, Required: true, AutoGenerate: schema.AutoUUID},
		}}.WithTimestamps(false),
	); panicked {
		t.Error("an auto-generated required column was rejected; the database can supply it")
	}
}

// The remaining ways one shared column can diverge. Each reaches a distinct
// branch of the comparison and of the message builder, and every one of them
// changes the emitted DDL — so accepting any would make the live schema
// depend on which version the migrator happened to read first.
func TestVersionConflict_EveryColumnAttributeThatReachesDDL(t *testing.T) {
	f := func(fld schema.Field) entity.EntityConfig {
		return entity.EntityConfig{Table: "posts", Fields: []schema.Field{fld}}.WithTimestamps(false)
	}
	max100, max200 := 100.0, 200.0

	for _, tc := range []struct {
		name   string
		v1, v2 schema.Field
	}{
		{"type", schema.Field{Name: "c", Type: schema.String}, schema.Field{Name: "c", Type: schema.Int}},
		{"string max length", schema.Field{Name: "c", Type: schema.String, Max: &max100},
			schema.Field{Name: "c", Type: schema.String, Max: &max200}},
		{"uniqueness", schema.Field{Name: "c", Type: schema.String},
			schema.Field{Name: "c", Type: schema.String, Unique: true}},
		{"nullability", schema.Field{Name: "c", Type: schema.String},
			schema.Field{Name: "c", Type: schema.String, Required: true, Default: "x"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			msg, panicked := registerTwoVersions(t, f(tc.v1), f(tc.v2))
			if !panicked {
				t.Fatalf("%s divergence was accepted", tc.name)
			}
			if !strings.Contains(msg, "c") {
				t.Errorf("error should name the column: %s", msg)
			}
		})
	}

	// Wire-only differences are NOT schema conflicts: Hidden, WireName and
	// ReadOnly never reach DDL, so two versions may legitimately disagree.
	for _, tc := range []struct {
		name string
		v2   schema.Field
	}{
		{"Hidden", schema.Field{Name: "c", Type: schema.String, Hidden: true}},
		{"WireName", schema.Field{Name: "c", Type: schema.String, WireName: "alias"}},
		{"ReadOnly", schema.Field{Name: "c", Type: schema.String, ReadOnly: true}},
	} {
		t.Run("wire-only/"+tc.name, func(t *testing.T) {
			if _, panicked := registerTwoVersions(t,
				f(schema.Field{Name: "c", Type: schema.String}), f(tc.v2)); panicked {
				t.Errorf("%s is a wire concern and must not be treated as a schema conflict", tc.name)
			}
		})
	}
}

// An index declared by only one version is additive, not a conflict — the
// same rule that lets one version add a column.
func TestVersionConflict_IndexOnlyInOneVersionIsAdditive(t *testing.T) {
	if _, panicked := registerTwoVersions(t,
		entity.EntityConfig{Table: "posts", Fields: baseFields()}.WithTimestamps(false),
		entity.EntityConfig{Table: "posts", Fields: baseFields(),
			Indices: []entity.Index{{Name: "idx_only_v2", Columns: []string{"title"}}}}.WithTimestamps(false),
	); panicked {
		t.Error("an index only v2 declares was rejected; additive index creation is allowed")
	}
}
