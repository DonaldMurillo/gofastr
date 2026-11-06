package ui_test

import (
	"context"
	"testing"

	"github.com/chromedp/chromedp"

	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/testkit/axetest"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

func TestFieldInstancesKeepOwnLabels(t *testing.T) {
	fields := map[string]func(context.Context) render.HTML{
		"text": func(ctx context.Context) render.HTML {
			return ui.TextField(ui.TextFieldConfig{Ctx: ctx, Name: "shared", Label: "Text", Help: "Hint", Error: "Error"})
		},
		"number": func(ctx context.Context) render.HTML {
			return ui.NumberField(ui.NumberFieldConfig{Ctx: ctx, Name: "shared", Label: "Number", Help: "Hint", Error: "Error"})
		},
		"date": func(ctx context.Context) render.HTML {
			return ui.DateField(ui.DateFieldConfig{Ctx: ctx, Name: "shared", Label: "Date", Help: "Hint", Error: "Error"})
		},
		"datetime": func(ctx context.Context) render.HTML {
			return ui.DateTimeField(ui.DateTimeFieldConfig{Ctx: ctx, Name: "shared", Label: "Time", Help: "Hint", Error: "Error"})
		},
		"select": func(ctx context.Context) render.HTML {
			return ui.Select(ui.SelectConfig{Ctx: ctx, Name: "shared", Label: "Select", Help: "Hint", Error: "Error", Options: []ui.SelectOption{{Value: "one", Text: "One"}}})
		},
	}
	for name, field := range fields {
		t.Run(name, func(t *testing.T) {
			ctx := render.WithIDScope(context.Background())
			body := `<section id="first">` + string(field(ctx)) + `</section><section id="second">` + string(field(ctx)) + `</section>`
			server := menuTriggerAxeServer(t, body)
			browser := axetest.NewBrowser(t)
			page, cancel := axetest.NewTab(t, browser)
			defer cancel()
			var valid bool
			if err := chromedp.Run(page, chromedp.Navigate(server.URL+"/"), chromedp.Evaluate(`(() => {
				const section = document.getElementById('second');
				const control = section.querySelector('input,select');
				section.querySelector('label').click();
				const ids = [...document.querySelectorAll('[id]')].map(node => node.id);
				return document.activeElement === control && new Set(ids).size === ids.length &&
					control.getAttribute('aria-describedby').split(' ').every(id => section.contains(document.getElementById(id)));
			})()`, &valid)); err != nil {
				t.Fatal(err)
			}
			if !valid {
				t.Fatal("second field shares a label or description target with the first")
			}
		})
	}
}

func TestFormRepeatersHaveScopedIDs(t *testing.T) {
	ctx := render.WithIDScope(context.Background())
	config := ui.FormRepeaterConfig{Name: "rows", Ctx: ctx}
	server := menuTriggerAxeServer(t, string(ui.FormRepeater(config))+string(ui.FormRepeater(config)))
	browser := axetest.NewBrowser(t)
	page, cancel := axetest.NewTab(t, browser)
	defer cancel()
	var unique bool
	if err := chromedp.Run(page, chromedp.Navigate(server.URL+"/"), chromedp.Evaluate(`(() => {
		const ids = [...document.querySelectorAll('[id]')].map(node => node.id);
		return new Set(ids).size === ids.length;
	})()`, &unique)); err != nil {
		t.Fatal(err)
	}
	if !unique {
		t.Fatal("same-named repeaters share DOM ids")
	}
}

func TestFieldIDFamiliesStayReserved(t *testing.T) {
	ctx := render.WithIDScope(context.Background())
	body := ui.TextField(ui.TextFieldConfig{Ctx: ctx, ID: "shared", Name: "first", Label: "First", Help: "Hint", Error: "Error"}) +
		ui.NumberField(ui.NumberFieldConfig{Ctx: ctx, Name: "shared", Label: "Second", Help: "Hint", Error: "Error"}) +
		ui.Select(ui.SelectConfig{Ctx: ctx, Name: "shared-hint", Label: "Select", Options: []ui.SelectOption{{Value: "one", Text: "One"}}}) +
		ui.FormRepeater(ui.FormRepeaterConfig{Ctx: ctx, ID: "rows", Name: "first_rows"}) +
		ui.FormRepeater(ui.FormRepeaterConfig{Ctx: ctx, Name: "rows"})
	server := menuTriggerAxeServer(t, string(body))
	browser := axetest.NewBrowser(t)
	page, cancel := axetest.NewTab(t, browser)
	defer cancel()
	var valid bool
	if err := chromedp.Run(page, chromedp.Navigate(server.URL+"/"), chromedp.Evaluate(`(() => {
		const ids = [...document.querySelectorAll('[id]')].map(node => node.id);
		return new Set(ids).size === ids.length && document.querySelector('input[name="first"]').id === 'shared' &&
			document.querySelector('[data-hui-repeater]').id === 'rows';
	})()`, &valid)); err != nil {
		t.Fatal(err)
	}
	if !valid {
		t.Fatal("explicit control IDs or their description families were reused")
	}
}
