package entityui

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/hook"
)

// itemUI is a record whose optional fields cover every control the edit
// form draws for a stored value: numbers set and unset, a date, a
// timestamp with seconds, enums set, unset and holding a value the field
// no longer lists, a decimal, a relation and a relation an AfterGet hook
// masks.
func itemUI(t *testing.T) *testUI {
	t.Helper()
	items := entity.EntityConfig{
		Table: "items",
		Fields: []schema.Field{
			{Name: "name", Type: schema.String, Required: true},
			{Name: "qty", Type: schema.Int},
			{Name: "count", Type: schema.Int},
			{Name: "due", Type: schema.Date},
			{Name: "at", Type: schema.Timestamp},
			{Name: "kind", Type: schema.Enum, Values: []string{"a", "b", "c"}},
			{Name: "tier", Type: schema.Enum, Values: []string{"x", "y"}},
			{Name: "grade", Type: schema.Enum, Values: []string{"p", "q"}},
			{Name: "price", Type: schema.Decimal},
			{Name: "client", Type: schema.Relation, To: "customers"},
			{Name: "patron", Type: schema.Relation, To: "customers"},
		},
		Relations: []entity.Relation{
			entity.BelongsTo("clientRef", "customers", "client"),
			entity.BelongsTo("patronRef", "customers", "patron"),
		},
	}
	customers := entity.EntityConfig{Table: "customers", Fields: []schema.Field{{Name: "name", Type: schema.String}}}
	x := newTestUI(t, map[string]entity.EntityConfig{
		"items": items.WithTimestamps(false), "customers": customers.WithTimestamps(false),
	}, map[string][]map[string]any{
		"customers": {{"id": "cus-1", "name": "Acme"}, {"id": "cus-2", "name": "Bolt"}},
		"items": {{"id": "it-1", "name": "Item", "qty": 7, "due": "2026-10-27",
			"at": "2026-10-27T10:00:30.000000Z", "kind": "b", "price": "12.50",
			"client": "cus-1", "patron": "cus-2", "grade": "old"}},
	}, withAPI(map[string]string{"items": "/api/items", "customers": "/api/customers"}))
	ch, err := x.host.Crud(mustEntity(t, x, "items"))
	if err != nil {
		t.Fatal(err)
	}
	ch.Hooks = hook.NewHookRegistry()
	ch.Hooks.RegisterHook(hook.AfterGet, func(_ context.Context, data any) error {
		if p, ok := data.(*hook.GetPayload); ok && p.Result != nil {
			p.Result["patron"] = nil
		}
		return nil
	})
	return x
}

func putItem(t *testing.T, x *testUI, fields map[string]string) {
	t.Helper()
	ch, err := x.host.Crud(mustEntity(t, x, "items"))
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(fields)
	req := httptest.NewRequest(http.MethodPut, "/api/items/it-1", strings.NewReader(string(raw)))
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("id", "it-1")
	req = req.WithContext(asUser(req.Context(), "u1"))
	rec := httptest.NewRecorder()
	ch.Update().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("save %v = %d: %s", fields, rec.Code, rec.Body.String())
	}
}

// storedItem reads the row straight from the table, past the mask.
func storedItem(t *testing.T, x *testUI) map[string]string {
	t.Helper()
	rows, err := x.db.Query(`SELECT * FROM items WHERE id = 'it-1'`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	cols, _ := rows.Columns()
	if !rows.Next() {
		t.Fatal("no row")
	}
	vals := make([]any, len(cols))
	ptrs := make([]any, len(cols))
	for i := range vals {
		ptrs[i] = &vals[i]
	}
	if err := rows.Scan(ptrs...); err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	for i, c := range cols {
		if b, ok := vals[i].([]byte); ok {
			vals[i] = string(b)
		}
		out[c] = fmt.Sprint(vals[i])
	}
	return out
}

func renderItem(t *testing.T, x *testUI) string {
	t.Helper()
	return string(x.ui.Record("items", "it-1").Base("/rec/items").
		RenderCtx(x.userCtx("/rec/items/it-1", "", "u1")))
}

// Saving the edit form without touching it writes back what is stored.
// A blank control clears its column on update, so every control the
// form draws for a stored value has to carry it, and a control for an
// unset value has to submit blank, not a stand-in like 0.
func TestUntouchedEditSaveKeepsValues(t *testing.T) {
	x := itemUI(t)
	before := storedItem(t, x)
	body := renderItem(t, x)
	fields := formFields(body, "eui-items-form")
	if _, ok := fields["patron"]; ok {
		t.Fatalf("the record form submits the masked relation: %v", fields)
	}
	if !regexp.MustCompile(`<input[^>]*name="at"[^>]*step="1"|<input[^>]*step="1"[^>]*name="at"`).MatchString(body) {
		t.Errorf("a timestamp with seconds needs step=1, or the browser refuses the save:\n%s", body)
	}
	putItem(t, x, fields)
	if after := storedItem(t, x); fmt.Sprint(after) != fmt.Sprint(before) {
		t.Fatalf("an untouched save changed the row\nbefore %v\nafter  %v\nsent   %v", before, after, fields)
	}
}

// An optional enum's empty choice can be picked again, and picking it
// clears the column.
func TestOptionalEnumClears(t *testing.T) {
	x := itemUI(t)
	body := renderItem(t, x)
	sel := regexp.MustCompile(`(?s)<select[^>]*name="kind".*?</select>`).FindString(body)
	empty := regexp.MustCompile(`<option[^>]*value=""[^>]*>`).FindString(sel)
	if empty == "" || strings.Contains(empty, "disabled") {
		t.Fatalf("the optional enum offers no empty choice a user can pick:\n%s", sel)
	}
	fields := formFields(body, "eui-items-form")
	fields["kind"] = ""
	putItem(t, x, fields)
	if got := storedItem(t, x)["kind"]; got != "<nil>" {
		t.Fatalf("kind = %q after picking the empty choice, want NULL", got)
	}
}

// The masked relation changes through its own Replace form, which
// carries that one field.
func TestMaskedRelationReplaceForm(t *testing.T) {
	x := itemUI(t)
	body := renderItem(t, x)
	const id = "eui-items-replace-patron"
	if !strings.Contains(body, `id="`+id+`"`) {
		t.Fatalf("no Replace form for the masked relation:\n%s", body)
	}
	fields := formFields(body, id)
	delete(fields, "_csrf")
	if v, ok := fields["patron"]; len(fields) != 1 || !ok || v != "" {
		t.Fatalf("the Replace form carries %v, want only a blank patron", fields)
	}
	fields["patron"] = "cus-1"
	putItem(t, x, fields)
	if got := storedItem(t, x)["patron"]; got != "cus-1" {
		t.Fatalf("patron = %q after Replace, want cus-1", got)
	}
}
