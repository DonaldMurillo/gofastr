package ui

// RecordSummary and MetricBand provide the missing composition primitives for
// record- and operations-heavy pages. They deliberately own structure and
// responsive behavior so host applications do not recreate a bespoke hero,
// stat-card grid, or button-width override for every important record.

import (
	"strconv"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// RecordSummaryTone selects the semantic accent rail for RecordSummary.
type RecordSummaryTone string

const (
	RecordSummaryToneNeutral RecordSummaryTone = ""
	RecordSummaryToneInfo    RecordSummaryTone = "info"
	RecordSummaryToneSuccess RecordSummaryTone = "success"
	RecordSummaryToneWarning RecordSummaryTone = "warning"
	RecordSummaryToneDanger  RecordSummaryTone = "danger"
)

// RecordSummaryConfig configures the dominant summary of one record, event, or
// operational state. The slots are intentionally bounded: use Highlight for
// the next decision, Metrics for a MetricBand, Aside for one compact supporting
// fact group, Footer for ownership/context, and Actions for natural-width
// primary controls. Actions render in the lead region so the primary path does
// not fall below a long summary on phones.
type RecordSummaryConfig struct {
	Title        string
	Eyebrow      string
	Description  string
	Status       render.HTML
	Highlight    render.HTML
	Metrics      render.HTML
	Aside        render.HTML
	Footer       render.HTML
	Actions      render.HTML
	Tone         RecordSummaryTone
	HeadingLevel int
	ID           string
	Class        string
}

// RecordSummary renders a compact semantic <article>. It is the one dominant
// summary for a page; do not repeat the same state in a separate Banner.
func RecordSummary(cfg RecordSummaryConfig) render.HTML {
	if cfg.Title == "" {
		panic("ui: RecordSummary requires Title")
	}
	switch cfg.Tone {
	case RecordSummaryToneNeutral, RecordSummaryToneInfo, RecordSummaryToneSuccess, RecordSummaryToneWarning, RecordSummaryToneDanger:
	default:
		panic("ui: RecordSummary unknown Tone " + string(cfg.Tone))
	}
	level := cfg.HeadingLevel
	if level == 0 {
		level = 1
	}
	if level < 1 || level > 6 {
		panic("ui: RecordSummary HeadingLevel must be 1-6")
	}

	copy := make([]render.HTML, 0, 3)
	if cfg.Eyebrow != "" {
		copy = append(copy, html.Paragraph(html.TextConfig{Class: "ui-record-summary__eyebrow"}, render.Text(cfg.Eyebrow)))
	}
	copy = append(copy, html.Heading(html.HeadingConfig{Level: level, Class: "ui-record-summary__title"}, render.Text(cfg.Title)))
	if cfg.Description != "" {
		copy = append(copy, html.Paragraph(html.TextConfig{Class: "ui-record-summary__description"}, render.Text(cfg.Description)))
	}

	children := make([]render.HTML, 0, 6)
	if cfg.Status != "" {
		children = append(children, html.Div(html.DivConfig{Class: "ui-record-summary__status"}, cfg.Status))
	}

	lead := []render.HTML{html.Div(html.DivConfig{Class: "ui-record-summary__copy"}, copy...)}
	leadClass := "ui-record-summary__lead"
	if cfg.Aside != "" || cfg.Actions != "" {
		leadClass += " ui-record-summary__lead--with-support"
		support := make([]render.HTML, 0, 2)
		if cfg.Aside != "" {
			support = append(support, html.Div(html.DivConfig{Class: "ui-record-summary__aside"}, cfg.Aside))
		}
		if cfg.Actions != "" {
			support = append(support, html.Div(html.DivConfig{Class: "ui-record-summary__actions"}, cfg.Actions))
		}
		lead = append(lead, html.Div(html.DivConfig{Class: "ui-record-summary__support"}, support...))
	}
	children = append(children, html.Div(html.DivConfig{Class: leadClass}, lead...))
	if cfg.Highlight != "" {
		children = append(children, html.Div(html.DivConfig{Class: "ui-record-summary__highlight"}, cfg.Highlight))
	}
	if cfg.Metrics != "" {
		children = append(children, html.Div(html.DivConfig{Class: "ui-record-summary__metrics"}, cfg.Metrics))
	}
	if cfg.Footer != "" {
		children = append(children, render.Tag("footer", map[string]string{"class": "ui-record-summary__footer"},
			html.Div(html.DivConfig{Class: "ui-record-summary__footer-copy"}, cfg.Footer)))
	}

	cls := "ui-record-summary"
	if cfg.Tone != RecordSummaryToneNeutral {
		cls += " ui-record-summary--" + string(cfg.Tone)
	}
	if cfg.Class != "" {
		cls += " " + cfg.Class
	}
	attrs := map[string]string{"class": cls}
	if cfg.ID != "" {
		attrs["id"] = cfg.ID
	}
	return recordSummaryStyle.WrapHTML(render.Tag("article", attrs, children...))
}

// MetricBandItem is one compact label/value signal.
type MetricBandItem struct {
	Label string
	Value string
	Hint  string
}

// MetricBandConfig configures a flat band of one to six related signals.
type MetricBandConfig struct {
	Items []MetricBandItem
	Label string
	ID    string
	Class string
}

// MetricBand renders a semantic <dl>. Wide viewports use one row; phones use
// two columns. When the phone grid has an odd item count, the final signal
// spans the row instead of leaving an accidental empty quadrant. It is
// intentionally flatter than a grid of StatCards.
func MetricBand(cfg MetricBandConfig) render.HTML {
	if len(cfg.Items) < 1 || len(cfg.Items) > 6 {
		panic("ui: MetricBand requires 1-6 Items")
	}
	items := make([]render.HTML, 0, len(cfg.Items))
	for _, item := range cfg.Items {
		if item.Label == "" || item.Value == "" {
			panic("ui: MetricBand items require Label and Value")
		}
		parts := []render.HTML{
			render.Tag("dt", map[string]string{"class": "ui-metric-band__label"}, render.Text(item.Label)),
			render.Tag("dd", map[string]string{"class": "ui-metric-band__value"}, render.Text(item.Value)),
		}
		if item.Hint != "" {
			parts = append(parts, render.Tag("dd", map[string]string{"class": "ui-metric-band__hint"}, render.Text(item.Hint)))
		}
		items = append(items, html.Div(html.DivConfig{Class: "ui-metric-band__item"}, parts...))
	}
	cls := "ui-metric-band ui-metric-band--" + strconv.Itoa(len(cfg.Items))
	if cfg.Class != "" {
		cls += " " + cfg.Class
	}
	attrs := map[string]string{"class": cls}
	if cfg.ID != "" {
		attrs["id"] = cfg.ID
	}
	if cfg.Label != "" {
		attrs["aria-label"] = cfg.Label
	}
	return metricBandStyle.WrapHTML(render.Tag("dl", attrs, items...))
}

var recordSummaryStyle = registry.RegisterStyle("ui-record-summary", recordSummaryCSS)
var metricBandStyle = registry.RegisterStyle("ui-metric-band", metricBandCSS)

func recordSummaryCSS(t style.Theme) string {
	return style.NewComponentSheet("ui-record-summary", t).
		Rule("&").Set(
		"display", "grid",
		"gap", "var(--spacing-lg, 24px)",
		"padding", "clamp(var(--spacing-lg, 24px), 4vw, var(--spacing-2xl, 40px))",
		"background", "var(--color-surface, #fff)",
		"border", "1px solid var(--color-border, #e4e4e7)",
		"border-inline-start", "4px solid var(--ui-record-summary-accent, var(--color-primary, currentColor))",
		"border-radius", "var(--radii-lg, 12px)",
	).End().
		Rule("&.ui-record-summary--info").Set("--ui-record-summary-accent", "var(--color-info, var(--color-primary, currentColor))").End().
		Rule("&.ui-record-summary--success").Set("--ui-record-summary-accent", "var(--color-success, var(--color-primary, currentColor))").End().
		Rule("&.ui-record-summary--warning").Set("--ui-record-summary-accent", "var(--color-warning, var(--color-primary, currentColor))").End().
		Rule("&.ui-record-summary--danger").Set("--ui-record-summary-accent", "var(--color-danger, var(--color-primary, currentColor))").End().
		Rule(".ui-record-summary__status").Set("display", "flex", "align-items", "center", "min-inline-size", "0").End().
		Rule(".ui-record-summary__lead").Set("display", "grid", "gap", "var(--spacing-lg, 24px)", "min-inline-size", "0").End().
		Rule(".ui-record-summary__lead--with-support").Set("grid-template-columns", "minmax(0, 1fr) minmax(15rem, 0.55fr)", "align-items", "start").End().
		Rule(".ui-record-summary__copy").Set("display", "grid", "gap", "var(--spacing-sm, 8px)", "max-inline-size", "54rem").End().
		Rule(".ui-record-summary__support").Set("display", "grid", "gap", "var(--spacing-md, 16px)", "min-inline-size", "0", "padding-inline-start", "var(--spacing-lg, 24px)", "border-inline-start", "1px solid var(--color-border, #e4e4e7)", "justify-items", "start").End().
		Rule(".ui-record-summary__aside").Set("min-inline-size", "0", "max-inline-size", "100%", "color", "var(--color-text-muted, currentColor)").End().
		Rule(".ui-record-summary__eyebrow").Set("margin", "0", "font-size", "var(--text-xs, 0.75rem)", "font-weight", "700", "letter-spacing", "0.08em", "text-transform", "uppercase", "color", "var(--color-text-subtle, currentColor)").End().
		Rule(".ui-record-summary__title").Set("margin", "0", "max-inline-size", "24ch", "font-size", "var(--ui-record-summary-title-size, var(--text-4xl, 2.25rem))", "line-height", "1.08", "letter-spacing", "-0.025em", "color", "var(--color-text, currentColor)").End().
		Rule(".ui-record-summary__description").Set("margin", "0", "max-inline-size", "64ch", "color", "var(--color-text-muted, currentColor)", "line-height", "1.6").End().
		Rule(".ui-record-summary__highlight").Set("min-inline-size", "0").End().
		Rule(".ui-record-summary__metrics").Set("min-inline-size", "0").End().
		Rule(".ui-record-summary__footer").Set("display", "flex", "flex-wrap", "wrap", "align-items", "center", "justify-content", "space-between", "gap", "var(--spacing-md, 16px)", "padding-block-start", "var(--spacing-md, 16px)", "border-block-start", "1px solid var(--color-border, #e4e4e7)").End().
		Rule(".ui-record-summary__footer-copy").Set("min-inline-size", "0", "color", "var(--color-text-muted, currentColor)").End().
		Rule(".ui-record-summary__actions").Set("display", "flex", "flex-wrap", "wrap", "align-items", "center", "gap", "var(--spacing-sm, 8px)", "inline-size", "fit-content", "max-inline-size", "100%").End().
		Rule(".ui-record-summary__actions > [data-fui-comp=\"ui-layout\"]").Set("max-inline-size", "100%").End().
		Media("(max-width: 720px)", func(s *style.ComponentSheet) {
			s.Rule("&").Set("gap", "var(--spacing-md, 16px)", "padding", "var(--spacing-lg, 20px)").End()
			s.Rule(".ui-record-summary__lead--with-support").Set("grid-template-columns", "minmax(0, 1fr)", "gap", "var(--spacing-md, 16px)").End()
			s.Rule(".ui-record-summary__support").Set("padding-inline-start", "0", "padding-block-start", "var(--spacing-md, 16px)", "border-inline-start", "0", "border-block-start", "1px solid var(--color-border, #e4e4e7)").End()
			s.Rule(".ui-record-summary__actions").Set("order", "-1", "inline-size", "100%").End()
			s.Rule(".ui-record-summary__title").Set("font-size", "var(--ui-record-summary-title-size-mobile, var(--text-2xl, 1.5rem))", "max-inline-size", "18ch").End()
			s.Rule(".ui-record-summary__footer").Set("align-items", "flex-start", "flex-direction", "column").End()
		}).
		MustBuild()
}

func metricBandCSS(t style.Theme) string {
	return style.NewComponentSheet("ui-metric-band", t).
		Rule("&").Set("display", "grid", "margin", "0", "padding", "0", "border-block", "1px solid var(--color-border, #e4e4e7)").End().
		Rule("&.ui-metric-band--1").Set("grid-template-columns", "1fr").End().
		Rule("&.ui-metric-band--2").Set("grid-template-columns", "repeat(2, minmax(0, 1fr))").End().
		Rule("&.ui-metric-band--3").Set("grid-template-columns", "repeat(3, minmax(0, 1fr))").End().
		Rule("&.ui-metric-band--4").Set("grid-template-columns", "repeat(4, minmax(0, 1fr))").End().
		Rule("&.ui-metric-band--5").Set("grid-template-columns", "repeat(5, minmax(0, 1fr))").End().
		Rule("&.ui-metric-band--6").Set("grid-template-columns", "repeat(6, minmax(0, 1fr))").End().
		Rule(".ui-metric-band__item").Set("display", "flex", "flex-direction", "column", "gap", "var(--spacing-xs, 4px)", "min-inline-size", "0", "padding", "var(--spacing-md, 16px)", "border-inline-start", "1px solid var(--color-border, #e4e4e7)").End().
		Rule(".ui-metric-band__item:first-child").Set("border-inline-start", "0").End().
		Rule(".ui-metric-band__label").Set("order", "2", "font-size", "var(--text-xs, 0.75rem)", "font-weight", "600", "letter-spacing", "0.04em", "text-transform", "uppercase", "color", "var(--color-text-subtle, currentColor)").End().
		Rule(".ui-metric-band__value").Set("order", "1", "margin", "0", "font-size", "var(--text-lg, 1.125rem)", "font-weight", "700", "font-variant-numeric", "tabular-nums", "color", "var(--color-text, currentColor)").End().
		Rule(".ui-metric-band__hint").Set("order", "3", "margin", "0", "font-size", "var(--text-xs, 0.75rem)", "color", "var(--color-text-muted, currentColor)").End().
		Media("(max-width: 720px)", func(s *style.ComponentSheet) {
			s.Rule("&.ui-metric-band--2, &.ui-metric-band--3, &.ui-metric-band--4, &.ui-metric-band--5, &.ui-metric-band--6").Set("grid-template-columns", "repeat(2, minmax(0, 1fr))").End()
			s.Rule(".ui-metric-band__item").Set("border-inline-start", "0", "border-block-start", "1px solid var(--color-border, #e4e4e7)").End()
			// Column divider for the two-up phone grid. A single-item band's
			// sole item is odd AND full-width — no divider to paint.
			s.Rule("&:not(.ui-metric-band--1) .ui-metric-band__item:nth-child(odd)").Set("border-inline-end", "1px solid var(--color-border, #e4e4e7)").End()
			s.Rule(".ui-metric-band__item:first-child, .ui-metric-band__item:nth-child(2)").Set("border-block-start", "0").End()
			s.Rule("&.ui-metric-band--3 .ui-metric-band__item:last-child, &.ui-metric-band--5 .ui-metric-band__item:last-child").Set("grid-column", "1 / -1", "align-items", "center", "text-align", "center", "border-inline-end", "0").End()
		}).
		MustBuild()
}
