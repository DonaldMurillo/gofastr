// check-csp:ignore-file
//
// The editor's per-session bootstrap script (theme_edit_page.js) is inlined
// in the controls page; nothing else in this file ships script or CSS.

package themecmd

import (
	_ "embed"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/runtime"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/headless"
	"github.com/DonaldMurillo/gofastr/framework/ui"
	uitheme "github.com/DonaldMurillo/gofastr/framework/ui/theme"
)

// ServeControlsPage renders the editor chrome over the working theme, with the
// session's bearer token in a meta tag.
func (s *ThemeEditServer) ServeControlsPage(w http.ResponseWriter, r *http.Request) {
	controls := renderTokenControls(s.currentTokens())

	page := themeEditPageHTML(s.Token, controls, s.OutPath, s.previewThemeKey())
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "frame-ancestors 'none'; base-uri 'none'")
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write([]byte(page))
}

type tokenGroupEntry struct {
	Name   string
	Order  int
	Tokens []tokenControl
}

type tokenControl struct {
	Key, Value, Type string
}

func renderTokenControls(tokens map[string]string) string {
	groups := groupTokenControls(tokens)
	parts := make([]render.HTML, 0, len(groups))
	for _, g := range groups {
		rows := make([]render.HTML, 0, len(g.Tokens))
		for _, t := range g.Tokens {
			rows = append(rows, renderOneControl(t))
		}
		parts = append(parts, ui.Collapsible(ui.CollapsibleConfig{
			Summary: fmt.Sprintf("%s (%d)", g.Name, len(g.Tokens)),
			Open:    g.Name == "Colors" || g.Name == "Colors (dark)" || g.Name == "Component options",
		}, rows...))
	}
	return string(ui.Stack(ui.StackConfig{Gap: ui.GapSM}, parts...))
}

func groupTokenControls(tokens map[string]string) []tokenGroupEntry {
	groupMap := make(map[string]*tokenGroupEntry)
	order := map[string]int{
		"Colors":            0,
		"Component options": 1,
		"Colors (dark)":     2,
		"Spacing":           3,
		"Radii":             4,
		"Fonts":             5,
		"Typography":        6,
		"Font weights":      7,
		"Sizes":             8,
		"Shadows":           9,
		"Z-Index":           10,
		"Durations":         11,
		"Easings":           12,
		"Breakpoints":       13,
		"Code":              14,
		"Code (dark)":       15,
	}
	for k, v := range tokens {
		gn := tokenGroupName(k)
		ge, ok := groupMap[gn]
		if !ok {
			ge = &tokenGroupEntry{Name: gn, Order: 100}
			if o, ok := order[gn]; ok {
				ge.Order = o
			}
			groupMap[gn] = ge
		}
		ge.Tokens = append(ge.Tokens, tokenControl{Key: k, Value: v, Type: tokenControlType(k)})
	}
	out := make([]tokenGroupEntry, 0, len(groupMap))
	catalogue := map[string]int{}
	for i, o := range uitheme.Options() {
		catalogue["component."+o.Key] = i
	}
	for _, g := range groupMap {
		sort.Slice(g.Tokens, func(i, j int) bool {
			ci, iok := catalogue[g.Tokens[i].Key]
			cj, jok := catalogue[g.Tokens[j].Key]
			if iok && jok {
				return ci < cj
			}
			return g.Tokens[i].Key < g.Tokens[j].Key
		})
		out = append(out, *g)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Order < out[j].Order })
	return out
}

func tokenGroupName(key string) string {
	isDark := strings.HasPrefix(key, "dark.")
	base := strings.TrimPrefix(key, "dark.")
	var group string
	switch {
	case strings.HasPrefix(base, "color-"):
		group = "Colors"
	case strings.HasPrefix(base, "component."):
		group = "Component options"
	case strings.HasPrefix(base, "tk-"):
		group = "Code"
	case strings.HasPrefix(base, "spacing-"):
		group = "Spacing"
	case strings.HasPrefix(base, "radii-"):
		group = "Radii"
	case strings.HasPrefix(base, "font-weight-"):
		group = "Font weights"
	case strings.HasPrefix(base, "font-"):
		group = "Fonts"
	case strings.HasPrefix(base, "size-"):
		group = "Sizes"
	case strings.HasPrefix(base, "breakpoint-"):
		group = "Breakpoints"
	case strings.HasPrefix(base, "shadow-"):
		group = "Shadows"
	case strings.HasPrefix(base, "z-"):
		group = "Z-Index"
	case strings.HasPrefix(base, "duration-"):
		group = "Durations"
	case strings.HasPrefix(base, "easing-"):
		group = "Easings"
	case strings.HasPrefix(base, "text-"):
		group = "Typography"
	default:
		group = "Other"
	}
	if isDark {
		return group + " (dark)"
	}
	return group
}

func renderOneControl(t tokenControl) render.HTML {
	if t.Type == "select" {
		if opt, ok := componentOptionCatalogue[t.Key]; ok {
			return componentOptionSelect(t, opt)
		}
	}
	displayValue := t.Value
	if t.Type == "number-px" {
		displayValue = strings.TrimSuffix(t.Value, "px")
	}
	id := controlInputID(t.Key)

	buildInput := func(c headless.FieldControl) render.HTML {
		switch t.Type {
		case "color":
			return ui.ColorField(ui.ColorFieldConfig{
				Name:        t.Key,
				Value:       t.Value,
				Field:       c,
				SwatchLabel: t.Key + " colour swatch",
				TextLabel:   t.Key,
				SwatchAttrs: map[string]string{
					"data-token": t.Key,
					"data-type":  "color-swatch",
				},
				TextAttrs: map[string]string{
					"data-token": t.Key,
					"data-type":  t.Type,
				},
			})
		case "number", "number-px":
			return ui.Control(ui.ControlConfig{
				Field: c, Type: "number", Name: t.Key, Value: displayValue,
				ExtraAttrs: map[string]string{"data-token": t.Key, "data-type": t.Type},
			})
		default:
			return ui.Control(ui.ControlConfig{
				Field: c, Type: "text", Name: t.Key, Value: t.Value,
				ExtraAttrs: map[string]string{"data-token": t.Key, "data-type": t.Type},
			})
		}
	}

	return ui.FormField(ui.FormFieldConfig{
		Label:        t.Key,
		For:          id,
		ReserveError: true,
		Input:        buildInput,
		ExtraAttrs: html.Attrs{
			"data-field":      t.Key,
			"data-field-type": t.Type,
		},
	})
}

func controlInputID(key string) string {
	var b strings.Builder
	for _, r := range key {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	return "te-input-" + b.String()
}

var componentOptionCatalogue = func() map[string]uitheme.Option {
	m := make(map[string]uitheme.Option, len(uitheme.Options()))
	for _, o := range uitheme.Options() {
		m["component."+o.Key] = o
	}
	return m
}()

func componentOptionSelect(t tokenControl, opt uitheme.Option) render.HTML {
	options := make([]ui.SelectOption, 0, len(opt.Members))
	matched := false
	for _, m := range opt.Members {
		options = append(options, ui.SelectOption{Value: m, Text: m, Selected: m == t.Value})
		matched = matched || m == t.Value
	}
	if !matched {
		options = append([]ui.SelectOption{{Value: t.Value, Text: t.Value, Selected: true}}, options...)
	}
	return ui.Select(ui.SelectConfig{
		Name:    t.Key,
		Label:   componentOptionLabel(t.Key),
		ID:      controlInputID(t.Key),
		Options: options,
		ExtraAttrs: html.Attrs{
			"data-token": t.Key,
			"data-type":  t.Type,
		},
	})
}

func componentOptionLabel(key string) string {
	base := strings.TrimPrefix(key, "component.")
	if base == "" {
		return key
	}
	words := strings.ReplaceAll(base, ".", " ")
	return strings.ToUpper(words[:1]) + words[1:]
}

func themeEditPageHTML(token, controls, outPath, previewKey string) string {
	title := render.Tag("h1", nil, render.Text("theme edit"))
	schemeBtn := ui.Button(ui.ButtonConfig{
		Label:   "◐ Light",
		Variant: ui.ButtonSecondary,
		ID:      "te-scheme",
		Type:    "button",
	})
	writeBtn := ui.Button(ui.ButtonConfig{
		Label:   "Write",
		Variant: ui.ButtonPrimary,
		ID:      "te-write",
		Type:    "button",
	})
	headerRow := ui.Cluster(ui.ClusterConfig{
		Align:   ui.AlignCenter,
		Justify: ui.JustifyBetween,
	}, title, ui.Cluster(ui.ClusterConfig{Gap: ui.GapSM}, schemeBtn, writeBtn))

	statusBox := render.Tag("div", map[string]string{
		"id":            "te-status",
		"role":          "status",
		"aria-live":     "polite",
		"class":         "fui-callout fui-callout--neutral",
		"data-cui-comp": "ui-callout",
	}, render.Tag("div", map[string]string{"class": "fui-callout__body"}, render.Text("")))

	contrastBox := render.Tag("div", map[string]string{
		"id":            "te-contrast",
		"role":          "alert",
		"hidden":        "",
		"class":         "fui-callout fui-callout--warning",
		"data-cui-comp": "ui-callout",
	})

	controlsBox := render.Tag("div", map[string]string{
		"data-controls": "",
	}, render.HTML(controls))

	sidebarInner := ui.Stack(ui.StackConfig{Gap: ui.GapMD},
		headerRow, statusBox, contrastBox, controlsBox)

	iframe := render.Tag("iframe", map[string]string{
		"id":    "te-frame",
		"src":   "/preview",
		"title": "Theme preview",
	})

	bodyInner := ui.Workbench(ui.WorkbenchConfig{
		Rail: sidebarInner,
		Pane: iframe,
		ExtraAttrs: map[string]string{
			"aria-label": "Theme editor",
		},
	})

	used := registry.Scan(string(bodyInner))
	sort.Strings(used)
	head := []render.HTML{
		render.VoidTag("meta", map[string]string{"charset": "utf-8"}),
		render.VoidTag("meta", map[string]string{"name": "theme-edit-token", "content": token}),
		render.VoidTag("meta", map[string]string{"name": "theme-edit-out", "content": outPath}),
		render.VoidTag("meta", map[string]string{"name": "theme-edit-variant", "content": previewKey}),
		render.Tag("title", nil, render.Text("gofastr theme edit")),
		render.VoidTag("link", map[string]string{"rel": "stylesheet", "href": "/__gofastr/app.css"}),
	}
	if len(used) > 0 {
		head = append(head, render.VoidTag("link", map[string]string{
			"rel":  "stylesheet",
			"href": "/__gofastr/comp-bundle.css?names=" + url.QueryEscape(strings.Join(used, ",")),
		}))
	}

	behaviors := runtime.BehaviorsJSON()
	if len(behaviors) > 0 {
		head = append(head,
			render.Tag("script", map[string]string{
				"type": "application/json", "id": "gofastr-behaviors",
			}, render.HTML(behaviors)))
	}

	page := render.Tag("html", map[string]string{"lang": "en", "data-color-scheme": "light"},
		render.Tag("head", nil, head...),
		render.Tag("body", nil,
			bodyInner,
			render.Tag("script", map[string]string{"src": "/__gofastr/runtime.js", "defer": ""}),
			render.Tag("script", nil, render.HTML(themeEditChromeJS)),
		),
	)
	return "<!DOCTYPE html>\n" + string(page)
}

//go:embed theme_edit_page.js
var themeEditChromeJS string
