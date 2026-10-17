package ui

import (
	"math"
	"slices"
	"strconv"
	"strings"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// ─── Gauge ──────────────────────────────────────────────────────────
//
// One reading against a range, drawn as a semicircle dial: CPU at 73%,
// p95 latency against its budget, disk used. The root is an ARIA meter
// carrying the range and the reading, so a screen reader announces
// "CPU, 73%" where a picture would say nothing. Bands color the arc by
// where the reading falls (green under 70, amber under 90, red above).
// Pure SVG, no script. A fraction of a task that finishes is Progress,
// not this.

// GaugeTone names the theme color the value arc takes.
type GaugeTone string

const (
	GaugePrimary GaugeTone = "primary"
	GaugeInfo    GaugeTone = "info"
	GaugeSuccess GaugeTone = "success"
	GaugeWarning GaugeTone = "warning"
	GaugeDanger  GaugeTone = "danger"
)

// GaugeBand colors the arc from From upward, until the next band's From.
type GaugeBand struct {
	From float64
	Tone GaugeTone
}

// GaugeConfig configures a Gauge.
type GaugeConfig struct {
	// Label names the gauge, visibly under the dial and as the meter's
	// accessible name. Required.
	Label string
	// Value is the reading. Outside [Min, Max] the arc stops at the
	// nearer end, and ValueText still says what was read. NaN or Inf
	// (a reading that never arrived) renders the empty state.
	Value float64
	// Min and Max bound the range. Both zero takes 0 to 100.
	Min, Max float64
	// ValueText is the reading as people read it ("73%", "412 ms"),
	// drawn in the dial and announced as aria-valuetext. Empty takes
	// Value as a plain number, to two decimals at most.
	ValueText string
	// Bands pick the arc's tone: the band with the highest From at or
	// below Value wins, in any order. No bands, or a Value under every
	// From, takes GaugePrimary.
	Bands []GaugeBand
	// Size is the dial's width in CSS pixels; the height is a little
	// over half of it, and the reading's text scales with it. Default 160.
	Size int

	ID    string
	Class string
	// ExtraAttrs forwards additional attributes to the root. Keys the
	// component owns are dropped: class and id (use Class / ID),
	// data-cui-*, role and the aria-label and aria-value* the meter
	// derives from config.
	ExtraAttrs html.Attrs
}

var gaugeTones = []GaugeTone{GaugePrimary, GaugeInfo, GaugeSuccess, GaugeWarning, GaugeDanger}

// Gauge renders the dial.
func Gauge(cfg GaugeConfig) render.HTML {
	if cfg.Label == "" {
		panic("ui: Gauge needs a Label")
	}
	lo, hi := cfg.Min, cfg.Max
	if lo == 0 && hi == 0 {
		hi = 100
	}
	if !finite(lo) || !finite(hi) || hi <= lo || !finite(hi-lo) {
		panic("ui: Gauge needs finite Min < Max")
	}
	for _, b := range cfg.Bands {
		if !finite(b.From) || !slices.Contains(gaugeTones, b.Tone) {
			panic("ui: Gauge band needs a finite From and one of the GaugeTone constants, got " + strconv.Quote(string(b.Tone)))
		}
	}
	if !finite(cfg.Value) {
		return chartEmpty(cfg.ID, "", cfg.Class, cfg.Label+": no data yet", cfg.ExtraAttrs)
	}

	v := min(max(cfg.Value, lo), hi)
	text := cfg.ValueText
	if text == "" {
		text = strings.TrimSuffix(strings.TrimRight(strconv.FormatFloat(cfg.Value, 'f', 2, 64), "0"), ".")
	}
	tone := GaugePrimary
	reached := math.Inf(-1)
	for _, b := range cfg.Bands {
		if b.From <= cfg.Value && b.From >= reached {
			tone, reached = b.Tone, b.From
		}
	}

	size := cfg.Size
	if size <= 0 {
		size = 160
	}
	s := float64(size)
	stroke := s / 10
	r := s/2 - stroke/2 - 1
	cx, cy := s/2, stroke/2+1+r
	h := cy + stroke/2 + 1

	var sb strings.Builder
	sb.WriteString(`<svg data-cui-internal="" aria-hidden="true" class="fui-gauge__dial" xmlns="http://www.w3.org/2000/svg" width="`)
	sb.WriteString(strconv.Itoa(size))
	sb.WriteString(`" height="`)
	sb.WriteString(ftoa(math.Ceil(h)))
	sb.WriteString(`" viewBox="0 0 `)
	sb.WriteString(strconv.Itoa(size))
	sb.WriteString(` `)
	sb.WriteString(ftoa(math.Ceil(h)))
	sb.WriteString(`">`)
	arc := func(class string, frac float64) {
		a := math.Pi * (1 + frac)
		sb.WriteString(`<path class="`)
		sb.WriteString(class)
		sb.WriteString(`" d="M`)
		sb.WriteString(ftoa(cx - r))
		sb.WriteString(`,`)
		sb.WriteString(ftoa(cy))
		sb.WriteString(` A`)
		sb.WriteString(ftoa(r))
		sb.WriteString(`,`)
		sb.WriteString(ftoa(r))
		sb.WriteString(` 0 0 1 `)
		sb.WriteString(ftoa(cx + r*math.Cos(a)))
		sb.WriteString(`,`)
		sb.WriteString(ftoa(cy + r*math.Sin(a)))
		sb.WriteString(`" stroke-width="`)
		sb.WriteString(ftoa(stroke))
		sb.WriteString(`"/>`)
	}
	arc("fui-gauge__track", 1)
	if frac := (v - lo) / (hi - lo); frac > 0 {
		arc("fui-gauge__value fui-gauge__value--"+string(tone), frac)
	}
	sb.WriteString(`<text class="fui-gauge__text" x="`)
	sb.WriteString(ftoa(cx))
	sb.WriteString(`" y="`)
	sb.WriteString(ftoa(cy - stroke/2))
	sb.WriteString(`" font-size="`)
	sb.WriteString(ftoa(s / 8))
	sb.WriteString(`" text-anchor="middle">`)
	sb.WriteString(escapeXML(text))
	sb.WriteString(`</text></svg>`)

	attrs := html.SafeExtraAttrs(cfg.ExtraAttrs,
		"role", "aria-label", "aria-valuemin", "aria-valuemax", "aria-valuenow", "aria-valuetext")
	if attrs == nil {
		attrs = html.Attrs{}
	}
	cls := "fui-gauge"
	if cfg.Class != "" {
		cls += " " + cfg.Class
	}
	attrs["class"] = cls
	if cfg.ID != "" {
		attrs["id"] = cfg.ID
	}
	attrs["role"] = "meter"
	attrs["aria-label"] = cfg.Label
	attrs["aria-valuemin"] = strconv.FormatFloat(lo, 'f', -1, 64)
	attrs["aria-valuemax"] = strconv.FormatFloat(hi, 'f', -1, 64)
	attrs["aria-valuenow"] = strconv.FormatFloat(v, 'f', -1, 64)
	attrs["aria-valuetext"] = text
	label := render.Tag("span", map[string]string{"class": "fui-gauge__label", "data-cui-internal": ""}, render.Text(cfg.Label))
	return gaugeStyle.WrapHTML(render.Tag("div", attrs, render.HTML(sb.String()), label))
}

func finite(f float64) bool { return !math.IsNaN(f) && !math.IsInf(f, 0) }

var gaugeStyle = registry.RegisterStyle("ui-gauge", gaugeCSS)

func gaugeCSS(_ style.Theme) string {
	return `[data-cui-comp="ui-gauge"] {
  display: inline-grid;
  justify-items: center;
  gap: var(--spacing-xs, 2px);
  vertical-align: middle;
}
[data-cui-comp="ui-gauge"] .fui-gauge__track {
  fill: none;
  stroke: var(--color-border, #E4E4E7);
  stroke-linecap: round;
}
[data-cui-comp="ui-gauge"] .fui-gauge__value {
  fill: none;
  stroke-linecap: round;
}
[data-cui-comp="ui-gauge"] .fui-gauge__value--primary { stroke: var(--color-primary, #18181B); }
[data-cui-comp="ui-gauge"] .fui-gauge__value--info    { stroke: var(--color-info, #3B82F6); }
[data-cui-comp="ui-gauge"] .fui-gauge__value--success { stroke: var(--color-success, #16A34A); }
[data-cui-comp="ui-gauge"] .fui-gauge__value--warning { stroke: var(--color-warning, #D97706); }
[data-cui-comp="ui-gauge"] .fui-gauge__value--danger  { stroke: var(--color-danger, #DC2626); }
[data-cui-comp="ui-gauge"] .fui-gauge__text {
  font-weight: var(--font-weight-bold);
  font-variant-numeric: tabular-nums;
  fill: var(--color-text, #18181B);
}
[data-cui-comp="ui-gauge"] .fui-gauge__label {
  font-size: var(--text-sm, 0.875rem);
  color: var(--color-text-muted, #52525B);
}`
}
