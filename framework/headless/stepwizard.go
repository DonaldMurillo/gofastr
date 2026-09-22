package headless

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/urlsafe"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// The step wizard: a multi-step form whose every transition is a
// plain form POST the server answers with the next step. The rail is
// a list of states (not controls), the current step is named, and the
// validation summary of a failed submit moves focus on arrival
// through the existing headless module's data-hui-form-errors hook.
// The module that binds this package's wizard hooks (headless-wizard)
// owns only the island path: it records which control submitted,
// focuses the summary or the new heading after the swap, and says the
// step-of sentence the server rendered.

// StepWizard parts.
const (
	PartSteps Part = "steps"
)

// WizardStep is one step of the flow.
type WizardStep struct {
	// Heading is the step's name. Required: the rail's dots and the
	// step's own heading say it, and a step with no name is a
	// nameless place in a flow.
	Heading string
	// Description is the supporting line under the heading.
	Description string
	// Fields are the step's form fields.
	Fields []render.HTML
}

// StepWizardProps configures the wizard.
type StepWizardProps struct {
	// Steps is the flow. At least one.
	Steps []WizardStep
	// Current is the 0-based step in progress. Outside the list is
	// refused.
	Current int
	// Action and Method are the form's own. Method defaults to POST;
	// anything but GET or POST is refused.
	Action string
	Method string
	// HiddenFields carry state between steps.
	HiddenFields []render.HTML
	// Errors is the rendered validation summary for a failed submit.
	// When set, ID is required and the form asks the runtime to move
	// focus to the summary on arrival.
	Errors render.HTML

	// Island, when set, makes the submit a region update: the same
	// form, the same controls, the answer swapped into the signal.
	// Partial is refused like every Island.
	Island Island

	ID         string
	ExtraAttrs html.Attrs

	// Parts: attrs and binds on every part. Strings carry the three
	// controls, the rail's name and each dot's.
	Parts Parts
	// Strings are the strings this component says. Nil means the English
	// defaults; a layer above sets them from the request's language.
	Strings *Strings
}

// StepWizard renders the form, its rail and its controls.
func StepWizard(p StepWizardProps, s Classes) render.HTML {
	if len(p.Steps) == 0 {
		panic("headless: StepWizard requires Steps — a flow with no steps is not a flow")
	}
	for i, st := range p.Steps {
		// Trimmed: a heading of spaces names nothing, exactly like no
		// heading.
		if strings.TrimSpace(st.Heading) == "" {
			panic("headless: StepWizard step " + strconv.Itoa(i+1) + " requires Heading — a nameless step is a nameless place in the flow")
		}
	}
	if p.Current < 0 || p.Current >= len(p.Steps) {
		panic("headless: StepWizard Current " + strconv.Itoa(p.Current) + " is outside 0.." + strconv.Itoa(len(p.Steps)-1))
	}
	if p.Action == "" {
		panic("headless: StepWizard requires Action — the transition is a form POST, and a form with no action posts to a page that cannot answer with the next step")
	}
	if urlsafe.CleanAnchor(p.Action) == "" {
		panic("headless: StepWizard Action " + strconv.Quote(p.Action) + " is not a URL the anchor policy allows")
	}
	method := orDefault(p.Method, "POST")
	if method != "GET" && method != "POST" {
		panic("headless: StepWizard Method " + strconv.Quote(method) + " is not GET or POST — the form's own method is what the no-script page submits")
	}
	if p.Errors != "" && p.ID == "" {
		panic("headless: StepWizard rendering Errors requires ID — the summary's id is derived from it, and two summaries on one page would share one title id")
	}
	if !p.Island.zero() {
		p.Island.check()
	}

	w := p.Strings.Resolve()
	b := p.Parts.Box(s)
	total := len(p.Steps)

	// The rail: a list of states. Each dot's row is named ("Step 2:
	// Billing") and the list carries the step-of sentence the status
	// repeats, so the rail can be re-announced after a swap from the
	// server's own words.
	dots := make([]render.HTML, 0, total)
	for i, st := range p.Steps {
		state, current := "todo", false
		switch {
		case i < p.Current:
			state = "done"
		case i == p.Current:
			state, current = "current", true
		}
		attrs := html.Attrs{"data-state": state}
		if current {
			attrs["aria-current"] = "step"
		}
		marker := "✓"
		if state != "done" {
			marker = strconv.Itoa(i + 1)
		}
		dots = append(dots, b.El("li", PartStep, attrs,
			b.El("span", PartStepRow,
				Attrs(map[string]string{"aria-label": fmt.Sprintf(w.StepName, i+1, st.Heading)}),
				b.El("span", PartMarker, Attrs(map[string]string{"aria-hidden": "true"}),
					render.Text(marker)),
				b.El("span", PartStepText, nil, render.Text(st.Heading)),
			),
		))
	}

	step := p.Steps[p.Current]
	kids := []render.HTML{
		b.El("ol", PartSteps, html.Attrs{
			"aria-label": fmt.Sprintf(w.StepOf, p.Current+1, total),
		}, dots...),
	}
	if p.Errors != "" {
		kids = append(kids, p.Errors)
	}
	if step.Heading != "" {
		// Focusable by script only: after an island swap the module
		// lands focus here, the same posture a summary's title keeps.
		kids = append(kids, b.El("h2", PartTitle,
			html.Attrs{"tabindex": "-1"}, render.Text(step.Heading)))
	}
	if step.Description != "" {
		kids = append(kids, b.El("p", PartDesc, nil, render.Text(step.Description)))
	}
	if len(step.Fields) > 0 {
		kids = append(kids, b.El("div", PartBody, nil, step.Fields...))
	}
	kids = append(kids, p.HiddenFields...)

	back := render.HTML("")
	if p.Current > 0 {
		back = b.El("button", PartPrev, html.Attrs{
			"type":                        "submit",
			"name":                        "wizard_action",
			"value":                       "back",
			"data-hui-step-wizard-action": "back",
		}, render.Text(w.StepBack))
	}
	nextWord := w.StepNext
	if p.Current == total-1 {
		nextWord = w.StepSubmit
	}
	next := b.El("button", PartNext, html.Attrs{
		"type":                        "submit",
		"name":                        "wizard_action",
		"value":                       "next",
		"data-hui-step-wizard-action": "next",
	}, render.Text(nextWord))
	kids = append(kids,
		b.El("div", PartActions, nil, back, next),
		// The step-of sentence: the server's words, repeated into the
		// live region after a swap by the module, clear then frame,
		// the way a table's announcement is.
		b.El("span", PartStatus, html.Attrs{
			"role":                        "status",
			"data-hui-step-wizard-status": "",
		}, render.Text(fmt.Sprintf(w.StepOf, p.Current+1, total))),
	)

	own := Merge(Safe(p.ExtraAttrs, "method", "action"), Attrs(map[string]string{
		"id":     p.ID,
		"action": p.Action,
		"method": method,
	}))
	Mark(own, "data-hui-step-wizard")
	own["data-hui-step-wizard-current"] = strconv.Itoa(p.Current)
	if p.Errors != "" {
		Mark(own, "data-hui-form-errors")
	}
	if !p.Island.zero() {
		// The method and action stay for no script; the contract
		// beside them makes the submit a region update.
		own = Merge(own, p.Island.attrs("", method))
	}
	return b.El("form", PartRoot, own, kids...)
}

func init() {
	Register(Spec{
		Name: "StepWizard",
		Anatomy: []Part{PartRoot, PartSteps, PartStep, PartStepRow, PartMarker,
			PartStepText, PartTitle, PartDesc, PartBody, PartActions, PartPrev,
			PartNext, PartStatus},
		Hooks: []string{"data-hui-step-wizard", "data-hui-step-wizard-current",
			"data-hui-step-wizard-action", "data-hui-step-wizard-status"},
		WithParts: func(s Classes, parts Parts) render.HTML {
			return StepWizard(StepWizardProps{
				Action: "/setup", Steps: []WizardStep{{Heading: "Source"}}, Parts: parts}, s)
		},
		Cases: func(k Kit) []Case {
			s := k.Classes
			steps := []WizardStep{
				{Heading: "Source", Fields: []render.HTML{
					Input(InputProps{Name: "repo", AriaLabel: "Repository"}, s)}},
				{Heading: "Build", Description: "About a minute"},
				{Heading: "Deploy"},
			}
			field := Input(InputProps{Name: "repo", AriaLabel: "Repository"}, s)
			return []Case{{
				Name: "the first step",
				HTML: StepWizard(StepWizardProps{Action: "/setup",
					Steps: []WizardStep{{Heading: "Source", Fields: []render.HTML{field}},
						{Heading: "Build", Description: "About a minute"}, {Heading: "Deploy"}},
					Current: 0}, s),
				Why: "the transition is a plain form POST with wizard_action=next — the no-script page walks the whole flow — and the first step renders no Back because there is nothing to go back to",
			}, {
				Name: "mid-flow, with Back",
				Why:  "the rail names every dot (the step and its heading) and says where the reader is (step of total), the current dot carries aria-current, and Back submits the same form with the other value",
				HTML: StepWizard(StepWizardProps{Action: "/setup", Steps: steps, Current: 1}, s),
			}, {
				Name: "the last step, an island, a failed submit",
				Why:  "the last step's weighted action says Submit, the failed submit's summary rides the form the existing headless module already focuses, and the Island makes the answer a region update the wizard module announces through the server's step-of sentence",
				HTML: StepWizard(StepWizardProps{Action: "/setup", Steps: steps, Current: 2, ID: "setup",
					Errors: ValidationSummary(ValidationSummaryProps{ID: "setup-errors", Errors: []FieldError{{Message: "Pick a region"}}}, k.For("ValidationSummary")),
					Island: Island{Endpoint: "/island/setup", Signal: "setup"}}, s),
			}}
		},
	})
}
