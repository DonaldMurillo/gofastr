package entityui

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// A ShowWhen on an entity with no States draws the conditional field on
// the edit and create forms; the state-field branch must not read the
// absent States.
func TestShowWhenWithoutStates(t *testing.T) {
	entities := invoiceEntities()
	cus := entities["customers"]
	cus.Fields = append(cus.Fields,
		schema.Field{Name: "enabled", Type: schema.Bool},
		schema.Field{Name: "note", Type: schema.String})
	cus.Display = &entity.DisplayConfig{Fields: map[string]entity.FieldDisplay{
		"note": {ShowWhen: "enabled = true"},
	}}
	entities["customers"] = cus
	x := newTestUI(t, entities, invoiceRows(), withAPI(map[string]string{"customers": "/api/customers"}))
	failed := strings.ReplaceAll("Couldn't load this section", "'", "&#39;")
	for name, body := range map[string]string{
		"edit": string(x.ui.Record("customers", "cus-1").Base("/rec/customers").
			RenderCtx(x.userCtx("/rec/customers/cus-1", "", "u1"))),
		"create": string(x.ui.Create("customers").Base("/rec/customers").
			RenderCtx(x.userCtx("/rec/customers/create", "", "u1"))),
	} {
		if strings.Contains(body, failed) || !strings.Contains(body, `name="note"`) || !strings.Contains(body, `data-hui-when="enabled"`) {
			t.Fatalf("%s form with a ShowWhen and no States:\n%s", name, body)
		}
	}
}
