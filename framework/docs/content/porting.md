# Porting an app with an existing frontend

If you are migrating an existing app onto GoFastr and tests or stylesheets pin an exact legacy DOM structure, you can render `html/template` markup inside GoFastr screens while keeping GoFastr's backend, auth, sessions, and routing.

> **Escape hatch, not the recommended path:** GoFastr has one supported styling surface (`framework/ui` + theme tokens). Ported foreign markup and CSS sit outside the design system, contracts pipeline, and upgrade guarantees. Migrate screens onto `framework/ui` one at a time when ready — both produce `render.HTML` and coexist cleanly.

## Rendering `html/template` inside a screen

Any screen returns `render.HTML`; embed `.tmpl` files with `//go:embed` rather than scattering inline strings:

<!-- gofastr:compile
stmt: _ = render.HTML(buf.String())
import "bytes"
import "html/template"
import "github.com/DonaldMurillo/gofastr/core/render"
-->
```go
var page = template.Must(template.New("invoice").Parse(
	`<article data-slot="invoice"><h1>{{.Title}}</h1></article>`))

var buf bytes.Buffer
if err := page.Execute(&buf, struct{ Title string }{"March"}); err != nil {
	panic(err)
}
```

Serve legacy compiled CSS/JS/fonts with `static.Mount(app.Router(), static.Config{FS: assets, Prefix: "/static"})` (see [Core packages](core-packages.md)).

## Opting legacy pages out of soft navigation

Pages whose legacy JS binds handlers only on initial document load can opt out of client-side SPA swaps:
- **Whole screen:** set `screen.NoSPA = true` so links to it perform a full document load.
- **Single link:** add `ExtraAttrs: html.Attrs{"data-cui-nav": "off"}`.

Mount a registered screen from a custom wildcard router path with `site.PageHandler("/legacy")`.

## Returning custom HTTP statuses (`404`, `410`) from screens

Implement `ScreenStatusCode() int` on a screen, or call `app.SetStatus(ctx, http.StatusNotFound)` inside `RenderCtx(ctx)` when a record is missing so crawlers, drawers, `llm.md`, and static export receive the true status code while rendering your full layout.

## Common mistakes

- **Mixing `framework/ui` styles and a foreign CSS framework on the same screen:** keep legacy screens isolated in their own package until migrated.
- **Forgetting `NoSPA = true` on legacy jQuery/DOMContentLoaded screens:** SPA swaps replace `<main>` without re-running legacy load listeners.
- **Using `ExtraAttrs` hoping to rewrite `framework/ui` DOM structure:** `ExtraAttrs` adds attributes; use `html/template` only if external tests require foreign DOM markup.
- **Scattering inline `template.Must` strings across dozens of Go files:** embed `.tmpl` files with `//go:embed`.
