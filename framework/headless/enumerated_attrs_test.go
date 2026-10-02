package headless

import (
	"regexp"
	"testing"
)

// An enumerated attribute is not a boolean: its empty value is a state
// of its own, never "on". draggable="" is "auto", and an <li> under auto
// is not draggable, which is how the sortable rows shipped with pointer
// drag dead while every synthetic-DragEvent test passed. Every Spec case
// is rendered here and no enumerated attribute may carry an empty value.
var emptyEnumeratedAttr = regexp.MustCompile(`\s(draggable|contenteditable|spellcheck|translate|autocapitalize|autocorrect|writingsuggestions)=""`)

func TestNoEnumeratedAttributeRendersEmpty(t *testing.T) {
	for _, sp := range Specs() {
		for _, c := range sp.Cases(Kit{}) {
			if m := emptyEnumeratedAttr.FindString(string(c.HTML)); m != "" {
				t.Errorf("%s / %s renders%s: an enumerated attribute needs its keyword (\"true\", \"false\")", sp.Name, c.Name, m)
			}
		}
	}
}
