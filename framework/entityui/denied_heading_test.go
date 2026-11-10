package entityui

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// A refused screen is still a page: it keeps its heading. Drawn as the
// notice alone it had no <h1>, so an axe run failed page-has-heading-one
// on every screen over an entity the caller may not read, and the
// shortest app spec (one entity, one list, no sign-in) generated an app
// whose own TestAxeEveryScreen failed.
func TestRefusedScreenKeepsItsHeading(t *testing.T) {
	x := newTestUI(t,
		map[string]entity.EntityConfig{"notes": {
			Fields: fields(schema.Field{Name: "title", Type: schema.String}),
		}},
		map[string][]map[string]any{"notes": {{"id": "n1", "title": "classified note"}}},
	)
	list := listHTML(t, x.ui.List("notes"), x.ctx("/notes", ""))
	if !strings.Contains(list, "<h1") || !strings.Contains(list, AccessDeniedTitle.English()) {
		t.Errorf("refused list: want an <h1> and the notice:\n%s", list)
	}
	// A list told its heading level keeps it: an embedded list under a
	// page's own <h1> must not print a second one.
	sub := listHTML(t, x.ui.List("notes").Heading("Notes", 2), x.ctx("/notes", ""))
	if strings.Contains(sub, "<h1") || !strings.Contains(sub, "<h2") {
		t.Errorf("refused list at level 2: want an <h2> and no <h1>:\n%s", sub)
	}

	inv := newInvoiceUI(t)
	rec := string(inv.ui.Record("invoices", "inv-1").Base("/rec/invoices").
		RenderCtx(inv.ctx("/rec/invoices/inv-1", "")))
	if !strings.Contains(rec, "<h1") || !strings.Contains(rec, AccessDeniedTitle.English()) {
		t.Errorf("refused record: want an <h1> and the notice:\n%s", rec)
	}
	if strings.Contains(rec, "inv-1") {
		t.Errorf("refused record names the id it refused:\n%s", rec)
	}
}
