package ui

import (
	"strings"
	"testing"
)

func TestTimelineRequiresAtLeastOneEvent(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("Timeline without Events should panic")
		}
	}()
	Timeline(TimelineConfig{})
}

func TestTimelineEventRequiresTitle(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("Timeline event without Title should panic")
		}
	}()
	Timeline(TimelineConfig{Events: []TimelineEvent{{Title: ""}}})
}

func TestTimelineRendersAsOrderedList(t *testing.T) {
	h := string(Timeline(TimelineConfig{
		Events: []TimelineEvent{
			{Title: "First"}, {Title: "Second"},
		},
	}))
	if !strings.Contains(h, "<ol") {
		t.Errorf("Timeline should render as <ol>:\n%s", h)
	}
	if strings.Count(h, "ui-timeline__item") != 2 {
		t.Errorf("expected 2 items in DOM:\n%s", h)
	}
}

func TestTimelineVariantsEmitClass(t *testing.T) {
	h := string(Timeline(TimelineConfig{
		Events: []TimelineEvent{
			{Title: "ok", Variant: TimelineSuccess},
			{Title: "broken", Variant: TimelineDanger},
		},
	}))
	if !strings.Contains(h, "ui-timeline__item--success") {
		t.Errorf("success variant should add modifier class:\n%s", h)
	}
	if !strings.Contains(h, "ui-timeline__item--danger") {
		t.Errorf("danger variant should add modifier class:\n%s", h)
	}
}

func TestTimelineRejectsUnknownVariant(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("Timeline event with unknown Variant should panic")
		}
	}()
	Timeline(TimelineConfig{
		Events: []TimelineEvent{{Title: "x", Variant: TimelineEventVariant("bogus")}},
	})
}
