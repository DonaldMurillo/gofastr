package ui

import (
	"strings"
	"testing"
)

func TestProgressStepsRequiresAtLeastOneStep(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("ProgressSteps without Steps should panic")
		}
	}()
	ProgressSteps(ProgressStepsConfig{})
}

func TestProgressStepsRendersOLWithNav(t *testing.T) {
	h := string(ProgressSteps(ProgressStepsConfig{
		Steps: []ProgressStep{{Label: "A"}, {Label: "B"}},
	}))
	if !strings.Contains(h, "<nav") {
		t.Errorf("ProgressSteps should wrap in <nav>:\n%s", h)
	}
	if !strings.Contains(h, "<ol") {
		t.Errorf("ProgressSteps inner list should be <ol>:\n%s", h)
	}
	if !strings.Contains(h, `aria-label="Progress"`) {
		t.Errorf("nav should default aria-label=Progress:\n%s", h)
	}
}

func TestProgressStepsCurrentEmitsAriaCurrent(t *testing.T) {
	h := string(ProgressSteps(ProgressStepsConfig{
		Steps: []ProgressStep{
			{Label: "Done", Status: ProgressStepComplete},
			{Label: "Now", Status: ProgressStepCurrent},
			{Label: "Later"},
		},
	}))
	if !strings.Contains(h, `aria-current="step"`) {
		t.Errorf("current step should have aria-current=step:\n%s", h)
	}
	if !strings.Contains(h, "ui-progress-steps__item--current") {
		t.Errorf("current step should have modifier class:\n%s", h)
	}
	if !strings.Contains(h, "ui-progress-steps__item--complete") {
		t.Errorf("complete step should have modifier class:\n%s", h)
	}
}

func TestProgressStepsCompleteWithHrefIsLink(t *testing.T) {
	h := string(ProgressSteps(ProgressStepsConfig{
		Steps: []ProgressStep{
			{Label: "Done", Status: ProgressStepComplete, Href: "/back"},
			{Label: "Now", Status: ProgressStepCurrent},
		},
	}))
	if !strings.Contains(h, `href="/back"`) {
		t.Errorf("complete + Href should render an <a>:\n%s", h)
	}
}

func TestProgressStepsVerticalOrientation(t *testing.T) {
	h := string(ProgressSteps(ProgressStepsConfig{
		Orientation: ProgressStepsVertical,
		Steps:       []ProgressStep{{Label: "A"}},
	}))
	if !strings.Contains(h, "ui-progress-steps--vertical") {
		t.Errorf("Vertical orientation should add modifier class:\n%s", h)
	}
}

func TestProgressStepsRejectsUnknownStatus(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("ProgressSteps with unknown Status should panic")
		}
	}()
	ProgressSteps(ProgressStepsConfig{
		Steps: []ProgressStep{{Label: "x", Status: ProgressStepStatus("bogus")}},
	})
}
