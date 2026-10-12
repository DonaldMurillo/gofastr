package entityui

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// JSON fields and the code kind draw in the mono font through TextArea's
// Monospace; a plain Text field does not.
func TestJSONAndCodeInputsAreMono(t *testing.T) {
	docs := entity.EntityConfig{
		Table: "docs",
		Fields: []schema.Field{
			{Name: "body", Type: schema.Text},
			{Name: "meta", Type: schema.JSON},
			{Name: "src", Type: schema.Text},
		},
		Display: &entity.DisplayConfig{Fields: map[string]entity.FieldDisplay{"src": {Input: "code"}}},
	}
	x := newTestUI(t, map[string]entity.EntityConfig{"docs": docs.WithTimestamps(false)}, nil,
		withAPI(map[string]string{"docs": "/api/docs"}))
	h := string(x.ui.Create("docs").Base("/rec/docs").RenderCtx(x.userCtx("/rec/docs/create", "", "u1")))
	if n := strings.Count(h, "fui-textarea--mono"); n != 2 {
		t.Fatalf("mono fields = %d, want meta and src:\n%s", n, h)
	}
	// The JSON field alone is checked as JSON in the browser.
	if n := strings.Count(h, `data-hui-json="Enter valid JSON"`); n != 1 {
		t.Errorf("JSON checks = %d, want the meta field's:\n%s", n, h)
	}
}
