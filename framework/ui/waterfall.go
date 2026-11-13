package ui

import (
	"context"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/i18nui"
)

// ─── Waterfall ──────────────────────────────────────────────────────
//
// The steps of one piece of work on a shared time axis: a request's
// database queries and outbound calls, a build's stages, a job's
// phases. Each row names a step, says when it started and how long it
// took, and draws that as a bar, so where the time went is visible at
// a glance. It is a table: the numbers are in cells assistive tech
// reads, and the bars are decoration beside them. Pure SVG bars, no
// script. Events with no duration are a Timeline, not this.

// WaterfallTone names the color a step's bar takes.
type WaterfallTone string

const (
	// WaterfallNeutral is the default bar.
	WaterfallNeutral WaterfallTone = ""
	// WaterfallWarning marks a step worth a look: a slow one.
	WaterfallWarning WaterfallTone = "warning"
	// WaterfallDanger marks a step that failed.
	WaterfallDanger WaterfallTone = "danger"
)

// waterfallMaxDepth is the deepest indent drawn; deeper steps sit at it.
const waterfallMaxDepth = 6

// WaterfallStep is one row.
type WaterfallStep struct {
	// Label names the step. Required.
	Label string
	// Detail is a muted line under the label: the query, the URL.
	Detail string
	// Start is when the step began, from the start of the whole.
	Start time.Duration
	// Dur is how long it took.
	Dur time.Duration
	// Depth indents the label: 0 for the whole, 1 for its steps, 2 for
	// theirs. Levels past six draw at six.
	Depth int
	// Tone colors the bar.
	Tone WaterfallTone
}

// WaterfallConfig configures a Waterfall.
type WaterfallConfig struct {
	// Label is the table's caption, read by assistive tech and hidden
	// from view. Required.
	Label string
	// Total is the width of the time axis. Zero takes the latest end
	// among Steps.
	Total time.Duration
	// Steps are the rows, in the order to draw them.
	Steps []WaterfallStep

	ID    string
	Class string
	// ExtraAttrs forwards additional attributes to the root. Keys the
	// component owns are dropped: class and id (use Class / ID) and
	// data-cui-*.
	ExtraAttrs html.Attrs
	// Ctx carries the request: the headings and the durations follow
	// the reader's locale. Nil writes English.
	Ctx context.Context
}

var waterfallTones = []WaterfallTone{WaterfallNeutral, WaterfallWarning, WaterfallDanger}

// DurationText writes d the way people read a latency: "412 µs",
// "3.1 ms", "1.24 s", with the number in ctx's locale. A negative d
// reads as zero.
func DurationText(ctx context.Context, d time.Duration) string {
	d = max(d, 0)
	switch {
	case d < time.Millisecond:
		return i18nui.DurationMicros.Text(ctx, map[string]any{"n": i18n.Number(ctx, d.Microseconds())})
	case d < time.Second:
		return i18nui.DurationMillis.Text(ctx, map[string]any{"n": i18n.Number(ctx, float64(d.Microseconds())/1e3, i18n.MaxFraction(1))})
	default:
		return i18nui.DurationSeconds.Text(ctx, map[string]any{"n": i18n.Number(ctx, d.Seconds(), i18n.MaxFraction(2))})
	}
}

// Waterfall renders the table of steps.
func Waterfall(cfg WaterfallConfig) render.HTML {
	if cfg.Label == "" {
		panic("ui: Waterfall needs a Label")
	}
	total := max(cfg.Total, 0)
	for _, s := range cfg.Steps {
		if s.Label == "" {
			panic("ui: Waterfall step needs a Label")
		}
		if !slices.Contains(waterfallTones, s.Tone) {
			panic("ui: Waterfall step needs one of the WaterfallTone constants, got " + strconv.Quote(string(s.Tone)))
		}
		if cfg.Total <= 0 {
			total = max(total, max(s.Start, 0)+max(s.Dur, 0))
		}
	}
	if len(cfg.Steps) == 0 {
		return chartEmpty(cfg.Ctx, cfg.ID, "", cfg.Class, i18nui.WaterfallEmpty.Text(cfg.Ctx), cfg.ExtraAttrs)
	}
	total = max(total, time.Nanosecond) // every step empty: one unit keeps the divide whole

	// The axis is 1000 units wide whatever the table's width: the SVG
	// stretches, so a bar's position is a number, never a style.
	const axis, minBar = 1000.0, 4.0
	var sb strings.Builder
	sb.WriteString(`<table data-cui-internal="" class="fui-waterfall__table"><caption class="fui-waterfall__caption">`)
	sb.WriteString(string(render.Text(cfg.Label)))
	sb.WriteString(`</caption><thead><tr><th scope="col">`)
	sb.WriteString(string(render.Text(i18nui.WaterfallStep.Text(cfg.Ctx))))
	sb.WriteString(`</th><th scope="col" class="fui-waterfall__num fui-waterfall__start">`)
	sb.WriteString(string(render.Text(i18nui.WaterfallStart.Text(cfg.Ctx))))
	sb.WriteString(`</th><th scope="col" class="fui-waterfall__num">`)
	sb.WriteString(string(render.Text(i18nui.WaterfallTook.Text(cfg.Ctx))))
	sb.WriteString(`</th><th scope="col" class="fui-waterfall__lane"><span class="fui-waterfall__caption">`)
	sb.WriteString(string(render.Text(i18nui.WaterfallTimeline.Text(cfg.Ctx))))
	sb.WriteString(`</span></th></tr></thead><tbody>`)
	for _, s := range cfg.Steps {
		start, dur := max(s.Start, 0), max(s.Dur, 0)
		w := min(max(float64(dur)/float64(total)*axis, minBar), axis)
		x := min(float64(start)/float64(total)*axis, axis-w)

		sb.WriteString(`<tr><th scope="row" class="fui-waterfall__step fui-waterfall__step--d`)
		sb.WriteString(strconv.Itoa(min(max(s.Depth, 0), waterfallMaxDepth)))
		sb.WriteString(`"><span class="fui-waterfall__name">`)
		sb.WriteString(string(render.Text(s.Label)))
		sb.WriteString(`</span>`)
		if s.Detail != "" {
			sb.WriteString(`<span class="fui-waterfall__detail">`)
			sb.WriteString(string(render.Text(s.Detail)))
			sb.WriteString(`</span>`)
		}
		sb.WriteString(`</th><td class="fui-waterfall__num fui-waterfall__start">`)
		sb.WriteString(string(render.Text(DurationText(cfg.Ctx, start))))
		sb.WriteString(`</td><td class="fui-waterfall__num">`)
		sb.WriteString(string(render.Text(DurationText(cfg.Ctx, dur))))
		sb.WriteString(`</td><td class="fui-waterfall__lane"><svg aria-hidden="true" focusable="false" class="fui-waterfall__svg" xmlns="http://www.w3.org/2000/svg" viewBox="0 0 1000 10" preserveAspectRatio="none"><rect class="fui-waterfall__track" x="0" y="0" width="1000" height="10"/><rect class="fui-waterfall__bar`)
		if s.Tone != WaterfallNeutral {
			sb.WriteString(` fui-waterfall__bar--`)
			sb.WriteString(string(s.Tone))
		}
		sb.WriteString(`" x="`)
		sb.WriteString(ftoa(x))
		sb.WriteString(`" y="0" width="`)
		sb.WriteString(ftoa(w))
		sb.WriteString(`" height="10"/></svg></td></tr>`)
	}
	sb.WriteString(`</tbody></table>`)

	attrs := html.SafeExtraAttrs(cfg.ExtraAttrs)
	if attrs == nil {
		attrs = html.Attrs{}
	}
	cls := "fui-waterfall"
	if cfg.Class != "" {
		cls += " " + cfg.Class
	}
	attrs["class"] = cls
	if cfg.ID != "" {
		attrs["id"] = cfg.ID
	}
	return waterfallStyle.WrapHTML(render.Tag("div", attrs, render.HTML(sb.String())))
}

var waterfallStyle = registry.RegisterStyle("ui-waterfall", waterfallCSS)

func waterfallCSS(_ style.Theme) string {
	// Knobs: --ui-waterfall-bar-size (0.75rem) is a bar's height,
	// --ui-waterfall-lane (45%) the share of the table the bars take,
	// --ui-waterfall-indent (the lg spacing step) one level of depth.
	return `[data-cui-comp="ui-waterfall"] {
  container-type: inline-size;
  font-size: var(--text-sm, 0.875rem);
}
[data-cui-comp="ui-waterfall"] .fui-waterfall__table {
  inline-size: 100%;
  border-collapse: collapse;
  table-layout: fixed;
}
[data-cui-comp="ui-waterfall"] .fui-waterfall__caption {
  position: absolute;
  inline-size: 1px;
  block-size: 1px;
  padding: 0;
  margin: -1px;
  overflow: hidden;
  clip: rect(0, 0, 0, 0);
  white-space: nowrap;
}
[data-cui-comp="ui-waterfall"] th,
[data-cui-comp="ui-waterfall"] td {
  padding: var(--spacing-sm, 4px) var(--spacing-md, 8px);
  border-bottom: var(--stroke-thin, 1px) solid var(--color-border, #E4E4E7);
  text-align: start;
  vertical-align: middle;
}
[data-cui-comp="ui-waterfall"] tbody tr:last-child > * { border-bottom: none; }
[data-cui-comp="ui-waterfall"] thead th {
  font-weight: var(--font-weight-medium);
  color: var(--color-text-muted, #52525B);
}
[data-cui-comp="ui-waterfall"] .fui-waterfall__num {
  inline-size: 6.5rem;
  text-align: end;
  white-space: nowrap;
  font-variant-numeric: tabular-nums;
}
[data-cui-comp="ui-waterfall"] .fui-waterfall__lane {
  inline-size: var(--ui-waterfall-lane, 45%);
}
[data-cui-comp="ui-waterfall"] .fui-waterfall__step {
  font-weight: var(--font-weight-medium);
  color: var(--color-text, #09090B);
  overflow-wrap: anywhere;
}
[data-cui-comp="ui-waterfall"] .fui-waterfall__step--d1 { padding-inline-start: calc(var(--spacing-md, 8px) + var(--ui-waterfall-indent, var(--spacing-lg, 16px))); }
[data-cui-comp="ui-waterfall"] .fui-waterfall__step--d2 { padding-inline-start: calc(var(--spacing-md, 8px) + 2 * var(--ui-waterfall-indent, var(--spacing-lg, 16px))); }
[data-cui-comp="ui-waterfall"] .fui-waterfall__step--d3 { padding-inline-start: calc(var(--spacing-md, 8px) + 3 * var(--ui-waterfall-indent, var(--spacing-lg, 16px))); }
[data-cui-comp="ui-waterfall"] .fui-waterfall__step--d4 { padding-inline-start: calc(var(--spacing-md, 8px) + 4 * var(--ui-waterfall-indent, var(--spacing-lg, 16px))); }
[data-cui-comp="ui-waterfall"] .fui-waterfall__step--d5 { padding-inline-start: calc(var(--spacing-md, 8px) + 5 * var(--ui-waterfall-indent, var(--spacing-lg, 16px))); }
[data-cui-comp="ui-waterfall"] .fui-waterfall__step--d6 { padding-inline-start: calc(var(--spacing-md, 8px) + 6 * var(--ui-waterfall-indent, var(--spacing-lg, 16px))); }
[data-cui-comp="ui-waterfall"] .fui-waterfall__name { display: block; }
[data-cui-comp="ui-waterfall"] .fui-waterfall__detail {
  display: block;
  font-weight: var(--font-weight-normal);
  font-size: var(--text-xs, 0.75rem);
  color: var(--color-text-muted, #52525B);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
[data-cui-comp="ui-waterfall"] .fui-waterfall__svg {
  display: block;
  inline-size: 100%;
  block-size: var(--ui-waterfall-bar-size, 0.75rem);
}
[data-cui-comp="ui-waterfall"] .fui-waterfall__track { fill: var(--color-surface-soft, #F4F4F5); }
[data-cui-comp="ui-waterfall"] .fui-waterfall__bar { fill: var(--color-primary, #18181B); }
[data-cui-comp="ui-waterfall"] .fui-waterfall__bar--warning { fill: var(--color-warning, #854D0E); }
[data-cui-comp="ui-waterfall"] .fui-waterfall__bar--danger { fill: var(--color-danger, #B91C1C); }
/* A narrow table keeps the name, how long, and the bar: when a step
   started is what the bar's position already says. */
@container (max-width: 36rem) {
  [data-cui-comp="ui-waterfall"] .fui-waterfall__start { display: none; }
  [data-cui-comp="ui-waterfall"] .fui-waterfall__num { inline-size: 4.5rem; }
  [data-cui-comp="ui-waterfall"] .fui-waterfall__lane { inline-size: var(--ui-waterfall-lane, 30%); }
}`
}
