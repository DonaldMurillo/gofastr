package ui_test

import (
	"context"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/chromedp/chromedp"

	"github.com/DonaldMurillo/gofastr/framework/testkit/axetest"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

func TestChoiceIDsSurviveSlugClashes(t *testing.T) {
	ctx := render.WithIDScope(context.Background())
	markup := ui.RadioGroup(ui.RadioGroupConfig{Name: "language", Legend: "Radio language", Help: "Choose one", Options: []ui.RadioGroupOption{
		{Value: "C", Label: "C"}, {Value: "C++", Label: "C++"}, {Value: "C#", Label: "C#"},
	}}) + ui.CheckboxGroup(ui.CheckboxGroupConfig{Name: "languages", Legend: "Checkbox languages", Help: "Choose some", Options: []ui.CheckboxGroupOption{
		{Value: "C", Label: "C"}, {Value: "C++", Label: "C++"}, {Value: "C#", Label: "C#"},
	}})
	for _, value := range []string{"C", "C++", "C#"} {
		markup += ui.Radio(ui.ToggleConfig{Ctx: ctx, Name: "standalone", Value: value, Label: value, Help: "Own hint"})
	}
	server := menuTriggerAxeServer(t, string(markup))
	browser := axetest.NewBrowser(t)
	page, cancel := axetest.NewTab(t, browser)
	defer cancel()
	var valid bool
	if err := chromedp.Run(page, chromedp.Navigate(server.URL+"/"), chromedp.Evaluate(`(() => {
		const controls = [...document.querySelectorAll('input')];
		const ids = [...document.querySelectorAll('[id]')].map(node => node.id);
		return controls.length === 9 && new Set(ids).size === ids.length && controls.every(control => {
			const label = control.closest('label'); label.click();
			const descriptions = (control.getAttribute('aria-describedby') || '').split(' ').filter(Boolean);
			return document.getElementById(control.id) === control && document.activeElement === control &&
				descriptions.every(id => label.contains(document.getElementById(id)));
		});
	})()`, &valid)); err != nil {
		t.Fatal(err)
	}
	if !valid {
		t.Fatal("punctuation-equivalent choices share IDs or description targets")
	}
}

func TestChoiceFamiliesStayScoped(t *testing.T) {
	ctx := render.WithIDScope(context.Background())
	group := ui.RadioGroupConfig{Ctx: ctx, ID: "language-group", Name: "language", Legend: "Languages", Help: "Choose one", Options: []ui.RadioGroupOption{{Value: "C", Label: "C"}, {Value: "C++", Label: "C++"}}}
	markup := ui.RadioGroup(group)
	group.ID = ""
	markup += ui.RadioGroup(group)
	markup += ui.Checkbox(ui.ToggleConfig{Ctx: ctx, Name: "language-group-0", Label: "Other"})
	markup += ui.Switch(ui.ToggleConfig{Ctx: ctx, Name: "toggle", Label: "First switch", Help: "First hint"})
	markup += ui.Switch(ui.ToggleConfig{Ctx: ctx, Name: "toggle", Label: "Second switch", Help: "Second hint"})
	server := menuTriggerAxeServer(t, string(markup))
	browser := axetest.NewBrowser(t)
	page, cancel := axetest.NewTab(t, browser)
	defer cancel()
	var valid bool
	if err := chromedp.Run(page, chromedp.Navigate(server.URL+"/"), chromedp.Evaluate(`(() => {
		const ids = [...document.querySelectorAll('[id]')].map(node => node.id);
		const switches = [...document.querySelectorAll('input[role="switch"]')];
		return new Set(ids).size === ids.length && document.querySelector('fieldset').id === 'language-group' &&
			switches.length === 2 && switches.every(control =>
				control.getAttribute('aria-describedby') === control.id + '-hint' && !!document.getElementById(control.id + '-hint'));
	})()`, &valid)); err != nil {
		t.Fatal(err)
	}
	if !valid {
		t.Fatal("group, leaf or switch descriptions escaped their scoped ID family")
	}
}
