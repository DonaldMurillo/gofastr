# battery/print

Out-of-the-box printable documents. Declare a print document like you
declare a screen/route; the battery renders it to clean, chrome-free,
print-friendly HTML (its own `@page` size + margins, the browser's native
print dialog, no nav/runtime). Optional real PDF via a pluggable renderer.

**Use this when** the prompt mentions: print, printable, "print this page",
print-friendly, invoice, receipt, packing slip, report, statement,
"download PDF", "generate a PDF", `@page`, page size / margins, "set up
printing".

**Import:** `github.com/DonaldMurillo/gofastr/battery/print`
(PDF adapter: `github.com/DonaldMurillo/gofastr/battery/print/chromepdf`)

**Shape:**
```go
pb := print.New(print.Config{
    PathPrefix:    "/print",                 // default
    AppCSSURL:     "/__gofastr/app.css",     // inherit host design tokens
    DefaultPage:   print.A4Portrait(print.MM(12)),
    DefaultAccess: print.RequireAuth,        // safe default — see below
    // PDFRenderer: chromepdf.New(chromepdf.Options{}), // opt into PDF
})

pb.Document(print.Document{
    Name:  "invoice",
    Path:  "/invoice/{id}",                  // Go 1.22 {param} syntax
    Title: "Invoice",
    Access: print.RequireOwner(func(r *http.Request, user any) bool {
        return ownsInvoice(user, router.Param(r, "id"))
    }),
    AutoPrint: true,
    Build: func(r *http.Request) (component.Component, error) {
        inv, err := invoices.Load(r.Context(), router.Param(r, "id"))
        if err != nil { return nil, print.ErrNotFound }
        return &InvoiceDoc{Inv: inv}, nil    // a normal core-ui component
    },
})

app.RegisterBattery(pb)
```

**Routes per document:**
- `GET {prefix}{Path}`       → print-friendly HTML
- `GET {prefix}{Path}/pdf`   → PDF (501 if no `PDFRenderer` configured)
- `GET {prefix}/__autoprint.js` → external auto-print script (shared)

**Print a document from a host page** — a plain link, not an island:
```go
pb.PrintLink("invoice", map[string]string{"id": inv.ID}, "Print invoice")
// → <a href="/print/invoice/42" target="_blank" rel="noopener">Print invoice</a>
```

**Why a `Build` closure (not a registered component):** a battery cannot
reach the UI app's DI container or render pipeline (`framework.App` holds
no core-ui app). The host already holds its services, so `Build` reads
route params, loads data, and returns the body component — which the
battery renders panic-safely. Return `print.ErrNotFound` /
`print.ErrForbidden` for clean 404/403 status pages (never a stack trace).

**Access is safe-by-default:** `DefaultAccess` is `RequireAuth`, so a
per-user document (`/print/invoice/42`) is **never world-readable** unless
you set `Access: print.Public`. Use `print.RequireOwner` for ownership
checks. The gate runs BEFORE `Build`, so unauthorized callers never hit
the DB.

**PDF is opt-in and dependency-isolated:** only `battery/print/chromepdf`
imports chromedp. It feeds the shelled HTML to headless Chromium via an
in-memory `data:` URL (no temp file) and honors the shell's CSS `@page`
via `WithPreferCSSPageSize`. Containers usually need
`Options{ExtraFlags: []string{"no-sandbox"}}`. For PDF token fidelity set
`Config.BaseURL` to the app's canonical origin (used to make the app.css
link absolute on the PDF path) — the battery never derives this from the
request `Host` header (that would be an SSRF vector).

**Component styles render in print:** the shell scans the body for
`data-fui-comp` markers and inlines the scoped CSS for every registered
`framework/ui`/`core-ui/patterns` component, so styled components print
correctly. Interactivity (hydration/RPC) does NOT apply — print pages are
inert.

**Anti-patterns** — if you're about to write any of these, stop:
- A bespoke `http.HandlerFunc` that hand-writes `<!doctype html>` + print
  CSS for one page — declare a `print.Document` instead.
- Re-implementing PDF generation in a handler, or pulling in a Go PDF
  library — wire `chromepdf.New(...)` as the `PDFRenderer`.
- Making "print" an in-page island/RPC or a SPA route swap — a print
  document is a legitimately separate, bookmarkable resource; link to it
  with `PrintLink` / `target="_blank"`.
- Exposing an invoice/receipt route as `print.Public` "to keep it simple"
  — that leaks per-user data. Keep `RequireAuth`/`RequireOwner`.
- Linking `runtime.js` or app chrome into a print page — print docs are
  inert; the shell deliberately omits both.
