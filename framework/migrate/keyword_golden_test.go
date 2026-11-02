package migrate_test

import (
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/migrate"
)

var updateGolden = flag.Bool("update-keyword-golden", false, "rewrite testdata/nonkeyword_*.golden")

// goldenPlan is a schema with no keyword names: plain, unique, composite
// and qualified-free tables, a belongs_to with ON DELETE, a pivot, declared
// and automatic indexes, defaults and a composite key.
func goldenPlan() migrate.Plan {
	minLen := 120.0
	author := entity.Define("author", entity.EntityConfig{
		Table: "authors",
		Fields: []schema.Field{
			{Name: "id", Type: schema.String, Required: true},
			{Name: "email", Type: schema.String, Required: true, Unique: true, Max: &minLen},
			{Name: "status", Type: schema.Enum, Default: "active"},
			{Name: "score", Type: schema.Int, Default: 0},
		},
		PrimaryKey: []string{"id"},
		Indices:    []entity.Index{{Columns: []string{"status", "score"}}, {Name: "ux_authors_lower_email", Expression: "lower(email)", Unique: true}},
		Relations:  []entity.Relation{entity.ManyToMany("tags", "tag", "author_tags", "author_id", "tag_id")},
	}.WithTimestamps(true))
	tag := entity.Define("tag", entity.EntityConfig{
		Table:      "tags",
		Fields:     []schema.Field{{Name: "id", Type: schema.String, Required: true}, {Name: "label", Type: schema.String}},
		PrimaryKey: []string{"id"},
	}.WithTimestamps(false))
	post := entity.Define("post", entity.EntityConfig{
		Table: "posts",
		Fields: []schema.Field{
			{Name: "id", Type: schema.Int, AutoGenerate: schema.AutoIncrement},
			{Name: "author_id", Type: schema.String, Required: true},
			{Name: "title", Type: schema.String, Required: true},
			{Name: "body", Type: schema.Text},
			{Name: "meta", Type: schema.JSON},
		},
		PrimaryKey: []string{"id"},
		Relations:  []entity.Relation{withOnDelete(entity.BelongsTo("author", "author", "author_id"), entity.OnDeleteCascade)},
		Scope:      &entity.ScopeConfig{SoftDelete: true},
	}.WithTimestamps(true))
	line := entity.Define("line", entity.EntityConfig{
		Table: "lines",
		Fields: []schema.Field{
			{Name: "post_id", Type: schema.Int, Required: true},
			{Name: "seq", Type: schema.Int, Required: true},
			{Name: "text", Type: schema.String},
			{Name: "legacy", Type: schema.String},
		},
		PrimaryKey: []string{"post_id", "seq"},
	}.WithTimestamps(false))
	return migrate.Plan{Registry: fkReg{author, tag, post, line}}
}

// goldenPlanChanged is goldenPlan with columns added and renamed, a Unique
// set and an index added on tags, and on lines, which nothing references
// (a SQLite rebuild of a referenced table is refused), a column retyped
// and one dropped.
func goldenPlanChanged() migrate.Plan {
	p := goldenPlan()
	reg := p.Registry.(fkReg)
	tag := entity.Define("tag", entity.EntityConfig{
		Table: "tags",
		Fields: []schema.Field{
			{Name: "id", Type: schema.String, Required: true},
			{Name: "caption", Type: schema.String, Unique: true},
			{Name: "weight", Type: schema.Int},
			{Name: "hits", Type: schema.Int, Default: 1},
		},
		PrimaryKey: []string{"id"},
		Renames:    map[string]string{"label": "caption"},
		Indices:    []entity.Index{{Columns: []string{"weight"}}},
	}.WithTimestamps(false))
	line := entity.Define("line", entity.EntityConfig{
		Table: "lines",
		Fields: []schema.Field{
			{Name: "post_id", Type: schema.Int, Required: true},
			{Name: "seq", Type: schema.Int, Required: true},
			{Name: "text", Type: schema.Int},
		},
		PrimaryKey: []string{"post_id", "seq"},
	}.WithTimestamps(false))
	out := fkReg{}
	for _, e := range reg {
		switch e.GetName() {
		case "tag":
			e = tag
		case "line":
			e = line
		}
		out = append(out, e)
	}
	return migrate.Plan{Registry: out}
}

func withOnDelete(r entity.Relation, a entity.OnDeleteAction) entity.Relation {
	r.OnDelete = a
	return r
}

// Quoting keyword names must leave every other name's DDL as it was: the
// snapshot and the generated SQL of a schema with no keyword names are
// byte-identical to what the release before keyword quoting wrote, so no
// existing app sees its indexes or constraints diff.
func TestNonKeywordSchemaRendersUnchanged(t *testing.T) {
	for _, d := range []migrate.Dialect{migrate.DialectSQLite, migrate.DialectPostgres} {
		up, _, snap, err := migrate.GeneratePlan(goldenPlan(), migrate.SchemaSnapshot{}, d)
		if err != nil {
			t.Fatal(err)
		}
		up2, down2, _, err := migrate.GeneratePlan(goldenPlanChanged(), snap, d)
		if err != nil {
			t.Fatal(err)
		}
		snapJSON, _ := json.MarshalIndent(snap, "", "  ")
		got := up + "\n---\n" + string(snapJSON) + "\n---\n" + up2 + "\n---\n" + down2 + "\n"
		path := filepath.Join("testdata", "nonkeyword_"+string(d)+".golden")
		if *updateGolden {
			if err := os.MkdirAll("testdata", 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
				t.Fatal(err)
			}
			continue
		}
		want, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if got != string(want) {
			t.Errorf("%s: the non-keyword schema renders differently from %s", d, path)
		}
	}
}
