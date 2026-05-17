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
//	import "github.com/DonaldMurillo/gofastr/framework/ui"
//	import "github.com/DonaldMurillo/gofastr/framework/ui/theme"
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
//
// Component inventory (alphabetical):
//
//	Avatar          — circular image/initials avatar (sm/md/lg/xl)
//	Button          — primary/secondary/danger/ghost variants
//	Callout         — inline info/warning/danger/info/neutral block
//	Card            — labelled <section> with header/body/footer
//	Checkbox        — labelled checkbox with FieldErrors wiring
//	CodeBlock       — styled <pre><code> sample block
//	DataTable       — sortable/paginated table (island-friendly)
//	Divider         — <hr> for plain horizontal; role="separator" otherwise
//	EmptyState      — title/description/action block for no-data screens
//	FileUpload      — drag-drop file picker over <input type="file">
//	Form            — opinionated <form> wrapper with submit + errors
//	FormField       — labelled input with required + help + error states
//	FormSection     — grouped fields with heading + description
//	Menu            — <details>-driven dropdown menu (keyboard + ARIA)
//	Notification    — toast-styled inline notification (variant + dismiss)
//	OptimizedImage  — responsive <picture> with srcset + lazy + Width/Height
//	PageHeader      — top-of-page header with title/eyebrow/subtitle/actions
//	Radio           — labelled radio with FieldErrors wiring
//	Section         — labelled content section with heading + description
//	Sidebar         — responsive primary navigation (inline/drawer)
//	Spinner         — inline role="status" loading indicator
//	StatCard        — metric tile with label/value/trend
//	StatusBadge     — small status pill (success/warning/danger/info/neutral)
//	Switch          — iOS-style toggle (Checkbox variant)
//	Tag             — interactive pill (filter link or × dismiss)
//	Themed          — wraps a subtree in a registered theme override
//	Tooltip         — CSS-only hover/focus reveal
//
// Layout primitives (Stack, Cluster, Grid, Center, Spacer, Box) share
// one ui-layout stylesheet — see layout.go.
package ui
