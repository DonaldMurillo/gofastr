package headless

import "testing"

// The spacer is space and nothing else. It is hidden from assistive
// tech and renders no content, because the facts it separates — the
// term, the value — are neighbours in the tree already, and a screen
// reader user gains nothing from an element that says nothing between
// them.
func TestSpacerIsHiddenAndEmpty(t *testing.T) {
	got := Spacer(SpacerProps{Grow: 1}, nil)
	has(t, got, `<span aria-hidden="true" data-hui-grow="1"></span>`,
		"a spacer is anything other than an empty, hidden span carrying its grow factor")
}

// Grow is a factor a stylesheet wires for 1 through 4. A 0 is a
// spacer that cannot grow — a typo, since the zero value means 1 in
// the styled layer above — and a 5 is a factor no rule carries, which
// would silently render as 1. Both are refused with the reason.
func TestSpacerRefusesAGrowNoRuleCarries(t *testing.T) {
	mustRefuse(t, "a grow of 0", func() {
		Spacer(SpacerProps{Grow: 0}, nil)
	})
	mustRefuse(t, "a grow of 5", func() {
		Spacer(SpacerProps{Grow: 5}, nil)
	})
}

// A leader ties a term to its value; a rule divides the space they
// share. Two lines in one space is two answers to one question, and
// whichever drew last would win in the stylesheet while both claimed
// to be drawn here.
func TestSpacerRefusesALeaderAndARuleAtOnce(t *testing.T) {
	mustRefuse(t, "a leader and a rule in the same space", func() {
		Spacer(SpacerProps{Grow: 1, Leader: true, Rule: true}, nil)
	})
}
