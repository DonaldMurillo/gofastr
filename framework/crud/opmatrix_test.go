package crud

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/filter"
)

// matrixEntity builds one entity whose visible fields include one
// probe field f of tpe (Define adds the id/timestamp system columns,
// so the probe cannot be the only visible field).
func matrixEntity(tpe schema.FieldType) *entity.Entity {
	return entity.Define("things", entity.EntityConfig{
		Name:   "things",
		Table:  "things",
		Fields: []schema.Field{{Name: "f", Type: tpe}},
	})
}

// everyType spans the schema's field types. Image and File stay in: the
// filter parser treats them like any other column shape.
var everyType = []schema.FieldType{
	schema.String, schema.Text, schema.Int, schema.Float, schema.Decimal,
	schema.Bool, schema.Enum, schema.UUID, schema.Timestamp, schema.Date,
	schema.JSON, schema.Relation, schema.Image, schema.File,
}

// appliesToCell re-derives what an operator's "applies to" cell must
// say: the label of every field type the filter predicate accepts.
func appliesToCell(op filter.FilterOp) string {
	var labels []string
	for _, t := range everyType {
		if filter.OpSuitsType(op, t) {
			labels = append(labels, schema.FieldTypeLabel(t, false))
		}
	}
	return strings.Join(labels, ", ")
}

// The MCP list tool's schema advertises `<f>_<op>` exactly when the
// filter predicate accepts the operator on that column type — an agent
// must never be offered a param the re-dispatched route answers 400,
// nor denied one it accepts.
func TestMCPSchemaMatchesOpMatrix(t *testing.T) {
	for _, tpe := range everyType {
		props, _ := listToolSchema(matrixEntity(tpe))["properties"].(map[string]any)
		if props == nil {
			t.Fatalf("type %d: list schema missing properties", tpe)
		}
		for _, s := range filter.FilterSuffixes {
			_, offered := props["f"+s.Suffix]
			if offered != filter.OpSuitsType(s.Op, tpe) {
				t.Errorf("type %d: schema offers f%s = %v, OpSuitsType says %v", tpe, s.Suffix, offered, !offered)
			}
		}
	}
}

// An Enum's _like prop takes any substring: the field's value list
// stays on the equality and _in props only.
func TestMCPLikeOnEnumTakesSubstring(t *testing.T) {
	ent := entity.Define("things", entity.EntityConfig{
		Name: "things", Table: "things",
		Fields: []schema.Field{{Name: "f", Type: schema.Enum, Values: []string{"paid", "open"}}},
	})
	props, _ := listToolSchema(ent)["properties"].(map[string]any)
	like, _ := props["f_like"].(map[string]any)
	if like == nil {
		t.Fatal("schema does not offer f_like on an Enum")
	}
	if _, ok := like["enum"]; ok {
		t.Errorf("f_like carries the value list, so a substring is refused: %v", like)
	}
	if eq, _ := props["f"].(map[string]any); eq == nil || eq["enum"] == nil {
		t.Errorf("plain f lost its value list: %v", props["f"])
	}
}

// llm.md's operator table matches the filter predicate on both of its
// derived cells: "applies to" lists exactly the types OpSuitsType
// accepts, and the example names a field whose type accepts the
// operator — a filter a caller can send against this entity, never one
// its route answers 400.
func TestLLMMDOperatorRowsMatchOpMatrix(t *testing.T) {
	for _, tpe := range everyType {
		e := matrixEntity(tpe)
		byName := map[string]schema.Field{}
		for _, f := range e.GetFields() {
			byName[f.Name] = f
		}
		md := EntityLLMMD(e)
		for _, s := range filter.FilterSuffixes {
			row := ""
			for _, line := range strings.Split(md, "\n") {
				if strings.HasPrefix(line, "| `"+s.Suffix+"` |") {
					row = line
					break
				}
			}
			if row == "" {
				t.Fatalf("type %d: llm.md operator table lacks a %s row", tpe, s.Suffix)
			}
			cells := strings.Split(row, "|")
			if len(cells) < 6 {
				t.Fatalf("type %d: %s row does not carry applies-to and example cells: %q", tpe, s.Suffix, row)
			}
			if want := appliesToCell(s.Op); strings.TrimSpace(cells[3]) != want {
				t.Errorf("type %d: %s applies-to cell %q does not list exactly the accepted types %q", tpe, s.Suffix, cells[3], want)
			}
			example := strings.TrimSpace(cells[4])
			inner := strings.Trim(example, "`")
			name, _, _ := strings.Cut(inner, "=")
			name = strings.TrimSuffix(name, s.Suffix)
			f, ok := byName[name]
			if !ok {
				t.Fatalf("type %d: %s example %q names unknown field %q", tpe, s.Suffix, example, name)
			}
			if !filter.OpSuitsType(s.Op, f.Type) {
				t.Errorf("type %d: %s example %q names field %q of type %d, which refuses the operator", tpe, s.Suffix, example, name, f.Type)
			}
		}
	}
}

// llm.md's _like example is a plain substring: the server escapes LIKE
// wildcards (filter.EscapeLikePattern, contains semantics), so a value
// shaped `%search%` would match the literal text "%search%".
func TestLLMMDLikeExampleIsPlainSubstring(t *testing.T) {
	md := EntityLLMMD(matrixEntity(schema.String))
	if strings.Contains(md, `%search%`) {
		t.Errorf("llm.md _like example wraps its value in wildcards the server adds itself:\n%s", md)
	}
	if !strings.Contains(md, "_like=search") {
		t.Errorf("llm.md _like example should be the plain substring:\n%s", md)
	}
}
