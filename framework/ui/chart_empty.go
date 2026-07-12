package ui

import (
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// chartEmpty renders the zero-data placeholder shared by every chart
// component. Data-bound charts (dashboards, reports) legitimately receive no
// rows — a brand-new user has no customers yet — so an empty dataset is a
// normal runtime state, NOT misuse. A chart must show a calm empty state in
// that case; panicking would crash the whole screen (and, behind the SSR
// host's panic recovery, surface as a 404 on the first page a user ever sees).
//
// Genuine programmer errors (negative values, missing series names, unknown
// shapes) still panic — those are bugs, not states.
// The height argument is accepted for call-site symmetry with the chart
// constructors but is intentionally not turned into an inline style — the
// framework forbids inline styles (everything ships via registered CSS), so the
// placeholder's min-height is a fixed token in the stylesheet below.
func chartEmpty(_ int, labelledBy, class, message string) render.HTML {
	if message == "" {
		message = "No data yet"
	}
	attrs := map[string]string{
		"data-fui-comp": "ui-chart-empty",
		"role":          "img",
	}
	if class != "" {
		attrs["class"] = class
	}
	if labelledBy != "" {
		attrs["aria-labelledby"] = labelledBy
	} else {
		attrs["aria-label"] = message
	}
	return render.Tag("div", attrs, render.Text(message))
}

var chartEmptyStyle = registry.RegisterStyle("ui-chart-empty", func(_ style.Theme) string {
	return `[data-fui-comp="ui-chart-empty"] {
	display: flex;
	align-items: center;
	justify-content: center;
	width: 100%;
	min-height: 10rem;
	color: var(--color-text-muted, #6b7280);
	font-size: var(--text-sm, 0.875rem);
	background: var(--color-surface-soft, transparent);
	border: 1px dashed var(--color-border, #e5e7eb);
	border-radius: var(--radii-md, 8px);
	padding: var(--spacing-lg, 1rem);
	text-align: center;
}`
})

var _ = chartEmptyStyle
