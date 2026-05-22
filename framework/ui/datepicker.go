package ui

import (
	"fmt"
	"strings"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// DatePickerConfig configures a calendar-based date picker.
type DatePickerConfig struct {
	Name        string
	Label       string
	ID          string
	Value       string
	Min         string
	Max         string
	Required    bool
	Placeholder string
	HelpText    string
	Locale      string
}

// DatePicker renders a date picker with calendar popup.
func DatePicker(cfg DatePickerConfig) render.HTML {
	if cfg.ID == "" {
		cfg.ID = autoID("dp")
	}
	if cfg.Locale == "" {
		cfg.Locale = "en"
	}

	popupID := cfg.ID + "-calendar"

	var children []render.HTML

	// Label (only if provided)
	if cfg.Label != "" {
		children = append(children, html.Label(html.LabelConfig{
			For:  cfg.ID,
			Text: cfg.Label,
		}))
	}

	// Display input (text, readonly)
	displayInput := html.Input(html.InputConfig{
		Type:  "text",
		Name:  cfg.Name + "_display",
		ID:    cfg.ID,
		Class: "ui-datepicker-display",
		Attrs: map[string]string{"readonly": "", "placeholder": cfg.Placeholder},
	})

	// Hidden input (form submission)
	hiddenAttrs := map[string]string{}
	if cfg.Value != "" {
		hiddenAttrs["value"] = cfg.Value
	}
	if cfg.Min != "" {
		hiddenAttrs["min"] = cfg.Min
	}
	if cfg.Max != "" {
		hiddenAttrs["max"] = cfg.Max
	}
	hiddenInput := html.Input(html.InputConfig{
		Type:  "hidden",
		Name:  cfg.Name,
		ID:    cfg.ID + "-value",
		Class: "ui-datepicker-value",
		Attrs: hiddenAttrs,
	})

	// Trigger button
	trigger := html.Button(html.ButtonConfig{
		Label: "📅",
		Type:  "button",
		Attrs: map[string]string{
			"data-fui-open":           popupID,
			"data-fui-popover-anchor": "bottom",
			"aria-label":              "Open calendar",
			"class":                   "ui-datepicker-trigger",
		},
	})

	children = append(children, html.Div(html.DivConfig{Class: "ui-datepicker-wrapper"},
		displayInput,
		hiddenInput,
		trigger,
	))

	// Calendar grid
	calendarGrid := renderCalendarGrid()
	children = append(children, html.Div(html.DivConfig{
		Class: "ui-datepicker-calendar",
		Attrs: map[string]string{"id": popupID, "hidden": ""},
	}, calendarGrid))

	// Help text
	if cfg.HelpText != "" {
		children = append(children, html.Span(html.TextConfig{
			Class: "ui-help-text",
		}, render.HTML(cfg.HelpText)))
	}

	return datepickerStyle.WrapHTML(html.Div(html.DivConfig{Class: "ui-datepicker"}, children...))
}

// renderCalendarGrid returns a static calendar grid skeleton.
// The runtime populates actual dates via RPC.
func renderCalendarGrid() render.HTML {
	days := []string{"Su", "Mo", "Tu", "We", "Th", "Fr", "Sa"}
	var headerCells []string
	for _, d := range days {
		headerCells = append(headerCells, fmt.Sprintf(`<th scope="col" class="ui-datepicker-day-name">%s</th>`, d))
	}

	var bodyRows []string
	for week := 0; week < 6; week++ {
		var cells []string
		for day := 0; day < 7; day++ {
			cells = append(cells, `<td class="ui-datepicker-day"><button type="button" class="ui-datepicker-day-btn" disabled>-</button></td>`)
		}
		bodyRows = append(bodyRows, "<tr>"+strings.Join(cells, "")+"</tr>")
	}

	table := fmt.Sprintf(`<table class="ui-datepicker-grid" role="grid"><thead><tr>%s</tr></thead><tbody>%s</tbody></table>`,
		strings.Join(headerCells, ""),
		strings.Join(bodyRows, ""))

	nav := `<div class="ui-datepicker-nav">
		<button type="button" class="ui-datepicker-prev" aria-label="Previous month">‹</button>
		<span class="ui-datepicker-month-year">Select a date</span>
		<button type="button" class="ui-datepicker-next" aria-label="Next month">›</button>
	</div>`

	return render.HTML(nav + table)
}

var datepickerStyle = registry.RegisterStyle("ui-datepicker", datepickerCSS)

func datepickerCSS(t style.Theme) string {
	return `.ui-datepicker { display: flex; flex-direction: column; gap: var(--spacing-xs); }
.ui-datepicker-wrapper { display: flex; position: relative; gap: var(--spacing-xs); align-items: center; }
.ui-datepicker-wrapper input { flex: 1; }
.ui-datepicker-trigger { padding: var(--spacing-sm); background: none; border: 1px solid var(--color-border); border-radius: var(--radius-md); cursor: pointer; }
.ui-datepicker-calendar { position: absolute; top: 100%; left: 0; z-index: var(--z-popover); background: var(--color-surface); border: 1px solid var(--color-border); border-radius: var(--radius-md); box-shadow: var(--shadow-md); padding: var(--spacing-md); }
.ui-datepicker-nav { display: flex; justify-content: space-between; align-items: center; margin-bottom: var(--spacing-sm); }
.ui-datepicker-grid { width: 100%; border-collapse: collapse; }
.ui-datepicker-day-name { font-size: var(--text-xs); font-weight: var(--font-weight-bold, 600); color: var(--color-text-muted); padding: var(--spacing-xs); text-align: center; }
.ui-datepicker-day-btn { width: 2rem; height: 2rem; border: none; background: none; cursor: pointer; border-radius: var(--radius-sm); }
.ui-datepicker-day-btn:hover { background: var(--color-surface-secondary); }`
}
