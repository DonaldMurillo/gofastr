package main

import (
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
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
