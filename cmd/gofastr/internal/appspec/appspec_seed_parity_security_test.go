package appspec

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
	"github.com/DonaldMurillo/gofastr/framework"
)

// seedBp builds a minimal valid app spec with the given entity fields and
// seed blocks, sharing the shape the sibling tests above use.
func seedBp(fields []framework.FieldDeclaration, seeds ...AppSpecSeedEntity) AppSpec {
	return AppSpec{
		App: AppSpecApp{Name: "SeedApp", Module: "example.com/seedapp", DBDriver: "sqlite", DBURL: "file:seed.db"},
		Entities: []framework.EntityDeclaration{{
			Name:   "posts",
			Fields: fields,
		}},
		Seed: seeds,
	}
}

// Property: a seed block's rows must all run, so two blocks for one entity
// must be refused.
//
// The generated seed hook gates each block on CountAll(entity) > 0. Two seed
// blocks naming the same entity (one file, or a directory merge) therefore
// insert the first block's rows and silently skip the second: no error, no
// warning, data the author wrote never lands. validateAppSpec rejects
// duplicate entities, duplicate screen routes and duplicate endpoint names —
// the duplicate seed entity is the missing sibling of that set.
func TestSeedRefusesDuplicateEntityBlocks(t *testing.T) {
	bp := AppSpec{
		App: AppSpecApp{Name: "DupSeed", Module: "example.com/dup", DBDriver: "sqlite", DBURL: "file:dup.db"},
		Entities: []framework.EntityDeclaration{{
			Name:   "posts",
			Fields: []framework.FieldDeclaration{{Name: "title", Type: "string", Required: true}},
		}},
		Seed: []AppSpecSeedEntity{
			{Entity: "posts", Rows: []map[string]any{{"title": "first"}}},
			{Entity: "posts", Rows: []map[string]any{{"title": "silently dropped"}}},
		},
	}
	err := ValidateAppSpec(bp)
	if err == nil || !strings.Contains(err.Error(), "posts") {
		t.Fatalf("SECURITY: [seed] duplicate seed blocks for one entity accepted (second block silently skipped by the CountAll gate): err=%v", err)
	}
	// Control: two DIFFERENT entities in two blocks is the normal shape.
	bp.Seed[1] = AppSpecSeedEntity{Entity: "other"}
	bp.Entities = append(bp.Entities, framework.EntityDeclaration{
		Name:   "other",
		Fields: []framework.FieldDeclaration{{Name: "label", Type: "string", Required: true}},
	})
	bp.Seed[1].Rows = []map[string]any{{"label": "x"}}
	if err := ValidateAppSpec(bp); err != nil {
		t.Fatalf("distinct-entity seed blocks must validate: %v", err)
	}
	// Second surface: the directory merge. loadAppSpecPath over a
	// directory concatenates every file's seed blocks (mergeAppSpecs)
	// before validating, so the same duplicate also arrives as two FILES —
	// the natural shape when an agent or a team splits an app spec. The
	// merged app spec must hit the same refusal.
	dir := t.TempDir()
	clitest.WriteTestFile(t, dir+"/a.yml", "app:\n  name: M\n  module: example.com/m\n  db:\n    driver: sqlite\n    url: file:m.db\nentities:\n  - name: posts\n    fields:\n      - name: title\n        type: string\n        required: true\nseed:\n  - entity: posts\n    rows:\n      - title: from file a\n")
	clitest.WriteTestFile(t, dir+"/b.yml", "seed:\n  - entity: posts\n    rows:\n      - title: from file b\n")
	if merged, merr := LoadAppSpec(dir); merr == nil {
		t.Fatalf("SECURITY: [seed] directory merge accepted duplicate seed blocks for posts: %+v", merged.Seed)
	}
}

// Property: a seed row key that matches no declared column must be refused,
// not silently dropped.
//
// appSpecSeedFieldKind returns false for an unknown key and
// validateAppSpecSeedTypes skips it ("not a declared column: not this
// check's business" — the TYPE mirror's carve-out, respected here: the pin
// is on validateAppSpec, which has no such carve-out). The generated seed
// hands the row to CreateOne, which writes only declared columns: a typo'd
// key ("titel") silently drops the value, and when the real column is
// required the app aborts at boot with an error naming the wrong key. Every
// other transcribed name in the app spec (entity, field, screen, relation)
// gets an unknown-target check; the seed row key is the one name that
// doesn't.
func TestSeedRowUnknownColumnRefused(t *testing.T) {
	bp := seedBp(
		[]framework.FieldDeclaration{{Name: "title", Type: "string"}},
		AppSpecSeedEntity{Entity: "posts", Rows: []map[string]any{{"titel": "Typo"}}},
	)
	err := ValidateAppSpec(bp)
	if err == nil || !strings.Contains(err.Error(), "titel") {
		t.Fatalf("SECURITY: [seed] row key matching no declared column accepted; the value is silently dropped at boot and a required column fails naming a key the author never wrote: err=%v", err)
	}
	// Control: declared column and the synthesized id are both legal
	// (appSpecSeedFieldKind documents id as "legal but takes a UUID
	// string").
	bp.Seed[0].Rows = []map[string]any{{"title": "ok"}}
	if err := ValidateAppSpec(bp); err != nil {
		t.Fatalf("declared-column row must validate: %v", err)
	}
}
