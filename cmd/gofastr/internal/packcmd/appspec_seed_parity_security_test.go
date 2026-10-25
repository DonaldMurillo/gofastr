package packcmd

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/appspec"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/kiln/freeze"
	"github.com/DonaldMurillo/gofastr/kiln/world"
)

// Property family: the generate-time seed verdict must not disagree with
// the boot validator. A seed row that core/schema rejects on CreateOne
// makes the SHIPPED app abort startup (generated WithSeed runs rows
// through the CRUD handler and errors return to Start — pinned by
// appspec_seed_failfast_test.go), so a row that previews and generates
// green is a time bomb on every fresh database.
//
// seedValueRejection's own docstring (appspec.go:2456-2461) states the
// contract: "Mirrors the per-type validators in core/schema/validate.go,
// including their coercions ... so the generate-time verdict never
// disagrees with boot." The mirror handles type coercions and enum
// closure only. For pattern and min/max it returns "" (accept), while
// core/schema validateString (validate.go:69-87) enforces exactly those —
// the acknowledged nil carve-out at appspec.go:2463-2465 covers
// required-ness only; pattern and bounds silence has no such carve-out.
//
// The kiln preview widens the gap (render.insertSeed is raw SQL, no
// validator at all), but the pin here is the narrower, self-declared
// generator contract: whatever the preview did, generate must not accept
// a row the boot engine rejects.
//
// The chain runs the real legs: a kiln-authored world (freeze emits
// pattern/min on fields, appspec.go:202-212) -> current decoder ->
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

			buf, err := freeze.AppSpecYAML(w)
			if err != nil {
				t.Fatalf("AppSpecYAML: %v", err)
			}
			bp, err := decodeAppSpecString(string(buf))
			if err != nil {
				t.Fatalf("decodeAppSpecString rejected freeze output: %v\n%s", err, buf)
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

			// Generate verdict on the identical app spec: must disagree
			// with boot in the ACCEPTING direction to be a defect.
			genErr := appspec.ValidateAppSpec(bp)
			if genErr == nil {
				t.Errorf("validateAppSpec accepted a seed row the boot validator rejects "+
					"(boot says: %v): the frozen app spec generates cleanly, compiles, and the shipped app "+
					"aborts startup on a fresh database — saw-green-ships-red across the seam the "+
					"seedValueRejection docstring claims \"never disagrees with boot\"\n%s",
					bootErr, buf)
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
// appspec_failclosed_security_test.go: a lax decoder guessing a value the
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
			_, err := decodeAppSpecString("entities:\n  - name: posts\n    fields:\n      - name: title\n" + tc.yaml)
			if err == nil {
				t.Fatalf("SECURITY: [seed] decoder accepted a non-integer numeric scalar and will silently coerce it to zero rows/weight:\n%s", tc.yaml)
			}
		})
	}
	// Control: real integers still decode (guards against a fix that rejects
	// every count).
	if _, err := decodeAppSpecString("entities:\n  - name: posts\n    fields:\n      - name: title\nseed:\n  - entity: posts\n    count: 5\n    weights:\n      status:\n        open: 3\n"); err != nil {
		t.Fatalf("integer count/weights must still decode: %v", err)
	}
}

// Property: a seed reference "@entity.field=value" whose target is a
// app spec entity must name an entity seeded earlier (or in the same block).
//
// resolveSeedRefs runs at boot and leaves unresolvable refs as-is "so the
// create fails loudly" — and it does: on a relation column the literal
// "@posts.title=First" fails UUID validation and the shipped app aborts
// startup. The generator knows the declaration order (stubs.go even documents
// "declared order (so entities that reference others are inserted
// after them)") and the numeric-FK sibling check above already instructs the
// author to "reference a row seeded earlier in the same pass" — but nothing
// verifies the author did. A ref to a later block or to a never-seeded
// app spec entity generates green and kills every fresh database at boot.
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
			// Control: a non-app-spec entity (registered by app code) is
			// outside the generator's knowledge and must stay legal.
			name:    "target outside the app spec",
			seed:    "  - entity: comments\n    rows:\n      - body: hi\n        post_id: \"@external.id=x\"\n",
			wantErr: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			bp, err := decodeAppSpecString(refEntity + tc.seed)
			if err != nil {
				t.Fatalf("decode: %v", err)
			}
			bp.App = appspec.AppSpecApp{Name: "RefOrder", Module: "example.com/ref", DBDriver: "sqlite", DBURL: "file:ref.db"}
			verr := appspec.ValidateAppSpec(bp)
			if tc.wantErr && (verr == nil || !strings.Contains(verr.Error(), "posts")) {
				t.Fatalf("SECURITY: [seed] forward seed reference accepted; the shipped app aborts at boot on a fresh DB (resolveSeedRefs leaves it unresolved and UUID validation fails): err=%v", verr)
			}
			if !tc.wantErr && verr != nil {
				t.Fatalf("legitimate shape must still validate: %v", verr)
			}
		})
	}
}
