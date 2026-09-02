package ui_test

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/render"
	ui "github.com/DonaldMurillo/gofastr/framework/ui"
)

// Property: request-derived strings (record names echoed as labels,
// user-entered values round-tripped through a form POST, validation
// error messages that quote user input) must arrive HTML-escaped in
// every text slot AND every attribute slot of every widget that renders
// them. render.Tag/Attr escape both, so this pins that no widget in the
// family bypasses them with hand-rolled markup.

// labelPayload is the attr/text breakout shape: closes the current
// attribute, injects an element, and smuggles an event handler.
const labelPayload = `"><img src=x onerror=alert(1)>`

// TestWidgetLabelsEscapeMarkupPayload loops the label-bearing surfaces
// of the widget families with no prior security sibling: tab labels,
// carousel slide labels (aria-label), progress-step labels + hints,
// step-wizard headings (rendered twice: as text and interpolated into
// the aria-label of every step dot), rail item text + eyebrow chips,
// polling-indicator labels, pane-host region labels, and collapsible
// summaries.
func TestWidgetLabelsEscapeMarkupPayload(t *testing.T) {
	surfaces := []struct {
		name   string
		render func() string
	}{
		{"tabs-label", func() string {
			return string(ui.Tabs(ui.TabsConfig{SignalName: "t",
				Tabs: []ui.TabItem{{Label: labelPayload, Content: render.Text("c")}}}))
		}},
		{"carousel-slide-label", func() string {
			return string(ui.Carousel(ui.CarouselConfig{Label: "L",
				Slides: []ui.CarouselSlide{{Label: labelPayload, Content: render.Text("c")}}}))
		}},
		{"progresssteps-label", func() string {
			return string(ui.ProgressSteps(ui.ProgressStepsConfig{
				Steps: []ui.ProgressStep{{Label: labelPayload, Hint: labelPayload}}}))
		}},
		{"stepwizard-heading", func() string {
			return string(ui.StepWizard(ui.StepWizardConfig{Action: "/w",
				Steps: []ui.StepWizardStep{{Heading: labelPayload}}}))
		}},
		{"anchoredrail-text", func() string {
			return string(ui.AnchoredRail(ui.AnchoredRailConfig{Label: "L", Items: []ui.RailItem{
				{Anchor: "a", Text: labelPayload, Eyebrow: labelPayload},
			}}))
		}},
		{"pollingindicator-label", func() string {
			return string(ui.PollingIndicator(ui.PollingIndicatorConfig{Label: labelPayload}))
		}},
		{"panehost-label", func() string {
			return string(ui.PaneHost(ui.PaneHostConfig{
				Primary:        render.Text("p"),
				Secondary:      render.Text("s"),
				SecondaryOpen:  true,
				SecondaryLabel: labelPayload,
			}))
		}},
		{"collapsible-summary", func() string {
			return string(ui.Collapsible(ui.CollapsibleConfig{Summary: labelPayload}, render.Text("b")))
		}},
	}
	for _, s := range surfaces {
		t.Run(s.name, func(t *testing.T) {
			h := s.render()
			if strings.Contains(h, labelPayload) {
				t.Errorf("SECURITY: %s rendered label payload verbatim:\n%s", s.name, h)
			}
			if strings.Contains(h, `<img src=x`) {
				t.Errorf("SECURITY: %s let the injected element parse:\n%s", s.name, h)
			}
			// An event handler is only live attached to a parsed element,
			// which requires a raw "<" — excluded above; the escaped-form
			// check below completes the pin.
			if !strings.Contains(h, "&lt;img") {
				t.Errorf("%s: expected escaped label to be present, got:\n%s", s.name, h)
			}
		})
	}
}

// valuePayload targets attribute context specifically: hidden input
// values, field values, and select option values are the slots that
// round-trip a previous POST (attacker-chosen bytes re-rendered by the
// server).
const valuePayload = `"><script>alert(1)</script>`

// TestRoundTripValuesEscapeAttrPayload pins that round-tripped values
// cannot break out of their value="…" attributes: TagInput initial
// tags (hidden inputs), TextField values, and Select option
// values/text (the FilterToolbar filter engine).
func TestRoundTripValuesEscapeAttrPayload(t *testing.T) {
	surfaces := []struct {
		name   string
		render func() string
	}{
		{"taginput-hidden-values", func() string {
			return string(ui.TagInput(ui.TagInputConfig{
				Name: "tags", Label: "Tags", Values: []string{valuePayload, "plain"},
			}))
		}},
		{"textfield-value", func() string {
			return string(ui.TextField(ui.TextFieldConfig{
				Name: "q", Label: "Q", Value: valuePayload,
			}))
		}},
		{"select-option-value", func() string {
			return string(ui.Select(ui.SelectConfig{
				Name: "status", Label: "Status",
				Options: []ui.SelectOption{{Value: valuePayload, Text: valuePayload, Selected: true}},
			}))
		}},
	}
	for _, s := range surfaces {
		t.Run(s.name, func(t *testing.T) {
			h := s.render()
			if strings.Contains(h, valuePayload) {
				t.Errorf("SECURITY: %s rendered round-tripped value verbatim:\n%s", s.name, h)
			}
			if strings.Contains(h, `value=""><script`) || strings.Contains(h, `"><script>alert`) {
				t.Errorf("SECURITY: %s value broke out of its attribute:\n%s", s.name, h)
			}
			if strings.Contains(h, "<script>") {
				t.Errorf("SECURITY: %s let an injected script element parse:\n%s", s.name, h)
			}
			if !strings.Contains(h, "&lt;script&gt;") {
				t.Errorf("%s: expected escaped value present, got:\n%s", s.name, h)
			}
		})
	}
}

// TestCounterSignalAttrsEscapePayload pins the signal-wiring surfaces of
// Counter: the SignalName is interpolated into data-fui-signal and
// data-fui-signal-inc attribute values (name and name:delta), which the
// runtime later parses; the payload must stay attribute-escaped so it
// cannot inject a second attribute onto the +/- buttons.
func TestCounterSignalAttrsEscapePayload(t *testing.T) {
	payload := `c" onclick="alert(1)`
	h := string(ui.Counter(ui.CounterConfig{SignalName: payload, Step: 3}))
	// A live handler needs a raw `onclick="…"` attribute; the escaped
	// name renders the quotes as &quot; so no second attribute exists.
	if strings.Contains(h, ` onclick="alert`) {
		t.Errorf("SECURITY: Counter signal name smuggled an event handler onto a button:\n%s", h)
	}
	// The escaped name must survive in all three wiring slots (dec
	// name:-step, display name, inc name:step).
	if n := strings.Count(h, `c&quot; onclick=&quot;alert(1)`); n != 3 {
		t.Errorf("Counter: escaped signal name missing from signal wiring (%d/3 slots):\n%s", n, h)
	}
}
