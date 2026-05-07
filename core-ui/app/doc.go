// Package app provides the root application hierarchy for GoFastr UI applications.
//
// It defines the App, Layout, Screen, and Router types that compose the
// top-level structure of a web application. The package includes a simple
// dependency injection container and code-based routing.
//
// # Quick Start
//
//	app := app.NewApp("MyApp")
//	screen := app.NewScreen("/", myComponent)
//	app.RegisterScreen(screen, nil)
//	html, _ := app.RenderPage("/")
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
// The Container type provides a simple DI mechanism. Register constructors
// or values with Provide, then resolve them with Resolve or inject them
// into struct fields tagged with `inject:""`.
package app
