package entityui

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// echoKind prints the value it is handed and the row's token column,
// the way a careless custom renderer would.
func echoKind() Extensions {
	echo := func(c CellContext) render.HTML {
		return render.Text("[" + cell(c.Value) + "|" + cell(rowValue(c.Row, "token")) + "]")
	}
	return Extensions{Kinds: map[string]Kind{"echo": {Cell: echo, Detail: echo}}}
}

// A locked field's Detail gets the hooked row: a custom renderer cannot
// print a column a read hook masked.
func TestKindDetailSeesHookedRow(t *testing.T) {
	ents := invoiceEntities()
	cfg := ents["invoices"]
	d := *cfg.Display
	d.Fields = map[string]entity.FieldDisplay{"memo": {Input: "echo", Locked: true}}
	cfg.Display = &d
	ents["invoices"] = cfg
	x := newTestUIExt(t, ents, invoiceRows(), echoKind(), withAPI(map[string]string{
		"invoices": "/api/invoices", "payments": "/api/payments", "customers": "/api/customers",
	}))
	maskToken(t, x)
	body := renderRecord(t, x, "inv-1", nil)
	if strings.Contains(body, "tok-secret") {
		t.Fatalf("a kind's Detail printed the masked column:\n%s", body)
	}
	if !strings.Contains(body, "[first note|****]") {
		t.Fatalf("the Detail did not draw from the hooked row:\n%s", body)
	}
}

// A relation the caller may not read is muted before any kind draws it:
// neither a list cell nor a locked record value hands the foreign key to
// the app's callback.
func TestKindRelationRefusedBeforeCallback(t *testing.T) {
	posts := entity.EntityConfig{
		Fields: fields(
			schema.Field{Name: "title", Type: schema.String},
			schema.Field{Name: "author_id", Type: schema.Relation, To: "users"},
		),
		Exposure: &entity.ExposureConfig{Public: true},
		Display:  &entity.DisplayConfig{Fields: map[string]entity.FieldDisplay{"author_id": {Input: "echo", Locked: true}}},
	}
	x := newTestUIExt(t,
		map[string]entity.EntityConfig{
			"users": {Fields: fields(schema.Field{Name: "name", Type: schema.String})},
			"posts": posts,
		},
		map[string][]map[string]any{
			"users": {{"id": "usr-9q", "name": "Jane Author"}},
			"posts": {{"id": "p1", "title": "Hello", "author_id": "usr-9q"}},
		},
		echoKind(),
		withAPI(map[string]string{"posts": "/api/posts", "users": "/api/users"}),
	)
	list := listHTML(t, x.ui.List("posts"), x.ctx("/posts", ""))
	if !strings.Contains(list, "Hello") {
		t.Fatalf("the list lost its own rows:\n%s", list)
	}
	cards := listHTML(t, x.ui.List("posts").As("cards"), x.ctx("/posts", ""))
	record := string(x.ui.Record("posts", "p1").RenderCtx(x.ctx("/posts/p1", "")))
	for name, h := range map[string]string{"list": list, "cards": cards, "record": record} {
		if strings.Contains(h, "usr-9q") {
			t.Errorf("%s: a kind printed the refused foreign key:\n%s", name, h)
		}
	}
}
