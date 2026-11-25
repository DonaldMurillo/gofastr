package ui

import (
	"regexp"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/style"
)

// cssRule splits a sheet into selector → body pairs, one per rule.
var cssRule = regexp.MustCompile(`(?s)([^{}]+)\{([^{}]*)\}`)

// The grip is the only part of a row that takes touch-action none, so
// a touch on the rest of the row scrolls the page, and each landing
// mark the drag module sets has a rule drawn with a border or outline.
func TestSortableListGripAloneTakesTheTouch(t *testing.T) {
	css := sortablelistCSS(style.Theme{})
	var touch []string
	for _, m := range cssRule.FindAllStringSubmatch(css, -1) {
		if strings.Contains(m[2], "touch-action") {
			touch = append(touch, strings.TrimSpace(m[1]))
		}
	}
	if len(touch) != 1 || !strings.HasSuffix(touch[0], ".fui-sortablelist__grip") || !strings.Contains(css, "touch-action: none") {
		t.Fatalf("touch-action must sit on the grip rule alone, found it on %q", touch)
	}
	for _, mark := range []string{".is-drop-before", ".is-drop-after", "].is-drop-target"} {
		if !strings.Contains(css, mark) {
			t.Errorf("the sheet draws no %s mark", mark)
		}
	}
	for _, m := range cssRule.FindAllStringSubmatch(css, -1) {
		if strings.Contains(m[1], "is-drop") && strings.Contains(m[2], "background") {
			t.Errorf("a landing mark is drawn with a background, which forced colours drop: %s", strings.TrimSpace(m[1]))
		}
	}
}
