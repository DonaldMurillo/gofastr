package ui

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
)

func TestRecordSummaryRendersBoundedCompositionSlots(t *testing.T) {
	h := string(RecordSummary(RecordSummaryConfig{
		Title:        "API latency regression",
		Eyebrow:      "INC-2841 · Payments",
		Description:  "Elevated latency is affecting card authorizations.",
		Status:       render.Raw(`<span>SEV-1 active</span>`),
		Highlight:    render.Raw(`<aside>Next decision at 14:30</aside>`),
		Metrics:      render.Raw(`<dl>signals</dl>`),
		Aside:        render.Raw(`<div>Bridge roster</div>`),
		Footer:       render.Raw(`<span>Commander: Mina</span>`),
		Actions:      render.Raw(`<a href="/incident">Open incident</a>`),
		Tone:         RecordSummaryToneDanger,
		HeadingLevel: 2,
		ID:           "incident-summary",
		Class:        "extra",
	}))

	for _, want := range []string{
		`data-fui-comp="ui-record-summary"`,
		`class="ui-record-summary ui-record-summary--danger extra"`,
		`id="incident-summary"`,
		`<h2 class="ui-record-summary__title"`,
		`>API latency regression</h2>`,
		`ui-record-summary__status`,
		`Next decision at 14:30`,
		`ui-record-summary__metrics`,
		`ui-record-summary__aside`,
		`Bridge roster`,
		`ui-record-summary__lead--with-support`,
		`Commander: Mina`,
		`ui-record-summary__actions`,
		`Open incident`,
	} {
		if !strings.Contains(h, want) {
			t.Errorf("RecordSummary missing %q\nhtml=%s", want, h)
		}
	}
}

func TestRecordSummaryDefaultsToH1AndNeutralTone(t *testing.T) {
	h := string(RecordSummary(RecordSummaryConfig{Title: "Overview"}))
	if !strings.Contains(h, `<h1 class="ui-record-summary__title"`) || !strings.Contains(h, `>Overview</h1>`) {
		t.Fatalf("default heading must be h1:\n%s", h)
	}
	if strings.Contains(h, "ui-record-summary--") {
		t.Fatalf("neutral summary must not add a tone variant:\n%s", h)
	}
}

func TestRecordSummaryCSSControlsMobileScaleAndActionWidth(t *testing.T) {
	css := recordSummaryCSS(style.Theme{})
	for _, want := range []string{
		`[data-fui-comp="ui-record-summary"]`,
		`border-inline-start: 4px solid`,
		`inline-size: fit-content`,
		`grid-template-columns: minmax(0, 1fr) minmax(15rem, 0.55fr)`,
		`border-inline-start: 1px solid var(--color-border, #e4e4e7)`,
		`@media (max-width: 720px)`,
		`font-size: var(--ui-record-summary-title-size-mobile, var(--text-2xl, 1.5rem))`,
		`order: -1`,
	} {
		if !strings.Contains(css, want) {
			t.Errorf("RecordSummary CSS missing %q\ncss=%s", want, css)
		}
	}
}

func TestRecordSummaryRejectsInvalidConfig(t *testing.T) {
	for name, fn := range map[string]func(){
		"missing title": func() { RecordSummary(RecordSummaryConfig{}) },
		"bad tone":      func() { RecordSummary(RecordSummaryConfig{Title: "x", Tone: "loud"}) },
		"bad heading":   func() { RecordSummary(RecordSummaryConfig{Title: "x", HeadingLevel: 7}) },
	} {
		t.Run(name, func(t *testing.T) { assertUIPanics(t, fn) })
	}
}

func TestMetricBandRendersSemanticSignals(t *testing.T) {
	h := string(MetricBand(MetricBandConfig{
		Label: "Incident signals",
		Items: []MetricBandItem{
			{Label: "Impact", Value: "32%", Hint: "checkout requests"},
			{Label: "Started", Value: "13:42 UTC"},
			{Label: "Owner", Value: "Payments"},
		},
	}))
	for _, want := range []string{
		`data-fui-comp="ui-metric-band"`,
		`class="ui-metric-band ui-metric-band--3"`,
		`aria-label="Incident signals"`,
		`<dt class="ui-metric-band__label">Impact</dt>`,
		`<dd class="ui-metric-band__value">32%</dd>`,
		`<dd class="ui-metric-band__hint">checkout requests</dd>`,
	} {
		if !strings.Contains(h, want) {
			t.Errorf("MetricBand missing %q\nhtml=%s", want, h)
		}
	}
}

func TestMetricBandCSSIsFlatWideAndTwoColumnMobile(t *testing.T) {
	css := metricBandCSS(style.Theme{})
	for _, want := range []string{
		`grid-template-columns: repeat(4, minmax(0, 1fr))`,
		`@media (max-width: 720px)`,
		`grid-template-columns: repeat(2, minmax(0, 1fr))`,
		`.ui-metric-band--3 .ui-metric-band__item:last-child`,
		`.ui-metric-band--5 .ui-metric-band__item:last-child`,
		`grid-column: 1 / -1`,
		`text-align: center`,
		`font-variant-numeric: tabular-nums`,
		// The phone divider between the two columns must not paint on a
		// single-item band, whose sole (odd) item spans the full width.
		`:not(.ui-metric-band--1) .ui-metric-band__item:nth-child(odd)`,
	} {
		if !strings.Contains(css, want) {
			t.Errorf("MetricBand CSS missing %q\ncss=%s", want, css)
		}
	}
}

func TestMetricBandRejectsInvalidItems(t *testing.T) {
	tooMany := make([]MetricBandItem, 7)
	for i := range tooMany {
		tooMany[i] = MetricBandItem{Label: "x", Value: "y"}
	}
	for name, fn := range map[string]func(){
		"empty":         func() { MetricBand(MetricBandConfig{}) },
		"too many":      func() { MetricBand(MetricBandConfig{Items: tooMany}) },
		"missing label": func() { MetricBand(MetricBandConfig{Items: []MetricBandItem{{Value: "x"}}}) },
		"missing value": func() { MetricBand(MetricBandConfig{Items: []MetricBandItem{{Label: "x"}}}) },
	} {
		t.Run(name, func(t *testing.T) { assertUIPanics(t, fn) })
	}
}

func assertUIPanics(t *testing.T, fn func()) {
	t.Helper()
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic")
		}
	}()
	fn()
}
