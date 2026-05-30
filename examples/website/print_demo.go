package main

import (
	"fmt"
	"net/http"

	"github.com/DonaldMurillo/gofastr/battery/print"
	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/core/router"
	"github.com/DonaldMurillo/gofastr/framework"
)

// registerPrintDemos wires battery/print demo documents onto the site.
//
// It mounts the routes directly via RegisterRoutes (rather than
// fwApp.RegisterBattery) because setupServer composes the router eagerly
// and the example is served via app.Router() without App.Start — the same
// reason the island/demo handlers in main.go register directly. The real
// host-app recipe is `app.RegisterBattery(pb)`, which runs the same
// RegisterRoutes at Start.
func registerPrintDemos(fwApp *framework.App) {
	pb := print.New(print.Config{
		// Demo pages carry no per-user data, so they're Public. Real
		// invoices/statements should keep the RequireAuth default or use
		// print.RequireOwner — see the "statement" doc below.
		DefaultAccess: print.Public,
		AppCSSURL:     "/__gofastr/app.css", // inherit the site's design tokens
	}).
		Document(print.Document{
			Name:      "demo-invoice",
			Path:      "/invoice/{id}",
			Title:     "Invoice",
			AutoPrint: false, // demo: don't pop the print dialog during E2E
			Build: func(r *http.Request) (component.Component, error) {
				return printInvoiceDemo{id: router.Param(r, "id")}, nil
			},
		}).
		Document(print.Document{
			Name:  "demo-receipt",
			Path:  "/receipt",
			Title: "Receipt",
			Page:  (&print.PageConfig{Size: print.Custom, CustomWidth: "80mm", CustomHeight: "auto", Margin: print.MM(4)}).Ptr(),
			Build: func(r *http.Request) (component.Component, error) {
				return printReceiptDemo{}, nil
			},
		}).
		Document(print.Document{
			Name:  "demo-statement",
			Path:  "/statement/{id}",
			Title: "Statement",
			// Override the Public default: a statement is per-user data, so
			// it stays behind RequireAuth. With no auth chain wired on the
			// demo site, anonymous callers always get 401 — which is the
			// point of the safe default.
			Access: print.RequireAuth,
			Build: func(r *http.Request) (component.Component, error) {
				return printInvoiceDemo{id: router.Param(r, "id")}, nil
			},
		})

	pb.RegisterRoutes(fwApp.Router())
}

// printInvoiceDemo is a tiny standalone invoice body used by the print
// demos. It renders only the document content — the battery's shell adds
// the <html>/<head>/@page chrome.
type printInvoiceDemo struct{ id string }

func (d printInvoiceDemo) Render() render.HTML {
	id := render.Escape(d.id)
	return render.HTML(fmt.Sprintf(`
<header class="inv-head">
  <h1>Invoice %s</h1>
  <p>GoFastr, Inc. · 123 Demo Street · billing@example.com</p>
</header>
<table class="inv-table">
  <thead><tr><th>Item</th><th>Qty</th><th>Price</th></tr></thead>
  <tbody>
    <tr><td>Widget Pro license</td><td>2</td><td>$49.00</td></tr>
    <tr><td>Priority support (1 yr)</td><td>1</td><td>$120.00</td></tr>
  </tbody>
  <tfoot><tr><th>Total</th><th></th><th>$218.00</th></tr></tfoot>
</table>
<p class="avoid-break">Thank you for your business.</p>`, id))
}

// printReceiptDemo is a narrow thermal-style receipt body.
type printReceiptDemo struct{}

func (printReceiptDemo) Render() render.HTML {
	return render.HTML(`
<h1>Receipt</h1>
<p>GoFastr Cafe</p>
<table class="inv-table">
  <tbody>
    <tr><td>Espresso</td><td>$3.00</td></tr>
    <tr><td>Croissant</td><td>$4.50</td></tr>
  </tbody>
  <tfoot><tr><th>Total</th><th>$7.50</th></tr></tfoot>
</table>
<p>Thanks — come again!</p>`)
}
