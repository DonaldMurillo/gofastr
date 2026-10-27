package entityui

import (
	"encoding/json"
	"html"
	"regexp"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

func storedValuesUI(t *testing.T, row map[string]any) *testUI {
	t.Helper()
	docs := entity.EntityConfig{
		Table: "docs",
		Fields: []schema.Field{
			{Name: "id", Type: schema.String},
			{Name: "meta", Type: schema.JSON},
			{Name: "price", Type: schema.Decimal},
		},
	}
	return newTestUI(t, map[string]entity.EntityConfig{"docs": docs.WithTimestamps(false)},
		map[string][]map[string]any{"docs": {row}},
		withAPI(map[string]string{"docs": "/api/docs"}))
}

// A JSON field reads back decoded, and the form drew the Go value:
// map[plan:pro], which the browser's JSON check refused, so no edit of
// the record could be saved.
func TestEditFormShowsStoredJSON(t *testing.T) {
	x := storedValuesUI(t, map[string]any{"id": "d1", "meta": `{"plan":"pro","n":[1,2]}`})
	h := string(x.ui.Record("docs", "d1").Base("/rec/docs").RenderCtx(x.userCtx("/rec/docs/d1", "", "u1")))
	m := regexp.MustCompile(`(?s)<textarea[^>]*name="meta"[^>]*>(.*?)</textarea>`).FindStringSubmatch(h)
	if m == nil {
		t.Fatalf("no meta textarea:\n%s", h)
	}
	text := html.UnescapeString(m[1])
	var got map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(text)), &got); err != nil || got["plan"] != "pro" {
		t.Fatalf("meta textarea holds %q, want the stored document as JSON", text)
	}
	if strings.Contains(h, "map[") {
		t.Errorf("the record page prints a Go map:\n%s", h)
	}
}

// A Decimal column holds four fraction digits, and the input stepped
// by 0.01, so a stored 19.995 failed the browser's step check and the
// form could not be saved untouched.
func TestDecimalStepAdmitsStoredValue(t *testing.T) {
	x := storedValuesUI(t, map[string]any{"id": "d1", "price": "19.995"})
	h := string(x.ui.Record("docs", "d1").Base("/rec/docs").RenderCtx(x.userCtx("/rec/docs/d1", "", "u1")))
	m := regexp.MustCompile(`<input[^>]*name="price"[^>]*>`).FindString(h)
	if m == "" {
		t.Fatalf("no price input:\n%s", h)
	}
	if !strings.Contains(m, `value="19.995"`) {
		t.Fatalf("price input = %s, want the stored 19.995", m)
	}
	if !strings.Contains(m, `step="0.0001"`) {
		t.Errorf("price input = %s, want step 0.0001, the column's scale", m)
	}
}

// SQLite keeps every digit written, so a stored value may be finer than
// the column's four; the step follows it.
func TestDecimalStepFollowsFinerValue(t *testing.T) {
	for val, want := range map[string]float64{"19.995": 1e-4, "3.14159": 1e-5, "7": 1e-4, "1.23456e3": 1e-4, "0.1234567890123456": 1e-4} {
		if got := decimalStep(val); got != want {
			t.Errorf("decimalStep(%q) = %g, want %g", val, got, want)
		}
	}
	x := storedValuesUI(t, map[string]any{"id": "d1", "price": "3.14159"})
	h := string(x.ui.Record("docs", "d1").Base("/rec/docs").RenderCtx(x.userCtx("/rec/docs/d1", "", "u1")))
	m := regexp.MustCompile(`<input[^>]*name="price"[^>]*>`).FindString(h)
	if !strings.Contains(m, `value="3.14159"`) || !strings.Contains(m, `step="0.00001"`) {
		t.Errorf("price input = %s, want value 3.14159 with step 0.00001", m)
	}
}
