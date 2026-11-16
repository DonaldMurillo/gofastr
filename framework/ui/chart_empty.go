package ui

import (
	"context"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/i18nui"
)

// chartEmpty renders the zero-data placeholder every chart shares.
func chartEmpty(ctx context.Context, id, labelledBy, class, message string, extra html.Attrs) render.HTML {
	if message == "" {
		message = i18nui.ChartNoData.Text(ctx)
	}
	attrs := html.SafeExtraAttrs(extra, "role", "aria-labelledby", "aria-label")
	if attrs == nil {
		attrs = map[string]string{}
	}
	attrs["data-cui-comp"] = "ui-chart-empty"
	attrs["role"] = "img"
	if class != "" {
		attrs["class"] = class
	}
	if id != "" {
		attrs["id"] = id
	}
	if labelledBy != "" {
		attrs["aria-labelledby"] = labelledBy
	} else {
		attrs["aria-label"] = message
	}
	return render.Tag("div", attrs, render.Text(message))
}

var chartEmptyStyle = registry.RegisterStyle("ui-chart-empty", func(_ style.Theme) string {
	return `[data-cui-comp="ui-chart-empty"] {
	display: flex;
	align-items: center;
	justify-content: center;
	width: 100%;
	min-height: var(--ui-chart-empty-min-height, 10rem);
	color: var(--color-text-muted, #52525B);
	font-size: var(--text-sm, 0.875rem);
	background: var(--color-surface-soft, transparent);
	border: var(--stroke-thin, 1px) dashed var(--color-border, #E4E4E7);
	border-radius: var(--radii-md, 8px);
	padding: var(--spacing-lg, 1rem);
	text-align: center;
}`
})

var _ = chartEmptyStyle
