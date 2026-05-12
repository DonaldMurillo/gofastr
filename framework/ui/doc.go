// Package ui is the framework's opinionated component layer on top of
// core-ui.
//
// The split:
//
//	core-ui/        — unstyled building blocks (elements, widget, runtime)
//	framework/ui/   — semantic components that consume framework/ui/theme
//
// Components in this package express *product intent* — PageHeader,
// FormField, EmptyState, StatusBadge — rather than HTML primitives.
// Every visual decision routes through framework/ui/theme so a single
// token swap re-skins the whole app.
//
// Consumers import this package directly:
//
//	import "github.com/gofastr/gofastr/framework/ui"
//	import "github.com/gofastr/gofastr/framework/ui/theme"
//
//	page := ui.PageHeader(ui.PageHeaderConfig{
//	    Title:    "Customers",
//	    Subtitle: "1,283 active",
//	    Actions:  ui.Button(ui.ButtonConfig{Label: "Delete all", Variant: ui.ButtonDanger}),
//	})
//
// If a piece of work maps 1:1 to an HTML element or ARIA pattern, it
// belongs in core-ui. If it composes primitives to express intent, it
// belongs here.
package ui
