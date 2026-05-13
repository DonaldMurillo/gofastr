// Package app is the URL → rendered page pipeline for GoFastr UI.
//
// It composes the screen registry (Router), the per-screen lifecycle
// (Screen + Load/Render), the shared chrome (Layout), and the request-
// in-context helpers into a single App value. Anything that turns
// "request for URL X" into "rendered HTML for URL X" lives in this
// package.
//
// The DI container is its own concern and lives in the sibling
// [core-ui/di] package. App wires one in via App.Container so screens
// can be injected during the Load phase. Visual primitives live in
// [core-ui/html] (1:1 HTML tags) and [core-ui/patterns] (higher-
// level UI patterns).
//
// # Quick Start
//
// Components that implement ScreenSpec carry their own metadata
// (title, description, type), so Register reads it directly:
//
//	import (
//	    "github.com/DonaldMurillo/gofastr/core-ui/app"
//	    "github.com/DonaldMurillo/gofastr/core/render"
//	)
//
//	type Home struct{}
//	func (h *Home) Render() render.HTML         { return render.Text("hi") }
//	func (h *Home) ScreenTitle() string         { return "Home" }
//	func (h *Home) ScreenDescription() string   { return "" }
//	func (h *Home) ScreenType() app.ScreenType  { return app.ScreenPage }
//
//	application := app.NewApp("MyApp")
//	application.Register("/", &Home{}, nil)
//	html, _ := application.RenderPage(ctx, "/")
//
// For components without ScreenSpec, use RegisterScreen with the
// builder: app.RegisterScreen(app.NewScreen("/", comp).WithTitle("Home"), nil).
//
// # Screen Types
//
// Four screen types are supported:
//   - Page: full-page views rendered inside <main>
//   - Drawer: side panels rendered inside <aside>
//   - Sheet: bottom sheets rendered as modal dialogs
//   - Dialog: modal dialogs with overlay backdrop
//
// # Layouts
//
// Layouts provide shared chrome (header, sidebar, footer) that wraps
// screen content. A default layout can be set for all screens, or
// individual screens can override with their own layout.
//
// # Dependency Injection
//
// App.Provide / App.Inject are thin convenience wrappers over the
// [core-ui/di.Container] held in App.Container. Register constructors
// or values with Provide, then resolve them with Resolve or inject them
// into struct fields tagged with `inject:""`.
package app
