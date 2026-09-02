package main

import (
	"go/parser"
	"go/token"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/kiln/freeze"
	"github.com/DonaldMurillo/gofastr/kiln/world"
)

// Property family: the generate-time seed verdict must not disagree with
// the boot validator. A seed row that core/schema rejects on CreateOne
// makes the SHIPPED app abort startup (generated WithSeed runs rows
// through the CRUD handler and errors return to Start — pinned by
// blueprint_seed_failfast_test.go), so a row that previews and generates
// green is a time bomb on every fresh database.
//
// seedValueRejection's own docstring (blueprint.go:2456-2461) states the
// contract: "Mirrors the per-type validators in core/schema/validate.go,
// including their coercions ... so the generate-time verdict never
// disagrees with boot." The mirror handles type coercions and enum
// closure only. For pattern and min/max it returns "" (accept), while
// core/schema validateString (validate.go:69-87) enforces exactly those —
// the acknowledged nil carve-out at blueprint.go:2463-2465 covers
// required-ness only; pattern and bounds silence has no such carve-out.
//
// The kiln preview widens the gap (render.insertSeed is raw SQL, no
// validator at all), but the pin here is the narrower, self-declared
// generator contract: whatever the preview did, generate must not accept
// a row the boot engine rejects.
//
// The chain runs the real legs: a kiln-authored world (freeze emits
// pattern/min on fields, blueprint.go:202-212) -> current decoder ->
// current validator, against the boot oracle
// (framework.FieldDeclaration.Field -> schema.Validate) on the same
// declaration the generator holds.
func TestSeedVerdictMatchesBootValidator(t *testing.T) {
	three := 3.0
	cases := []struct {
		name  string
		field world.Field
		row   map[string]any
	}{
		{
			name:  "string violating pattern",
			field: world.Field{Name: "code", Type: "string", Pattern: "^[A-Z]{3}$"},
			row:   map[string]any{"code": "abc"},
		},
		{
			name:  "string under min length",
			field: world.Field{Name: "title", Type: "string", Required: true, Min: &three},
			row:   map[string]any{"title": "ab"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := world.New()
			w.App = world.AppConfig{Name: "forge", Module: "example.com/forge", DBDriver: "sqlite", DBURL: "forge.db"}
			w.Entities["tasks"] = &world.Entity{
				Name:   "tasks",
				Fields: []world.Field{tc.field},
			}
			w.Seeds = []*world.Seed{{Entity: "tasks", Rows: []map[string]any{tc.row}}}

			buf, err := freeze.BlueprintYAML(w)
			if err != nil {
				t.Fatalf("BlueprintYAML: %v", err)
			}
			bp, err := decodeBlueprintString(string(buf))
			if err != nil {
				t.Fatalf("decodeBlueprintString rejected freeze output: %v\n%s", err, buf)
			}

			// Boot oracle: the same declaration, converted the way the
			// framework converts it, judged by the same validator the
			// shipped app runs on CreateOne.
			if len(bp.Entities) != 1 || len(bp.Entities[0].Fields) != 1 {
				t.Fatalf("fixture lost its field during freeze/decode: %+v", bp.Entities)
			}
			decl := bp.Entities[0].Fields[0]
			bootField, err := decl.Field()
			if err != nil {
				t.Fatalf("FieldDeclaration.Field(): %v", err)
			}
			var value any
			for _, v := range tc.row {
				value = v
			}
			bootErr := schema.Validate(bootField, value)
			if bootErr == nil {
				t.Fatalf("fixture invalid: boot validator accepted the row (%+v); pick a row it rejects", tc.row)
			}

			// Generate verdict on the identical blueprint: must disagree
			// with boot in the ACCEPTING direction to be a defect.
			genErr := validateBlueprint(bp)
			if genErr == nil {
				t.Errorf("validateBlueprint accepted a seed row the boot validator rejects "+
					"(boot says: %v): the frozen blueprint generates cleanly, compiles, and the shipped app "+
					"aborts startup on a fresh database — saw-green-ships-red across the seam the "+
					"seedValueRejection docstring claims \"never disagrees with boot\"\n%s",
					bootErr, buf)
			}
		})
	}
}

// seedBp builds a minimal valid blueprint with the given entity fields and
// seed blocks, sharing the shape the sibling tests above use.
func seedBp(fields []framework.FieldDeclaration, seeds ...BlueprintSeedEntity) Blueprint {
	return Blueprint{
		App: BlueprintApp{Name: "SeedApp", Module: "example.com/seedapp", DBDriver: "sqlite", DBURL: "file:seed.db"},
		Entities: []framework.EntityDeclaration{{
			Name:   "posts",
			Fields: fields,
		}},
		Seed: seeds,
	}
}

// Property: every seed row value the validator ACCEPTS must reach seedData()
// verbatim — the emitter may not silently null it.
//
// validateBlueprintSeedTypes accepts any YAML shape on a json field ("Any
// value marshals; the validator accepts strings, maps, lists"), and the boot
// validator agrees: a map or nested list marshals and inserts. But
// renderBlueprintStubs' value switch has no map case, so a nested object or a
// list-inside-a-list falls to `default:` and is emitted as

//	{"meta": nil // unsupported type map[string]interface {}}

// The row generates green, compiles, and the shipped app seeds NULL where the
// author wrote data (or aborts when the column is required) — the same
// saw-green-ships-red seam the file pins above, on the emitter leg instead of
// the validator leg. Surfaces: object on a json column, list-in-list,
// object-in-list.
func TestSeedComplexValueEmittedNotNulled(t *testing.T) {
	cases := []struct {
		name  string
		value any
		// datum that must survive into the emitted Go somewhere under the key
		datum string
	}{
		{"object on json column", map[string]any{"a": int64(1)}, `"a":`},
		{"list inside list", []any{[]any{"x"}}, `"x"`},
		{"object inside list", []any{map[string]any{"b": int64(2)}}, `"b":`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			bp := seedBp(
				[]framework.FieldDeclaration{
					{Name: "title", Type: "string", Required: true},
					{Name: "meta", Type: "json"},
				},
				BlueprintSeedEntity{Entity: "posts", Rows: []map[string]any{{
					"title": "T", "meta": tc.value,
				}}},
			)
			if err := validateBlueprint(bp); err != nil {
				t.Fatalf("fixture must validate (the json case is documented as accepted): %v", err)
			}
			stubs := renderBlueprintStubs(bp)
			i := strings.Index(stubs, `"meta":`)
			if i < 0 {
				t.Fatalf("seed row lost the meta key entirely:\n%s", stubs)
			}
			rest := stubs[i:]
			if strings.HasPrefix(rest, `"meta": nil`) {
				t.Fatalf("SECURITY: [seed] emitted value was silently nulled by the stub emitter; the author's data never reaches the shipped app and nothing failed:\n%s", stubs)
			}
			if !strings.Contains(stubs, tc.datum) {
				t.Fatalf("emitted row does not carry the seeded datum %s:\n%s", tc.datum, stubs)
			}
			// Whatever the emitter wrote must at least be Go.
			fset := token.NewFileSet()
			if _, err := parser.ParseFile(fset, "stubs.go", stubs, parser.AllErrors); err != nil {
				t.Fatalf("emitted stubs.go does not parse: %v\n%s", err, stubs)
			}
		})
	}
}

// Property: a seed count/weight that is not an integer scalar must be an
// error, not a silent zero.
//
// intValue, the lax decoder, maps a string ("5"), a fractional float (3.7),
// or a list to 0. Zero is legal ("must be >= 0"), so `count: "5"` silently
// generates no rows at all: the author asked for five demo rows and got none,
// with no error anywhere. Same coercion on a weight silently zeroes it. This
// is the count/weights sibling of the protectiveBool finding in
// blueprint_failclosed_security_test.go: a lax decoder guessing a value the
// author demonstrably tried to set.
func TestSeedNumericScalarsMustBeIntegers(t *testing.T) {
	cases := []struct {
		name string
		yaml string
	}{
		{"count as string", "seed:\n  - entity: posts\n    count: \"5\"\n"},
		{"count as list", "seed:\n  - entity: posts\n    count: [3]\n"},
		{"count fractional", "seed:\n  - entity: posts\n    count: 3.7\n"},
		{"weight as string", "seed:\n  - entity: posts\n    weights:\n      status:\n        open: \"3\"\n"},
		{"weight as map", "seed:\n  - entity: posts\n    weights:\n      status:\n        open: {a: 1}\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := decodeBlueprintString("entities:\n  - name: posts\n    fields:\n      - name: title\n" + tc.yaml)
			if err == nil {
				t.Fatalf("SECURITY: [seed] decoder accepted a non-integer numeric scalar and will silently coerce it to zero rows/weight:\n%s", tc.yaml)
			}
		})
	}
	// Control: real integers still decode (guards against a fix that rejects
	// every count).
	if _, err := decodeBlueprintString("entities:\n  - name: posts\n    fields:\n      - name: title\nseed:\n  - entity: posts\n    count: 5\n    weights:\n      status:\n        open: 3\n"); err != nil {
		t.Fatalf("integer count/weights must still decode: %v", err)
	}
}

// Property: a seed block's rows must all run, so two blocks for one entity
// must be refused.
//
// The generated seed hook gates each block on CountAll(entity) > 0. Two seed
// blocks naming the same entity (one file, or a directory merge) therefore
// insert the first block's rows and silently skip the second: no error, no
// warning, data the author wrote never lands. validateBlueprint rejects
// duplicate entities, duplicate screen routes and duplicate endpoint names —
// the duplicate seed entity is the missing sibling of that set.
func TestSeedRefusesDuplicateEntityBlocks(t *testing.T) {
	bp := Blueprint{
		App: BlueprintApp{Name: "DupSeed", Module: "example.com/dup", DBDriver: "sqlite", DBURL: "file:dup.db"},
		Entities: []framework.EntityDeclaration{{
			Name:   "posts",
			Fields: []framework.FieldDeclaration{{Name: "title", Type: "string", Required: true}},
		}},
		Seed: []BlueprintSeedEntity{
			{Entity: "posts", Rows: []map[string]any{{"title": "first"}}},
			{Entity: "posts", Rows: []map[string]any{{"title": "silently dropped"}}},
		},
	}
	err := validateBlueprint(bp)
	if err == nil || !strings.Contains(err.Error(), "posts") {
		t.Fatalf("SECURITY: [seed] duplicate seed blocks for one entity accepted (second block silently skipped by the CountAll gate): err=%v", err)
	}
	// Control: two DIFFERENT entities in two blocks is the normal shape.
	bp.Seed[1] = BlueprintSeedEntity{Entity: "other"}
	bp.Entities = append(bp.Entities, framework.EntityDeclaration{
		Name:   "other",
		Fields: []framework.FieldDeclaration{{Name: "label", Type: "string", Required: true}},
	})
	bp.Seed[1].Rows = []map[string]any{{"label": "x"}}
	if err := validateBlueprint(bp); err != nil {
		t.Fatalf("distinct-entity seed blocks must validate: %v", err)
	}
	// Second surface: the directory merge. loadBlueprintPath over a
	// directory concatenates every file's seed blocks (mergeBlueprints)
	// before validating, so the same duplicate also arrives as two FILES —
	// the natural shape when an agent or a team splits a blueprint. The
	// merged blueprint must hit the same refusal.
	dir := t.TempDir()
	writeTestFile(t, dir+"/a.yml", "app:\n  name: M\n  module: example.com/m\n  db:\n    driver: sqlite\n    url: file:m.db\nentities:\n  - name: posts\n    fields:\n      - name: title\n        type: string\n        required: true\nseed:\n  - entity: posts\n    rows:\n      - title: from file a\n")
	writeTestFile(t, dir+"/b.yml", "seed:\n  - entity: posts\n    rows:\n      - title: from file b\n")
	if merged, merr := loadBlueprint(dir); merr == nil {
		t.Fatalf("SECURITY: [seed] directory merge accepted duplicate seed blocks for posts: %+v", merged.Seed)
	}
}

// Property: a seed reference "@entity.field=value" whose target is a
// blueprint entity must name an entity seeded earlier (or in the same block).
//
// resolveSeedRefs runs at boot and leaves unresolvable refs as-is "so the
// create fails loudly" — and it does: on a relation column the literal
// "@posts.title=First" fails UUID validation and the shipped app aborts
// startup. The generator knows the declaration order (stubs.go even documents
// "blueprint-declared order (so entities that reference others are inserted
// after them)") and the numeric-FK sibling check above already instructs the
// author to "reference a row seeded earlier in the same pass" — but nothing
// verifies the author did. A ref to a later block or to a never-seeded
// blueprint entity generates green and kills every fresh database at boot.
func TestSeedRefTargetsSeededEarlier(t *testing.T) {
	const refEntity = `entities:
  - name: posts
    fields:
      - name: title
  - name: comments
    fields:
      - name: body
      - name: post_id
        type: relation
        to: posts
seed:
`
	cases := []struct {
		name    string
		seed    string
		wantErr bool
	}{
		{
			name:    "target seeded in a later block",
			seed:    "  - entity: comments\n    rows:\n      - body: hi\n        post_id: \"@posts.title=First\"\n  - entity: posts\n    rows:\n      - title: First\n",
			wantErr: true,
		},
		{
			name:    "target never seeded",
			seed:    "  - entity: comments\n    rows:\n      - body: hi\n        post_id: \"@posts.title=First\"\n",
			wantErr: true,
		},
		{
			// Control: posts first is exactly the documented shape.
			name:    "target seeded in an earlier block",
			seed:    "  - entity: posts\n    rows:\n      - title: First\n  - entity: comments\n    rows:\n      - body: hi\n        post_id: \"@posts.title=First\"\n",
			wantErr: false,
		},
		{
			// Control: a non-blueprint entity (registered by app code) is
			// outside the generator's knowledge and must stay legal.
			name:    "target outside the blueprint",
			seed:    "  - entity: comments\n    rows:\n      - body: hi\n        post_id: \"@external.id=x\"\n",
			wantErr: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			bp, err := decodeBlueprintString(refEntity + tc.seed)
			if err != nil {
				t.Fatalf("decode: %v", err)
			}
			bp.App = BlueprintApp{Name: "RefOrder", Module: "example.com/ref", DBDriver: "sqlite", DBURL: "file:ref.db"}
			verr := validateBlueprint(bp)
			if tc.wantErr && (verr == nil || !strings.Contains(verr.Error(), "posts")) {
				t.Fatalf("SECURITY: [seed] forward seed reference accepted; the shipped app aborts at boot on a fresh DB (resolveSeedRefs leaves it unresolved and UUID validation fails): err=%v", verr)
			}
			if !tc.wantErr && verr != nil {
				t.Fatalf("legitimate shape must still validate: %v", verr)
			}
		})
	}
}

// Property: a blueprint that seeds data must declare a database, or generate
// must refuse.
//
// With no app.db the generated openDB returns (nil, nil) — a legal DB-less
// app — and the seed hook then calls fwApp.CrudHandler, which returns "no DB
// configured". WithSeed errors abort App.Start, so the app dies on every
// boot while generate reported success. The "seed targets unknown entity"
// check above exists for exactly this fail-late shape; a seed with no
// database to seed into is the same class one step earlier.
func TestSeedRequiresDatabase(t *testing.T) {
	// A seeded blueprint with no app.db is NOT a database-less app: the
	// generator defaults any entity-bearing blueprint to SQLite
	// (renderBlueprintMain: driver "sqlite", DSN "file:gofastr.db"),
	// the contract blueprints.md documents for an empty driver. So the
	// probe pins that default end to end: validation passes, and the
	// emitted openDB carries the SQLite fallback the seed hook runs on.
	// The only route to a DB-less app is an explicit driver: none.
	bp := Blueprint{
		App:      BlueprintApp{Name: "NoDB", Module: "example.com/nodb"},
		Entities: []framework.EntityDeclaration{{Name: "posts", Fields: []framework.FieldDeclaration{{Name: "title", Type: "string", Required: true}}}},
		Seed:     []BlueprintSeedEntity{{Entity: "posts", Rows: []map[string]any{{"title": "x"}}}},
	}
	if err := validateBlueprint(bp); err != nil {
		t.Fatalf("seeded blueprint without app.db must validate under the SQLite default: %v", err)
	}
	files, err := renderBlueprintFiles(bp)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	var mainGo string
	for _, f := range files {
		if f.name == "main.go" {
			mainGo = f.content
		}
	}
	if !strings.Contains(mainGo, `getEnv("DB_DRIVER", "sqlite")`) || !strings.Contains(mainGo, `"file:gofastr.db"`) {
		t.Fatalf("emitted main.go does not carry the SQLite default the seed hook depends on:\n%s", mainGo)
	}
	// Control: an explicit db is honoured verbatim.
	bp.App.DBDriver, bp.App.DBURL = "sqlite", "file:x.db"
	if err := validateBlueprint(bp); err != nil {
		t.Fatalf("seeded blueprint with a db must validate: %v", err)
	}
}

// Property: count-seeding an entity whose required fields cannot be
// fabricated must be refused at generate.
//
// blueprintGenerateSeedRows skips relations ("Relations can't be safely
// fabricated, so count-seeding suits scalar/enum entities (use explicit
// rows:)"), so every generated row is missing the required relation. Each row
// then fails CreateOne's required check and — per the fail-fast contract this
// family pins — aborts startup. The doc comment tells the author to use
// explicit rows; nothing enforces it before the app ships.
func TestCountSeedRefusesUnfillableRequired(t *testing.T) {
	bp := Blueprint{
		App: BlueprintApp{Name: "CntRel", Module: "example.com/cnt", DBDriver: "sqlite", DBURL: "file:cnt.db"},
		Entities: []framework.EntityDeclaration{
			{Name: "posts", Fields: []framework.FieldDeclaration{{Name: "title", Type: "string"}}},
			{Name: "comments", Fields: []framework.FieldDeclaration{
				{Name: "body", Type: "string"},
				{Name: "post_id", Type: "relation", To: "posts", Required: true},
			}},
		},
		Seed: []BlueprintSeedEntity{{Entity: "comments", Count: 3}},
	}
	err := validateBlueprint(bp)
	if err == nil {
		expanded := blueprintExpandSeed(bp)
		t.Fatalf("SECURITY: [seed] count-seed on an entity with a required relation accepted; every expanded row lacks post_id and the shipped app aborts at boot. Expanded rows: %+v", expanded.Seed[0].Rows)
	}
	// Control: count on the scalar entity is the documented use.
	bp.Seed = []BlueprintSeedEntity{{Entity: "posts", Count: 3}}
	if err := validateBlueprint(bp); err != nil {
		t.Fatalf("scalar count-seed must validate: %v", err)
	}
}

// Property: a seed row key that matches no declared column must be refused,
// not silently dropped.
//
// blueprintSeedFieldKind returns false for an unknown key and
// validateBlueprintSeedTypes skips it ("not a declared column: not this
// check's business" — the TYPE mirror's carve-out, respected here: the pin
// is on validateBlueprint, which has no such carve-out). The generated seed
// hands the row to CreateOne, which writes only declared columns: a typo'd
// key ("titel") silently drops the value, and when the real column is
// required the app aborts at boot with an error naming the wrong key. Every
// other transcribed name in the blueprint (entity, field, screen, relation)
// gets an unknown-target check; the seed row key is the one name that
// doesn't.
func TestSeedRowUnknownColumnRefused(t *testing.T) {
	bp := seedBp(
		[]framework.FieldDeclaration{{Name: "title", Type: "string"}},
		BlueprintSeedEntity{Entity: "posts", Rows: []map[string]any{{"titel": "Typo"}}},
	)
	err := validateBlueprint(bp)
	if err == nil || !strings.Contains(err.Error(), "titel") {
		t.Fatalf("SECURITY: [seed] row key matching no declared column accepted; the value is silently dropped at boot and a required column fails naming a key the author never wrote: err=%v", err)
	}
	// Control: declared column and the synthesized id are both legal
	// (blueprintSeedFieldKind documents id as "legal but takes a UUID
	// string").
	bp.Seed[0].Rows = []map[string]any{{"title": "ok"}}
	if err := validateBlueprint(bp); err != nil {
		t.Fatalf("declared-column row must validate: %v", err)
	}
}
