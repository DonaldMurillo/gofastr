package main

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/framework/filter"
)

// A spec whose fields collide through an operator suffix (`status` and
// `status_ne`) used to pass the duplicate-flag check — which never
// looked at -ne — and panic inside flag.FlagSet when the generated CLI
// ran. The check now derives the flag set from the same operator table
// the emitter uses, so the spec is refused at generate time naming both
// fields.
func TestRenderCLI_SuffixFieldFlagCollision(t *testing.T) {
	decls := []framework.EntityDeclaration{{
		Name:  "invoices",
		Table: "invoices",
		Fields: []framework.FieldDeclaration{
			{Name: "status", Type: "string"},
			{Name: "status_ne", Type: "string"},
		},
	}}
	_, err := buildCLISpec(decls, defaultCLIOptions(), "example.com/app/entities/client")
	if err == nil {
		t.Fatal("expected suffix-collision error for status/status_ne, got nil")
	}
	for _, want := range []string{"status", "status_ne", "--status-ne"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error should name %q: %v", want, err)
		}
	}
}

// declTypeForType names the declaration spelling of every schema field
// type the generator accepts.
var declTypeForType = map[schema.FieldType]string{
	schema.String:    "string",
	schema.Text:      "text",
	schema.Int:       "int",
	schema.Float:     "float",
	schema.Decimal:   "decimal",
	schema.Bool:      "bool",
	schema.Enum:      "enum",
	schema.UUID:      "uuid",
	schema.Timestamp: "timestamp",
	schema.Date:      "date",
	schema.JSON:      "json",
	schema.Relation:  "relation",
}

// The list verb's filter table offers `<field>-<op>` exactly when the
// filter predicate accepts the operator on that column type — one
// matrix over every field type × every suffix, both directions: a flag
// the server answers 400 must not ship, and one it accepts must not be
// hidden (the old hardcoded Comparable/Likeable lists did both).
func TestRenderCLI_FilterFlagsMatchOpMatrix(t *testing.T) {
	for tpe, declType := range declTypeForType {
		decls := []framework.EntityDeclaration{{
			Name:   "things",
			Table:  "things",
			Fields: []framework.FieldDeclaration{{Name: "f", Type: declType}},
		}}
		spec, err := buildCLISpec(decls, cliOptions{outDir: "cli", binary: "myapp", verbs: "list"}, "example.com/app/entities/client")
		if err != nil {
			t.Fatalf("type %s: buildCLISpec: %v", declType, err)
		}
		var sb strings.Builder
		renderCLIListTables(&sb, spec.Entities[0])
		table := sb.String()
		for _, s := range filter.FilterSuffixes {
			if s.Op == filter.OpIn {
				// IN is the bare equality flag's comma-list mode, not a
				// flag of its own; it suits every type.
				continue
			}
			flagRow := `{flag: "f-` + strings.TrimPrefix(s.Suffix, "_") + `", param: "f` + s.Suffix + `"`
			if filter.OpSuitsType(s.Op, tpe) {
				if !strings.Contains(table, flagRow) {
					t.Errorf("type %s: table lacks the accepted operator's row %q", declType, flagRow)
				}
			} else if strings.Contains(table, flagRow) {
				t.Errorf("type %s: table offers the refused operator's row %q", declType, flagRow)
			}
		}
	}
}
