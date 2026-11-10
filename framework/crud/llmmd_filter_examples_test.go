package crud

import (
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/router"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/filter"
)

// llm.md's filter-operator table ends each row with an example query that
// names the first visible field whose type suits the operator, "a filter a
// caller can send against this entity" (writeLLMOpRow). Its value is one
// of that field's type, so the route the page documents answers every
// example 200.
//
// The bug this holds: the field was chosen by type but the value was
// fixed per operator ("active", "a,b,c"), so on an entity whose first
// field is an integer key the page told an agent to send ?id=active,
// ?id_ne=active and ?id_in=a,b,c, each a 400 "not a valid int".

// llmFilterExamples is the last cell of each operator row of md's filter
// table: `key=value`.
func llmFilterExamples(t *testing.T, md string) []string {
	t.Helper()
	start := strings.Index(md, "**Filter operators**")
	end := strings.Index(md, "Or name the operator by word")
	if start < 0 || end < start {
		t.Fatalf("llm.md has no filter-operator table:\n%s", md)
	}
	var out []string
	for _, m := range regexp.MustCompile("\\| `([^`|]+=[^`|]*)` \\|\n").FindAllStringSubmatch(md[start:end], -1) {
		out = append(out, m[1])
	}
	if len(out) < 8 {
		t.Fatalf("found %d example cells, want one per operator row:\n%s", len(out), md[start:end])
	}
	return out
}

func TestLLMMDFilterExamplesAreAccepted(t *testing.T) {
	db := setupDB(t, `CREATE TABLE tickets (id INTEGER PRIMARY KEY AUTOINCREMENT, title TEXT, priority INTEGER)`)
	ent := entity.Define("tickets", entity.EntityConfig{
		Fields: []schema.Field{
			{Name: "id", Type: schema.Int, AutoGenerate: schema.AutoIncrement},
			{Name: "title", Type: schema.String},
			{Name: "priority", Type: schema.Int},
		},
		Exposure: &entity.ExposureConfig{Public: true},
	}.WithTimestamps(false))
	ent.SetDB(db)
	ch := NewCrudHandler(ent, db).WithJSONCase(CaseSnake)
	r := router.New()
	RegisterCrudRoutes(r, ch, "/tickets")

	for _, example := range llmFilterExamples(t, EntityLLMMD(ent)) {
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/tickets?"+example, nil))
		if rec.Code != http.StatusOK {
			t.Errorf("llm.md documents `?%s` as an example filter; the route answers %d %s", example, rec.Code, strings.TrimSpace(rec.Body.String()))
		}
	}
}

// The same holds whatever type the entity's first field has: each type
// the filter parser checks a value against gets a value it reads. The
// typed field "v" comes first, so every operator that suits its type
// names it; the others fall through to the text key. A reference is
// declared before the integer-keyed entity it points at, and the document
// is written without a registry: its sample must be a value of the
// integer the route, which has one, checks for.
func TestLLMMDFilterExamplesMatchTheFieldType(t *testing.T) {
	shops := entity.Define("shops", entity.EntityConfig{
		Fields:   []schema.Field{{Name: "id", Type: schema.Int, AutoGenerate: schema.AutoIncrement}},
		Exposure: &entity.ExposureConfig{Public: true},
	}.WithTimestamps(false))
	for _, c := range []struct {
		name  string
		field schema.Field
		// suits is how many of the eight operator rows name v.
		suits int
	}{
		{"int", schema.Field{Name: "v", Type: schema.Int}, 7},
		{"float", schema.Field{Name: "v", Type: schema.Float}, 7},
		{"decimal", schema.Field{Name: "v", Type: schema.Decimal}, 7},
		{"bool", schema.Field{Name: "v", Type: schema.Bool}, 3},
		{"timestamp", schema.Field{Name: "v", Type: schema.Timestamp}, 7},
		{"date", schema.Field{Name: "v", Type: schema.Date}, 7},
		{"enum", schema.Field{Name: "v", Type: schema.Enum, Values: []string{"open", "closed"}}, 8},
		{"uuid", schema.Field{Name: "v", Type: schema.UUID}, 8},
		{"string", schema.Field{Name: "v", Type: schema.String}, 8},
		{"reference to an integer key", schema.Field{Name: "v", Type: schema.Relation, To: "shops"}, 7},
	} {
		t.Run(c.name, func(t *testing.T) {
			db := setupDB(t, `CREATE TABLE things (v TEXT, id TEXT PRIMARY KEY)`)
			ent := entity.Define("things", entity.EntityConfig{
				Fields:   []schema.Field{c.field, {Name: "id", Type: schema.String}},
				Exposure: &entity.ExposureConfig{Public: true},
			}.WithTimestamps(false))
			ent.SetDB(db)
			ch := NewCrudHandler(ent, db).WithJSONCase(CaseSnake)
			ch.Registry = stubRegistry{byName: map[string]*entity.Entity{"shops": shops, "things": ent}}
			r := router.New()
			RegisterCrudRoutes(r, ch, "/things")

			named := 0
			for _, example := range llmFilterExamples(t, EntityLLMMD(ent)) {
				if strings.HasPrefix(example, "v") {
					named++
				}
				rec := httptest.NewRecorder()
				r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/things?"+example, nil))
				if rec.Code != http.StatusOK {
					t.Errorf("llm.md documents `?%s` as an example filter; the route answers %d %s", example, rec.Code, strings.TrimSpace(rec.Body.String()))
				}
			}
			if named != c.suits {
				t.Errorf("%d example rows name the %s field, want %d: the check above did not read its samples", named, c.name, c.suits)
			}
		})
	}
}

// An entity with no filterable field keeps the placeholder row and the
// operator's text sample: there is no field type to match.
func TestLLMMDFilterExamplePlaceholderKeepsTextSample(t *testing.T) {
	var b strings.Builder
	writeLLMOpRow(&b, "_in", filter.OpIn, "IN (comma-separated)", "a,b,c", nil, filter.NewKeySpeller())
	if !strings.Contains(b.String(), "`<field>_in=a,b,c`") {
		t.Errorf("placeholder row = %q, want the <field>_in=a,b,c example", b.String())
	}
}
