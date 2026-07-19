# Printable documents

`battery/print` adds printing to a GoFastr app. You **declare
a printable document** the same way you declare a screen/route — a named,
route-addressable document — and the battery server-renders it into a
clean, chrome-free, **print-friendly HTML** page: its own `@page` size and
margins, the browser's native print dialog, and none of the host's
nav/sidebar/runtime. The HTML core is pure Go with no extra dependencies.

Real PDF output (invoices to email, receipts to archive, "Download PDF"
buttons) is **opt-in** through a pluggable renderer. The headless-Chromium
implementation lives in the separate `battery/print/chromepdf` subpackage,
so the core battery never imports `chromedp`.

## Wiring

```go
import "github.com/DonaldMurillo/gofastr/battery/print"

pb := print.New(print.Config{
    // All optional; defaults shown.
    PathPrefix:    "/print",
    DefaultPage:   print.A4Portrait(print.MM(12)),
    DefaultAccess: print.RequireAuth,        // per-user docs aren't public
    AppCSSURL:     "/__gofastr/app.css",     // inherit host design tokens
    // PDFRenderer: nil,                      // nil → /pdf routes return 501
})

app.RegisterBattery(pb)
```

`AppCSSURL` links the host's single stylesheet into every print page so
documents inherit your theme's `var(--*)` tokens. `runtime.js` is **never**
linked — a print document is inert.

### Component styles are preserved

A print document has no runtime to lazy-load component CSS, but the battery
still renders styled components faithfully: it scans the rendered body for
`data-fui-comp` markers and **inlines the scoped CSS** for every registered
`framework/ui` / `core-ui/patterns` component it finds. So a `Build` that
returns, say, a `DataTable` prints with its real styling — you get tokens
from `AppCSSURL` plus the component's own rules. What you don't get is
interactivity (sort/expand/RPC): a print page is static by design.

## Declaring documents

Call `Document` once per printable document, before `RegisterBattery`:

```go
pb.Document(print.Document{
    Name:  "invoice",            // unique; PDF filename stem
    Path:  "/invoice/{id}",      // Go 1.22 {param} syntax, under PathPrefix
    Title: "Invoice",
    Build: func(r *http.Request) (component.Component, error) {
        inv, err := invoices.Load(r.Context(), router.Param(r, "id"))
        if err != nil {
            return nil, print.ErrNotFound      // → clean 404
        }
        return &InvoiceDoc{Inv: inv}, nil      // a normal core-ui component
    },
})
```

This mounts:

- `GET /print/invoice/{id}`       — print-friendly HTML
- `GET /print/invoice/{id}/pdf`   — PDF (when a `PDFRenderer` is configured)

### Why `Build` is a closure

A battery cannot reach the UI app's DI container or render pipeline —
`framework.App` holds no reference to the `core-ui/app.App`. So the
document body is produced by your `Build` closure, which already has the
request and closes over your own services. It reads route params via
`router.Param`, loads data, and returns a `component.Component`. The
battery renders it with `component.SafeRenderCtx` (panic-safe). Return
`print.ErrNotFound` or `print.ErrForbidden` to get clean status pages
instead of a 500 with a stack trace.

### Printing from a host page

A print document is a legitimately separate, bookmarkable resource, so you
link to it — you do **not** make it an in-page island or a SPA route swap:

```go
pb.PrintLink("invoice", map[string]string{"id": inv.ID}, "Print invoice")
// → <a href="/print/invoice/42" target="_blank" rel="noopener">Print invoice</a>
```

`target="_blank"` opens the print view in a new tab so your SPA is
untouched. Set `Document.AutoPrint: true` to open the browser print dialog
automatically on load (served as an external, CSP-safe script — never
inline).

## Page setup

`PageConfig` becomes the `@page` rule in the rendered HTML and the paper
flags for the PDF renderer:

```go
pb.Document(print.Document{
    Name: "receipt", Path: "/receipt/{id}",
    Page: (&print.PageConfig{
        Size:         print.Custom,   // A4 | Letter | Legal | Custom
        CustomWidth:  "80mm",
        CustomHeight: "auto",
        Margin:       print.Margins{Top: "4mm", Right: "4mm", Bottom: "4mm", Left: "4mm"},
    }).Ptr(),
    Build: /* ... */,
})
```

Convenience constructors: `print.A4Portrait(m)`, `print.LetterPortrait(m)`,
and `print.MM(n)` for uniform millimetre margins. A document's `Page`
inherits any unset field from `Config.DefaultPage`. Custom lengths are
validated against an allow-list (`<number><mm|cm|in|px|pt>` or `auto`), so
a page config can never inject arbitrary CSS into the `@page` block.

Built-in CSS utilities available in your document components:
`.page-break` (force a page break), `.avoid-break` (keep a block together),
`.print-only` / `.screen-only` (show content only in one medium).

## Access control

Printable documents usually contain per-user data, so access is
**safe-by-default**: `Config.DefaultAccess` is `print.RequireAuth`. A
document is never world-readable unless you explicitly opt in:

```go
// Public marketing one-pager — no per-user data:
pb.Document(print.Document{Name: "brochure", Path: "/brochure",
    Access: print.Public, Build: /* ... */})

// Only the owner may print their invoice:
pb.Document(print.Document{Name: "invoice", Path: "/invoice/{id}",
    Access: print.RequireOwner(func(r *http.Request, user any) bool {
        return ownsInvoice(user, router.Param(r, "id"))
    }),
    Build: /* ... */})
```

The access policy runs **before** `Build`, so an unauthorized caller never
triggers a data load. The PDF route enforces the same gate — there's no
way to fetch the PDF of a document you can't see. Per-user pages are served
`Cache-Control: no-store`.

The print routes mount a CSP that allows inline `<style>` (the shell emits
server-generated `@page` rules and the print base) but keeps `script-src`
at `'self'` only — which is why auto-print is an external script, not an
inline `window.print()`.

## PDF output

Wire the headless-Chromium adapter to enable the `/pdf` routes:

```go
import "github.com/DonaldMurillo/gofastr/battery/print/chromepdf"

pb := print.New(print.Config{
    BaseURL:     "https://app.example.com", // for PDF token fidelity — see below
    AppCSSURL:   "/__gofastr/app.css",
    PDFRenderer: chromepdf.New(chromepdf.Options{
        // ExecPath:   "/usr/bin/chromium",   // "" → auto-detect
        // Timeout:    30 * time.Second,
        // ExtraFlags: []string{"no-sandbox"}, // common in containers
    }),
})
```

`RenderPDF` feeds the already-shelled HTML to Chromium via an in-memory
`data:` URL — the document bytes never touch disk, which matters for
per-user invoices — and prints with `WithPreferCSSPageSize`, so the shell's
`@page` size and margins win. Without a renderer, `/pdf` returns
`501 Not Implemented` (the route exists, the capability is unwired) rather
than a misleading 404.

**`Config.BaseURL` and PDF tokens.** A `data:` document has no origin, so
it can't resolve a relative `/__gofastr/app.css` link — design tokens would
be missing from the PDF. Set `BaseURL` to your app's canonical origin and
the battery makes the app.css link absolute against it on the PDF path
only. This origin is taken from `BaseURL` and **never** from the request
`Host` header — trusting `Host` would let a spoofed header point the
server-side stylesheet fetch at an internal/arbitrary address (an SSRF).
If `BaseURL` is unset, the PDF link stays relative (PDFs render with the
print base + inlined component CSS, just without app.css tokens).

`chromepdf` is the only package in this feature that imports `chromedp`;
apps that don't need PDF never pull it in.

## Common mistakes

- **Hand-rolling a print handler.** Writing a one-off `http.HandlerFunc`
  that emits `<!doctype html>` plus print CSS for a single page —
  declare a `print.Document` instead and get page setup, access, and PDF
  for free.
- **Re-implementing PDF generation.** Don't add a Go PDF library or shell
  out yourself — set `Config.PDFRenderer` to `chromepdf.New(...)`.
- **Making print an island or SPA route.** Print is a separate resource;
  link to it with `PrintLink` / `target="_blank"`, not a `data-fui-rpc`
  island or a `location.href` swap.
- **`print.Public` on per-user data.** Leaves invoices/receipts
  world-readable. Keep `RequireAuth` or use `RequireOwner`.
- **Expecting interactivity or host chrome.** Print pages are deliberately
  inert and chrome-free — no nav, no `runtime.js`, no sort/expand/RPC.
  Component *styles* are preserved (scoped CSS is inlined), but anything
  that needs hydration won't run. Build document bodies that read well as
  static output.
