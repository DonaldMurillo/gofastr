package entityui

import (
	"context"
	"regexp"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// dealsConfig names the money kind on an editable amount and a locked
// fee.
func dealsConfig() entity.EntityConfig {
	return entity.EntityConfig{
		Fields: fields(
			schema.Field{Name: "name", Type: schema.String, Required: true},
			schema.Field{Name: "amount", Type: schema.Decimal, Required: true, Min: fptr(0)},
			schema.Field{Name: "fee", Type: schema.Decimal},
		),
		Exposure: &entity.ExposureConfig{Public: true},
		Display: &entity.DisplayConfig{
			TitleField: "name",
			Fields: map[string]entity.FieldDisplay{
				"amount": {Input: "money", Label: "Deal value", Help: "Before tax"},
				"fee":    {Input: "money", Locked: true},
			},
		},
	}
}

func newDealsUI(t *testing.T, opts ...testUIOption) *testUI {
	t.Helper()
	return newTestUI(t,
		map[string]entity.EntityConfig{"deals": dealsConfig()},
		map[string][]map[string]any{"deals": {{"id": "d1", "name": "alpha", "amount": "1234.5", "fee": "-5"}}},
		append([]testUIOption{withAPI(map[string]string{"deals": "/api/deals"})}, opts...)...)
}

// The money input is a number control behind the currency symbol, its
// step letting cents through and its bounds from the schema.
func TestMoneyInputHasSymbolAndStep(t *testing.T) {
	x := newDealsUI(t)
	body := string(x.ui.Create("deals").Base("/deals").RenderCtx(x.userCtx("/deals/create", "", "u1")))
	group := regexp.MustCompile(`(?s)<div[^>]*fui-input-group[^>]*>.*?</div>`).FindString(body)
	if !regexp.MustCompile(`fui-input-group__prepend[^>]*>\$</span>`).MatchString(group) {
		t.Fatalf("the amount has no $ prefix:\n%s", body)
	}
	tag := tagFor(t, body, "amount")
	for _, want := range []string{`type="number"`, `step="0.01"`, `min="0"`, `required`} {
		if !strings.Contains(tag, want) {
			t.Errorf("amount lacks %s: %s", want, tag)
		}
	}
}

// A money column prints as an amount in the list, a negative one with
// the sign ahead of the symbol.
func TestMoneyCellPrintsAmount(t *testing.T) {
	x := newDealsUI(t)
	html := listHTML(t, x.ui.List("deals"), x.userCtx("/deals", "", "u1"))
	for _, want := range []string{"$1,234.50", "-$5.00"} {
		if !strings.Contains(html, want) {
			t.Errorf("the list lacks %s:\n%s", want, html)
		}
	}
}

// A locked money field reads as its label over the formatted amount,
// the same stacked row every read-only field draws.
func TestMoneyLockedFieldKeepsLabel(t *testing.T) {
	x := newDealsUI(t)
	body := string(x.ui.Record("deals", "d1").Base("/deals").RenderCtx(x.userCtx("/deals/d1", "", "u1")))
	row := regexp.MustCompile(`(?s)<dl[^>]*fui-detail-list--stacked.*?</dl>`).FindString(body)
	if !strings.Contains(row, ">Fee<") || !strings.Contains(row, "-$5.00") {
		t.Fatalf("the locked fee lost its label or amount:\n%s", body)
	}
}

// The catalog's currency entry replaces the symbol everywhere the kind
// draws it.
func TestMoneySymbolFromCatalog(t *testing.T) {
	cat := i18n.NewMapCatalog()
	cat.Set("en", "ui.entity.currency", i18n.Message{Text: "€"})
	x := newDealsUI(t, withTranslator(i18n.NewTranslator(cat, "en")))
	html := listHTML(t, x.ui.List("deals"), x.userCtx("/deals", "", "u1"))
	if !strings.Contains(html, "€1,234.50") || strings.Contains(html, "$1,234.50") {
		t.Errorf("the list did not take the catalog's symbol:\n%s", html)
	}
}

func TestMoneyFormatsText(t *testing.T) {
	ctx := context.Background()
	for in, want := range map[string]string{"0": "$0.00", "-1234": "-$1,234.00", "n/a": "n/a"} {
		if got := money(ctx, in); got != want {
			t.Errorf("money(%q) = %q, want %q", in, got, want)
		}
	}
}

// A built-in kind on a field type it does not fit is refused when the
// UI is built, before a screen draws a number box over text.
func TestBuiltinKindRefusesWrongType(t *testing.T) {
	for kind, typ := range map[string]schema.FieldType{"money": schema.String, "email": schema.Int} {
		cfg := entity.EntityConfig{
			Fields:   fields(schema.Field{Name: "v", Type: typ}),
			Exposure: &entity.ExposureConfig{Public: true},
			Display:  &entity.DisplayConfig{Fields: map[string]entity.FieldDisplay{"v": {Input: kind}}},
		}
		x := newTestHost(t, map[string]entity.EntityConfig{"things": cfg}, nil)
		if _, err := New(x.host, Extensions{}); err == nil || !strings.Contains(err.Error(), "does not fit") {
			t.Errorf("%s on %v: err = %v", kind, typ, err)
		}
	}
	// An app kind of the same name owns its own fit.
	cfg := entity.EntityConfig{
		Fields:   fields(schema.Field{Name: "v", Type: schema.String}),
		Exposure: &entity.ExposureConfig{Public: true},
		Display:  &entity.DisplayConfig{Fields: map[string]entity.FieldDisplay{"v": {Input: "money"}}},
	}
	x := newTestHost(t, map[string]entity.EntityConfig{"things": cfg}, nil)
	if _, err := New(x.host, Extensions{Kinds: map[string]Kind{"money": {Input: func(InputContext) render.HTML { return "" }}}}); err != nil {
		t.Errorf("an app money kind was refused: %v", err)
	}
}

// A kind draws the label and help the form resolved, the Display hint
// included, never the humanized field name.
func TestKindUsesDisplayLabel(t *testing.T) {
	x := newDealsUI(t)
	body := string(x.ui.Create("deals").Base("/deals").RenderCtx(x.userCtx("/deals/create", "", "u1")))
	label := regexp.MustCompile(`<label[^>]*\bfor="eui-f-amount"[^>]*>[^<]*`).FindString(body)
	if !strings.HasSuffix(label, ">Deal value") {
		t.Errorf("the amount label is not the hint: %q", label)
	}
	if !strings.Contains(body, "Before tax") {
		t.Errorf("the amount help is missing:\n%s", body)
	}
}
