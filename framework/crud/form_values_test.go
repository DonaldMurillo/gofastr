package crud

import (
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
)

// An HTML form posts every control, so a blank number or date arrives
// as "". The body must lose an optional blank, nil a required blank,
// and keep empty text and every real value.
func TestBlankFormValuesDropOrNil(t *testing.T) {
	s := schema.Schema{Fields: []schema.Field{
		{Name: "count", Type: schema.Int},
		{Name: "due", Type: schema.Date, Required: true},
		{Name: "title", Type: schema.String},
		{Name: "notes", Type: schema.Text},
		{Name: "price", Type: schema.Float},
	}}
	body := map[string]any{
		"count": "",
		"due":   "",
		"title": "",
		"notes": "",
		"price": "9.5",
	}
	dropEmptyFormValues(s, body)

	if _, ok := body["count"]; ok {
		t.Errorf("blank optional int kept: %#v", body["count"])
	}
	if v, ok := body["due"]; !ok || v != nil {
		t.Errorf("blank required date = %#v (present %v), want nil", v, ok)
	}
	for _, k := range []string{"title", "notes"} {
		if v, ok := body[k]; !ok || v != "" {
			t.Errorf("empty text %q = %#v (present %v), want \"\"", k, v, ok)
		}
	}
	if body["price"] != "9.5" {
		t.Errorf("non-blank value changed: %#v", body["price"])
	}
}
