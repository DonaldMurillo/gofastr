package framework

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// A queryable field whose name is another's plus an operator suffix is
// a silent wrong-column filter: ?status_ne=x parses as `status != x`
// and the status_ne column can never be filtered. Registration must
// refuse the pair, naming both fields.
func TestEntityRefusesFilterSuffixCollisions(t *testing.T) {
	cases := []struct {
		name   string
		fields []schema.Field
		want   string
	}{
		{
			"ne",
			[]schema.Field{
				{Name: "status", Type: schema.String},
				{Name: "status_ne", Type: schema.String},
			},
			`field "status_ne" collides with field "status"`,
		},
		{
			// The dotted-down surfaces (?author.status_ne=) parse the
			// same way, so a range suffix collides too.
			"gte",
			[]schema.Field{
				{Name: "amount", Type: schema.Int},
				{Name: "amount_gte", Type: schema.Int},
			},
			`field "amount_gte" collides with field "amount"`,
		},
		{
			// A WireName claims its key just as a name does: the pair
			// arrives under different column names.
			"wire name",
			[]schema.Field{
				{Name: "status", Type: schema.String},
				{Name: "state", Type: schema.String, WireName: "status_ne"},
			},
			`field "state" collides with field "status"`,
		},
		{
			// The shadowed side can be the one carrying the alias.
			"wire name shadowed",
			[]schema.Field{
				{Name: "flag", Type: schema.Bool},
				{Name: "mark", Type: schema.Bool, WireName: "flag_like"},
			},
			`field "mark" collides with field "flag"`,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			app := atomicTestApp(t)
			err := app.TryEntity("invoices", entity.EntityConfig{Fields: c.fields})
			if err == nil {
				t.Fatalf("entity with colliding fields %v registered", c.fields)
			}
			for _, want := range []string{c.want, "rename one field or set a WireName"} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error %q does not name %q", err, want)
				}
			}
		})
	}
}

// Fields a query cannot reach claim nothing: a NoQuery or Hidden
// status_ne is refused by name on every filter surface, so it cannot
// shadow the operator. And a field whose own WireName is its name plus
// a suffix keeps its column filterable under the bare name — the alias
// is shadowed, not the column.
func TestEntityAcceptsBenignSuffixShapes(t *testing.T) {
	ok := [][]schema.Field{
		{
			{Name: "status", Type: schema.String},
			{Name: "status_ne", Type: schema.String, NoQuery: true},
		},
		{
			{Name: "status", Type: schema.String},
			{Name: "status_ne", Type: schema.String, Hidden: true},
		},
		{
			// ?x_ne= parses as x != …, but the column x is still
			// filterable as ?x=; no column becomes unreachable.
			{Name: "x", Type: schema.String, WireName: "x_ne"},
		},
	}
	for _, fields := range ok {
		app := atomicTestApp(t)
		if err := app.TryEntity("invoices", entity.EntityConfig{Fields: fields}); err != nil {
			t.Fatalf("benign shape %v refused: %v", fields, err)
		}
	}
}

// GroupEntity parses ?field_<op>= exactly the way App.Entity's routes
// do, so the same collision refuses the group-scoped registration.
func TestGroupEntityRefusesFilterSuffixCollisions(t *testing.T) {
	app := atomicTestApp(t)
	g := app.Group("/billing")
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("GroupEntity registered colliding fields")
		}
		if !strings.Contains(strings.TrimSpace(r.(string)), `field "status_ne" collides with field "status"`) {
			t.Errorf("panic %v does not name both fields", r)
		}
	}()
	app.GroupEntity(g, "invoices", entity.EntityConfig{
		Fields: []schema.Field{
			{Name: "status", Type: schema.String},
			{Name: "status_ne", Type: schema.String},
		},
	})
}
