package entityui

import (
	"context"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core/render"
)

type createScreen struct {
	component.ContextOnly
	x *testUI
}

func (s *createScreen) RenderCtx(ctx context.Context) render.HTML {
	return s.x.ui.Create("invoices").Base("/rec/invoices").RenderCtx(ctx)
}

// A create opened as a drawer returns to the page it opened over (the
// customer whose Related tab added the invoice); as a full page it lands
// on the list.
func TestCreateReturnsToItsOrigin(t *testing.T) {
	x := newInvoiceUI(t)
	a := app.NewApp("rec")
	a.Register("/rec/invoices/create", &createScreen{x: x}, nil, app.InterceptFrom("/rec/invoices", app.ScreenDrawer))
	ctx := x.userCtx("/rec/invoices/create", "prefill_customer_id=c-1", "u1")

	drawer, err := a.RenderOverlayResult(ctx, "/rec/invoices/create", "/rec/customers/c-1?tab=related", app.ScreenDrawer)
	if err != nil {
		t.Fatal(err)
	}
	if want := `data-cui-rpc-navigate="/rec/customers/c-1"`; !strings.Contains(string(drawer.HTML), want) {
		t.Errorf("the drawer's create does not return to its origin (%s):\n%s", want, drawer.HTML)
	}
	page, err := a.RenderPartialResult(ctx, "/rec/invoices/create")
	if err != nil {
		t.Fatal(err)
	}
	if want := `data-cui-rpc-navigate="/rec/invoices"`; !strings.Contains(string(page.HTML), want) {
		t.Errorf("the page's create does not land on the list (%s):\n%s", want, page.HTML)
	}
}
