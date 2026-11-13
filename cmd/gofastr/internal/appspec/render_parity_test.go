package appspec

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

func parityPanic(fn func()) (message string) {
	defer func() {
		if p := recover(); p != nil {
			message = fmt.Sprint(p)
		}
	}()
	fn()
	return ""
}

// These calls mirror appgen's renderAppSpecCatalogBlock and control emitter.
// Validation cannot share their Go constructors: it must report a spec path
// before emitting Go. Each row checks both independent owners, so removing
// either precondition fails. Discovery below makes a new catalog kind choose
// a panic case or explain why its emitted config cannot panic.
func TestRenderPanicParity(t *testing.T) {
	ctx := context.Background()
	type example struct {
		name      string
		block     AppSpecBlock
		construct func(AppSpecBlock)
		panicWant string
	}
	b := func(kind string, props map[string]any) AppSpecBlock { return AppSpecBlock{Kind: kind, Props: props} }
	stat := func(b AppSpecBlock) {
		ui.StatCard(ui.StatCardConfig{Label: AppSpecProp(b, "label"), Value: AppSpecProp(b, "value")})
	}
	linkButton := func(b AppSpecBlock) {
		ui.LinkButton(ui.LinkButtonConfig{Label: AppSpecProp(b, "label"), Href: AppSpecProp(b, "href"), Variant: ui.ButtonPrimary})
	}
	hero := func(b AppSpecBlock) {
		var actions []render.HTML
		for _, pair := range [][2]string{{"cta_text", "cta_href"}, {"secondary_text", "secondary_href"}} {
			if label := AppSpecProp(b, pair[0]); label != "" {
				actions = append(actions, ui.LinkButton(ui.LinkButtonConfig{Label: label, Href: AppSpecProp(b, pair[1]), Variant: ui.ButtonPrimary}))
			}
		}
		ui.Hero(ui.HeroConfig{Title: AppSpecProp(b, "title"), Actions: actions})
	}
	pricing := func(b AppSpecBlock) {
		plans, _ := b.Props["plans"].([]any)
		for _, plan := range plans {
			p := AppSpecBlock{Props: plan.(map[string]any)}
			ui.PricingCard(ui.PricingCardConfig{Ctx: ctx, Name: AppSpecProp(p, "name"), Price: AppSpecProp(p, "price"), Period: AppSpecProp(p, "period"), Description: AppSpecProp(p, "description"), CTALabel: AppSpecProp(p, "cta_text"), CTAHref: AppSpecProp(p, "cta_href")})
		}
	}
	form := func(b AppSpecBlock) {
		ui.Form(ui.FormConfig{Ctx: ctx, Action: AppSpecProp(b, "action"), Method: strings.ToUpper(AppSpecProp(b, "method"))})
	}
	table := func(b AppSpecBlock) {
		columns, _ := AppSpecStringList(b, "columns")
		var cols []ui.Column
		for i, c := range columns {
			cols = append(cols, ui.Column{Key: fmt.Sprintf("c%d", i), Header: c})
		}
		ui.DataTable(ui.DataTableConfig{Ctx: ctx, Columns: cols, Caption: AppSpecProp(b, "caption"), CaptionHidden: AppSpecBoolProp(b, "caption_hidden")})
	}
	cases := []example{
		{"stat_label", b("stat_card", map[string]any{"value": "1"}), stat, "requires Label"},
		{"stat_value", b("stat_card", map[string]any{"label": "Count"}), stat, "requires Value"},
		{"link_label", b("link_button", map[string]any{"href": "/"}), linkButton, "requires Label"},
		{"link_href", b("link_button", map[string]any{"label": "Go"}), linkButton, "requires Href"},
		{"link_unsafe", b("link_button", map[string]any{"label": "Go", "href": "javascript:alert(1)"}), linkButton, "unsafe Href"},
		{"link_relative_host", b("link_button", map[string]any{"label": "Go", "href": "//evil.example"}), linkButton, "protocol-relative"},
		{"hero_cta", b("hero", map[string]any{"cta_text": "Go"}), hero, "requires Href"},
		{"hero_secondary", b("hero", map[string]any{"secondary_text": "Go", "secondary_href": "javascript:bad"}), hero, "unsafe Href"},
		{"pricing_cta", b("pricing", map[string]any{"plans": []any{map[string]any{"name": "Pro", "cta_text": "Buy"}}}), pricing, "requires Href"},
		{"pricing_unsafe", b("pricing", map[string]any{"plans": []any{map[string]any{"name": "Pro", "cta_href": "javascript:bad"}}}), pricing, "unsafe Href"},
		{"page_title", b("page_header", nil), func(b AppSpecBlock) {
			ui.PageHeader(ui.PageHeaderConfig{Title: AppSpecProp(b, "title"), Subtitle: AppSpecProp(b, "subtitle"), Eyebrow: AppSpecProp(b, "eyebrow")})
		}, "PageHeader requires Title"},
		{"callout_empty", b("callout", nil), func(b AppSpecBlock) {
			ui.Callout(ui.CalloutConfig{Title: AppSpecProp(b, "title")}, render.Text(b.Text))
		}, "Alert requires"},
		{"markdown_empty", b("markdown", nil), func(b AppSpecBlock) { ui.Markdown(ui.MarkdownConfig{Ctx: ctx, Source: b.Text, Measure: true}) }, "requires Source"},
		{"action_label", b("action_button", nil), func(b AppSpecBlock) {
			ui.Button(ui.ButtonConfig{Ctx: ctx, Label: AppSpecProp(b, "label"), Variant: ui.ButtonPrimary, Type: "button"})
		}, "requires Label"},
		{"form_action", b("custom_form", nil), form, "requires Action"},
		{"form_method", b("custom_form", map[string]any{"action": "/save", "method": "DELETE"}), form, "Method must be GET or POST"},
		{"form_unsafe", b("custom_form", map[string]any{"action": "javascript:bad"}), form, "not a URL"},
		{"table_columns", b("data_table", nil), table, "requires at least one Column"},
		{"table_caption", b("data_table", map[string]any{"columns": []any{"Name"}, "caption_hidden": true}), table, "CaptionHidden"},
		{"nav_label", b("nav_links", nil), func(b AppSpecBlock) {
			html.Nav(html.NavConfig{Label: AppSpecProp(b, "label")}, ui.Cluster(ui.ClusterConfig{}))
		}, "Nav requires Label"},
		{"heading_level", AppSpecBlock{Type: "heading", Text: "Title", Level: 7}, func(b AppSpecBlock) { html.Heading(html.HeadingConfig{Level: b.Level}, render.Text(b.Text)) }, "Level"},
		{"image_src", AppSpecBlock{Kind: "image"}, func(b AppSpecBlock) {
			html.Image(html.ImageConfig{Src: AppSpecProp(b, "src"), Alt: AppSpecProp(b, "alt")})
		}, "Image requires Src"},
		{"anchor_href", AppSpecBlock{Type: "link", Text: "Go"}, func(b AppSpecBlock) { ui.Link(ui.LinkConfig{Href: b.Href, Text: b.Text}) }, "requires Href"},
	}
	fields := map[string]func(AppSpecBlock){
		"text_field": func(b AppSpecBlock) {
			ui.TextField(ui.TextFieldConfig{Ctx: ctx, Name: AppSpecProp(b, "name"), Label: AppSpecProp(b, "label")})
		},
		"number_field": func(b AppSpecBlock) {
			ui.NumberField(ui.NumberFieldConfig{Ctx: ctx, Name: AppSpecProp(b, "name"), Label: AppSpecProp(b, "label")})
		},
		"checkbox": func(b AppSpecBlock) {
			ui.Checkbox(ui.ToggleConfig{Ctx: ctx, Name: AppSpecProp(b, "name"), Label: AppSpecProp(b, "label")})
		},
		"switch": func(b AppSpecBlock) {
			ui.Switch(ui.ToggleConfig{Ctx: ctx, Name: AppSpecProp(b, "name"), Label: AppSpecProp(b, "label")})
		},
		"select_field": func(b AppSpecBlock) {
			choices, _ := AppSpecChoices(b)
			var options []ui.SelectOption
			for _, c := range choices {
				options = append(options, ui.SelectOption{Value: c.Value, Text: c.Label, Selected: c.On})
			}
			ui.Select(ui.SelectConfig{Ctx: ctx, Name: AppSpecProp(b, "name"), Label: AppSpecProp(b, "label"), Options: options})
		},
		"radio_group": func(b AppSpecBlock) {
			choices, _ := AppSpecChoices(b)
			var options []ui.RadioGroupOption
			for _, c := range choices {
				options = append(options, ui.RadioGroupOption{Value: c.Value, Label: c.Label, Checked: c.On})
			}
			ui.RadioGroup(ui.RadioGroupConfig{Ctx: ctx, Name: AppSpecProp(b, "name"), Legend: AppSpecProp(b, "legend"), Options: options})
		},
	}
	for _, kind := range slices.Sorted(maps.Keys(fields)) {
		label, panicLabel := "label", "Label"
		if kind == "radio_group" {
			label, panicLabel = "legend", "Legend"
		}
		cases = append(cases,
			example{kind + "_name", b(kind, map[string]any{label: "Choice", "options": []any{"One"}}), fields[kind], "requires Name"},
			example{kind + "_label", b(kind, map[string]any{"name": "choice", "options": []any{"One"}}), fields[kind], "requires " + panicLabel},
		)
	}
	noRenderPanic := map[string]string{
		"stack":      "emitter maps gap and axes to known constants",
		"cluster":    "emitter maps gap and axes to known constants",
		"grid":       "emitted Grid has no required content",
		"stat_grid":  "same Grid composition as grid",
		"stat_row":   "Grid composition with a fixed minimum",
		"section":    "Section accepts an empty heading and body",
		"card":       "Card accepts empty content and the emitter fixes its heading level",
		"divider":    "zero-value Divider is valid",
		"bar_chart":  "source is checked at boot; chart accepts empty data",
		"pie_chart":  "source is checked at boot; chart accepts empty data",
		"line_chart": "source is checked at boot; chart accepts empty data",
		"item_list":  "native lists accept empty content",
	}
	covered := map[string]bool{}
	for _, tc := range cases {
		kind := tc.block.Kind
		if kind == "" {
			kind = tc.block.Type
		}
		covered[kind] = true
		t.Run(tc.name, func(t *testing.T) {
			bp := AppSpec{App: AppSpecApp{Name: "Demo"}, Screens: []AppSpecScreen{{Name: "parity", Route: "/", Body: []AppSpecBlock{tc.block}}}}
			if err := ValidateAppSpec(bp); err == nil || !strings.Contains(err.Error(), `screen "parity"`) {
				t.Errorf("validate accepted render-panic case %s or lost its spec path: %v", tc.name, err)
			}
			if got := parityPanic(func() { tc.construct(tc.block) }); !strings.Contains(got, tc.panicWant) {
				t.Errorf("constructor panic = %q; want %q", got, tc.panicWant)
			}
		})
	}
	for _, kinds := range []map[string]bool{appSpecCatalogKinds, appSpecControlKinds} {
		for _, kind := range slices.Sorted(maps.Keys(kinds)) {
			if !covered[kind] && noRenderPanic[kind] == "" {
				t.Errorf("catalog kind %q needs a render-panic case or a noRenderPanic reason", kind)
			}
		}
	}
}
