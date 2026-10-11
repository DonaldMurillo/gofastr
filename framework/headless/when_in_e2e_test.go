package headless

// Browser coverage for the in half of the when behaviour: a region
// shown while the watched field holds ANY of the values its
// data-hui-when-in list carries. The same harness as the behaviour
// file above: the real runtime, the real module, one page per test.
// The member values deliberately hold a comma, a quote and a bracket:
// the list is JSON in one attribute, so those bytes must survive the
// browser's attribute parser and JSON.parse as one member.

import (
	"context"
	"testing"

	"github.com/chromedp/chromedp"
)

// setSelectValue picks an option by value and fires a change, the way
// a real select interaction does. A JSON string literal is a valid JS
// string literal, so a value holding a quote survives the trip.
func setSelectValue(t *testing.T, ctx context.Context, id, val string) {
	t.Helper()
	if err := chromedp.Run(ctx, chromedp.Evaluate(`(() => {
		const sel = document.getElementById('`+id+`');
		sel.value = `+string(mustJSON(val))+`;
		sel.dispatchEvent(new Event('change', {bubbles: true}));
	})()`, nil)); err != nil {
		t.Fatalf("changing %s: %v", id, err)
	}
}

// A bool field renders a hidden "false" before its "true" checkbox (the
// pair the form submits as one value). The box decides the condition,
// and unchecked the hidden "false" is the value, as the form submits it.
func TestE2E_WhenFollowsBoolPair(t *testing.T) {
	page := string(Form(FormProps{Action: "/x"}, nil,
		Input(InputProps{Type: "hidden", Name: "rush", Value: "false"}, nil),
		Choice(ChoiceProps{Type: "checkbox", Name: "rush", Value: "true", Label: "Rush", ID: "rush"}, nil),
		ConditionalField(ConditionalFieldProps{When: "rush", Values: []string{"true"}}, nil,
			Input(InputProps{Name: "by", ID: "rush-by"}, nil)),
		ConditionalField(ConditionalFieldProps{When: "rush", Values: []string{"false"}}, nil,
			Input(InputProps{Name: "slot", ID: "slow-slot"}, nil)),
	))
	b := startBehaviorServer(t, page)
	ctx := behaviorPage(t, b)
	const regions = `document.querySelectorAll('[data-hui-when]')`
	if !pollTrue(ctx, regions+`.length === 2 && `+regions+`[0].hidden && !`+regions+`[1].hidden`) {
		t.Fatal(`unchecked, the field's value is "false": the true region must hide and the false region show`)
	}
	if err := chromedp.Run(ctx, chromedp.Click("#rush", chromedp.ByID)); err != nil {
		t.Fatalf("click: %v", err)
	}
	if !pollTrue(ctx, `!`+regions+`[0].hidden && `+regions+`[1].hidden`) {
		t.Fatal(`checked, the field's value is "true": the true region must show and the false region hide`)
	}
}

// A select controller: the region shows for every listed value — the
// hostile member included — hides for an unlisted one, and the hidden
// region's control is disabled so it never submits.
func TestE2E_WhenInShowsEachListedValue(t *testing.T) {
	hostile := `webhook, "both"]`
	sel := Select(SelectProps{Name: "notify", ID: "notify-sel", Options: []Option{
		{Value: "email", Label: "Email"},
		{Value: hostile, Label: "Both"},
		{Value: "none", Label: "None"},
	}, Selected: "none"}, nil)
	region := ConditionalField(ConditionalFieldProps{When: "notify",
		Values: []string{"email", hostile}}, nil,
		Input(InputProps{Name: "addr", ID: "in-addr"}, nil))
	b := startBehaviorServer(t, string(Form(FormProps{Action: "/x"}, nil, sel, region)))
	ctx := behaviorPage(t, b)

	// Boot: the rendered value ("none") is not in the list, so the
	// module hides the region and disables its control.
	if !pollTrue(ctx, `document.querySelector('[data-hui-when]').hidden`) {
		t.Fatal("the region was never hidden for the unlisted value")
	}
	var disabled bool
	if err := chromedp.Run(ctx, chromedp.Evaluate(`document.getElementById('in-addr').disabled`, &disabled)); err != nil {
		t.Fatal(err)
	}
	if !disabled {
		t.Fatal("the hidden region left its control enabled, so it would submit")
	}

	for _, v := range []string{"email", hostile} {
		setSelectValue(t, ctx, "notify-sel", v)
		if !pollTrue(ctx, `!document.querySelector('[data-hui-when]').hidden`) {
			t.Fatalf("the region never showed for the listed value %q", v)
		}
		if err := chromedp.Run(ctx, chromedp.Evaluate(`document.getElementById('in-addr').disabled`, &disabled)); err != nil {
			t.Fatal(err)
		}
		if disabled {
			t.Fatalf("the shown region kept its control disabled for the listed value %q", v)
		}
	}

	setSelectValue(t, ctx, "notify-sel", "none")
	if !pollTrue(ctx, `document.querySelector('[data-hui-when]').hidden`) {
		t.Fatal("the region never hid again for the unlisted value")
	}
}

// Radio and checkbox controllers drive the list the way they drive the
// single value: the checked radio's value, the checkbox's value when
// checked and the empty string when not — which the list form refuses
// as a member, so an unchecked checkbox leaves the region hidden.
func TestE2E_WhenInFollowsRadioAndCheckbox(t *testing.T) {
	page := string(Form(FormProps{Action: "/x"}, nil,
		Choice(ChoiceProps{Type: "radio", Name: "freq", Value: "daily", Label: "Daily", ID: "freq-daily"}, nil),
		Choice(ChoiceProps{Type: "radio", Name: "freq", Value: "weekly", Label: "Weekly", ID: "freq-weekly"}, nil),
		ConditionalField(ConditionalFieldProps{When: "freq", Values: []string{"daily", "weekly"}}, nil,
			Input(InputProps{Name: "at", ID: "radio-at"}, nil)),
		Choice(ChoiceProps{Type: "checkbox", Name: "digest", Value: "on", Label: "Digest", ID: "digest"}, nil),
		ConditionalField(ConditionalFieldProps{When: "digest", Values: []string{"on"}}, nil,
			Input(InputProps{Name: "when", ID: "digest-when"}, nil)),
	))
	b := startBehaviorServer(t, page)
	ctx := behaviorPage(t, b)

	// Neither controller starts on a listed value: both regions hide.
	if !pollTrue(ctx, `document.querySelectorAll('[data-hui-when]').length === 2 && document.querySelectorAll('[data-hui-when]')[0].hidden && document.querySelectorAll('[data-hui-when]')[1].hidden`) {
		t.Fatal("the regions never hid for the unlisted starting values")
	}

	click := func(id string) {
		t.Helper()
		if err := chromedp.Run(ctx, chromedp.Click("#"+id, chromedp.ByID)); err != nil {
			t.Fatalf("clicking %s: %v", id, err)
		}
	}
	click("freq-weekly")
	if !pollTrue(ctx, `!document.querySelectorAll('[data-hui-when]')[0].hidden`) {
		t.Fatal("the region never showed for the listed radio value")
	}
	click("freq-daily")
	if !pollTrue(ctx, `!document.querySelectorAll('[data-hui-when]')[0].hidden`) {
		t.Fatal("the region hid when the selection moved between two listed values")
	}

	click("digest")
	if !pollTrue(ctx, `!document.querySelectorAll('[data-hui-when]')[1].hidden`) {
		t.Fatal("the region never showed for the listed checkbox value")
	}
	click("digest")
	if !pollTrue(ctx, `document.querySelectorAll('[data-hui-when]')[1].hidden`) {
		t.Fatal("the unchecked checkbox's empty value showed the region — it is not a listed member")
	}
}

// A list the module cannot parse matches nothing: the region hides,
// the same posture as a value that never arrives. It never shows on a
// guess, and nothing throws.
func TestE2E_WhenInHidesOnAnUnparsableList(t *testing.T) {
	b := startBehaviorServer(t, `<select name="plan" id="plan"><option value="pro">Pro</option></select>`+
		`<div data-hui-when="plan" data-hui-when-in="not json"><input name="seats" id="seats"></div>`)
	ctx := behaviorPage(t, b)
	if !pollTrue(ctx, `document.querySelector('[data-hui-when]').hidden`) {
		t.Fatal("a region whose list cannot be parsed must hide, not show on a guess")
	}
	if err := chromedp.Run(ctx, chromedp.Evaluate(`(() => {
		const sel = document.getElementById('plan');
		sel.value = 'pro';
		sel.dispatchEvent(new Event('change', {bubbles: true}));
	})()`, nil)); err != nil {
		t.Fatal(err)
	}
	if !pollTrue(ctx, `document.querySelector('[data-hui-when]').hidden && document.getElementById('seats').disabled`) {
		t.Fatal("the unparsable list matched a value it was never told to match")
	}
}
