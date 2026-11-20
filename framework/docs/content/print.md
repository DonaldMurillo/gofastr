# Printable documents (`battery/print`)

`battery/print` renders named, route-addressable GoFastr components into clean, chrome-free, print-friendly HTML pages (`@page` rules, theme tokens, inlined component CSS, zero `runtime.js`) — with optional headless-Chromium PDF rendering via the separate `battery/print/chromepdf` module.

## Quickstart

```go
import "github.com/DonaldMurillo/gofastr/battery/print"

pb := print.New(print.Config{
	PathPrefix:    "/print",
	DefaultPage:   print.A4Portrait(print.MM(12)),
	DefaultAccess: print.RequireAuth,
	AppCSSURL:     "/__gofastr/app.css",
})

pb.Document(print.Document{
	Name:  "invoice",
	Path:  "/invoice/{id}",
	Title: "Invoice",
	Build: func(r *http.Request) (component.Component, error) {
		inv, err := invoices.Load(r.Context(), router.Param(r, "id"))
		if err != nil {
			return nil, print.ErrNotFound
		}
		return &InvoiceDoc{Inv: inv}, nil
	},
})

app.RegisterBattery(pb)
```

This mounts `GET /print/invoice/{id}` (HTML) and `GET /print/invoice/{id}/pdf` (`501` until a `PDFRenderer` is wired). Link to printable pages in a new tab with `pb.PrintLink(ctx, "invoice", map[string]string{"id": inv.ID}, "Print invoice")` (set `Document.AutoPrint: true` to open the browser print dialog via a CSP-safe external script).

## Page setup and access control

- **Paper sizes & margins:** `print.A4Portrait(m)`, `print.LetterPortrait(m)`, or `print.Custom` (e.g. `80mm` × `auto` thermal receipts); built-in helpers `.page-break`, `.avoid-break`, `.print-only`, `.screen-only`.
- **Safe-by-default access:** defaults to `print.RequireAuth` (use `print.RequireOwner(...)` for per-user records, or `print.Public` for brochures). Scoped API tokens/embed grants also require `print.Scope` (`"print:read"`). Access checks run *before* `Build` and apply to both HTML and PDF routes.

## Optional PDF rendering (`battery/print/chromepdf`)

```go
import "github.com/DonaldMurillo/gofastr/battery/print/chromepdf"

pb := print.New(print.Config{
	BaseURL:     "https://app.example.com",
	AppCSSURL:   "/__gofastr/app.css",
	PDFRenderer: chromepdf.New(chromepdf.Options{
		AllowedHosts: []string{"app.example.com"},
	}),
})
```

- `chromepdf` lives in its own Go module so apps that don't need PDF never pull in `chromedp`.
- Renders over DevTools directly from memory (document bytes never touch disk) with a concurrency cap (`MaxConcurrentRenders`, default `4`, returning `503` + `Retry-After: 1` when saturated).
- Set `Config.BaseURL` (never derived from `Host`, preventing SSRF) and list its host in `AllowedHosts` so headless Chromium can load `/__gofastr/app.css` tokens.

## Common mistakes

- **Mounting `PathPrefix: "/"`:** rejected at startup so print routes stay cleanly isolated from app routes and embed grants.
- **Using `print.Public` on invoices or receipts:** leaves private documents world-readable. Use `RequireOwner`.
- **Expecting interactive islands or sorting inside a print view:** print pages inline component CSS for faithful layout but omit `runtime.js` by design.
- **Opening print views inside a SPA swap or island:** link to them as separate resources (`PrintLink` / `target="_blank"`).
