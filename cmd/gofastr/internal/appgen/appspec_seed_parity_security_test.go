package appgen

import (
	"go/parser"
	"go/token"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/appspec"
	"github.com/DonaldMurillo/gofastr/framework"
)

// seedBp builds a minimal valid app spec with the given entity fields and
// seed blocks, sharing the shape the sibling tests above use.
func seedBp(fields []framework.FieldDeclaration, seeds ...appspec.AppSpecSeedEntity) appspec.AppSpec {
	return appspec.AppSpec{
		App: appspec.AppSpecApp{Name: "SeedApp", Module: "example.com/seedapp", DBDriver: "sqlite", DBURL: "file:seed.db"},
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
// validateAppSpecSeedTypes accepts any YAML shape on a json field ("Any
// value marshals; the validator accepts strings, maps, lists"), and the boot
// validator agrees: a map or nested list marshals and inserts. But
// renderAppSpecStubs' value switch has no map case, so a nested object or a
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
				appspec.AppSpecSeedEntity{Entity: "posts", Rows: []map[string]any{{
					"title": "T", "meta": tc.value,
				}}},
			)
			if err := appspec.ValidateAppSpec(bp); err != nil {
				t.Fatalf("fixture must validate (the json case is documented as accepted): %v", err)
			}
			stubs := RenderAppSpecStubs(bp)
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

// Property: an app spec that seeds data must declare a database, or generate
// must refuse.
//
// With no app.db the generated openDB returns (nil, nil) — a legal DB-less
// app — and the seed hook then calls fwApp.CrudHandler, which returns "no DB
// configured". WithSeed errors abort App.Start, so the app dies on every
// boot while generate reported success. The "seed targets unknown entity"
// check above exists for exactly this fail-late shape; a seed with no
// database to seed into is the same class one step earlier.
func TestSeedRequiresDatabase(t *testing.T) {
	// A seeded app spec with no app.db is NOT a database-less app: the
	// generator defaults any entity-bearing app spec to SQLite
	// (renderAppSpecMain: driver "sqlite", DSN "file:gofastr.db"),
	// the contract app-spec.md documents for an empty driver. So the
	// probe pins that default end to end: validation passes, and the
	// emitted openDB carries the SQLite fallback the seed hook runs on.
	// The only route to a DB-less app is an explicit driver: none.
	bp := appspec.AppSpec{
		App:      appspec.AppSpecApp{Name: "NoDB", Module: "example.com/nodb"},
		Entities: []framework.EntityDeclaration{{Name: "posts", Fields: []framework.FieldDeclaration{{Name: "title", Type: "string", Required: true}}}},
		Seed:     []appspec.AppSpecSeedEntity{{Entity: "posts", Rows: []map[string]any{{"title": "x"}}}},
	}
	if err := appspec.ValidateAppSpec(bp); err != nil {
		t.Fatalf("seeded app spec without app.db must validate under the SQLite default: %v", err)
	}
	files, err := RenderAppSpecFiles(bp)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	var mainGo string
	for _, f := range files {
		if f.Name == "main.go" {
			mainGo = f.Content
		}
	}
	if !strings.Contains(mainGo, `getEnv("DB_DRIVER", "sqlite")`) || !strings.Contains(mainGo, `"file:gofastr.db"`) {
		t.Fatalf("emitted main.go does not carry the SQLite default the seed hook depends on:\n%s", mainGo)
	}
	// Control: an explicit db is honoured verbatim.
	bp.App.DBDriver, bp.App.DBURL = "sqlite", "file:x.db"
	if err := appspec.ValidateAppSpec(bp); err != nil {
		t.Fatalf("seeded app spec with a db must validate: %v", err)
	}
}

// Property: count-seeding an entity whose required fields cannot be
// fabricated must be refused at generate.
//
// appSpecGenerateSeedRows skips relations ("Relations can't be safely
// fabricated, so count-seeding suits scalar/enum entities (use explicit
// rows:)"), so every generated row is missing the required relation. Each row
// then fails CreateOne's required check and — per the fail-fast contract this
// family pins — aborts startup. The doc comment tells the author to use
// explicit rows; nothing enforces it before the app ships.
func TestCountSeedRefusesUnfillableRequired(t *testing.T) {
	bp := appspec.AppSpec{
		App: appspec.AppSpecApp{Name: "CntRel", Module: "example.com/cnt", DBDriver: "sqlite", DBURL: "file:cnt.db"},
		Entities: []framework.EntityDeclaration{
			{Name: "posts", Fields: []framework.FieldDeclaration{{Name: "title", Type: "string"}}},
			{Name: "comments", Fields: []framework.FieldDeclaration{
				{Name: "body", Type: "string"},
				{Name: "post_id", Type: "relation", To: "posts", Required: true},
			}},
		},
		Seed: []appspec.AppSpecSeedEntity{{Entity: "comments", Count: 3}},
	}
	err := appspec.ValidateAppSpec(bp)
	if err == nil {
		expanded := appSpecExpandSeed(bp)
		t.Fatalf("SECURITY: [seed] count-seed on an entity with a required relation accepted; every expanded row lacks post_id and the shipped app aborts at boot. Expanded rows: %+v", expanded.Seed[0].Rows)
	}
	// Control: count on the scalar entity is the documented use.
	bp.Seed = []appspec.AppSpecSeedEntity{{Entity: "posts", Count: 3}}
	if err := appspec.ValidateAppSpec(bp); err != nil {
		t.Fatalf("scalar count-seed must validate: %v", err)
	}
}
