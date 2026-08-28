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

// ExtraAttrs land on the <ol> root but never override what the
// component owns (#262): class and data-fui-* variants are dropped
// (there are no other owned attributes on the root).
func TestTimelineExtraAttrsCannotOverrideOwned(t *testing.T) {
	h := string(Timeline(TimelineConfig{
		Class:  "mine",
		Events: []TimelineEvent{{Title: "First"}},
		ExtraAttrs: map[string]string{
			"data-test": "hook", "Class": "evil", "data-fui-comp": "spoof",
		},
	}))
	root := h[:strings.Index(h, ">")+1]
	for _, banned := range []string{"evil", "spoof"} {
		if strings.Contains(root, banned) {
			t.Errorf("owned attr overridden by ExtraAttrs (%q):\n%s", banned, root)
		}
	}
	for _, want := range []string{
		`data-test="hook"`, `class="ui-timeline mine"`,
	} {
		if !strings.Contains(root, want) {
			t.Errorf("root missing %q:\n%s", want, root)
		}
	}
}
