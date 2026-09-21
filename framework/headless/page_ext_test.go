package headless

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/render"
)

// The props this change added to existing primitives, each pinned by
// what it renders: a golden case covers the shape, and these hold the
// behaviour the shape cannot show on its own.

// Stack's main-axis distribution is a named variant, the same
// vocabulary Cluster already used.
func TestStackJustifiesOnItsOwnAxis(t *testing.T) {
	got := Stack(StackProps{Justify: "between"}, Classes{PartRoot: "s", "root--justify-between": "s--between"})
	has(t, got, `class="s s--between"`, "the justify variant did not land")
}

// Alert's Body is markup in its own region, after the prose.
func TestAlertBodyRendersMarkupAfterTheProse(t *testing.T) {
	got := Alert(AlertProps{Title: "Attention", Text: "Two apps.", Body: render.HTML("<ul><li>blog</li></ul>")}, nil)
	has(t, got, "<ul><li>blog</li></ul>", "the body markup did not render")
	if prose, body := strings.Index(string(got), "Two apps."), strings.Index(string(got), "<ul>"); body < prose {
		t.Errorf("the body renders before the prose it elaborates:\n%s", got)
	}
}

// Card's href makes the whole card the link, and a href the anchor
// policy refuses is the developer's mistake, said at render the way
// every configured href in this package is.
func TestCardHrefMakesTheSurfaceTheLink(t *testing.T) {
	got := Card(CardProps{Title: "blog", Href: "/apps/blog"}, nil, render.HTML("<p>Up.</p>"))
	has(t, got, `<a href="/apps/blog">`, "the card did not render as a link")
	has(t, got, "<h3>blog</h3>", "the linked card lost its heading")

	refuse(t, "Href", func() {
		Card(CardProps{Title: "blog", Href: "javascript:alert(1)"}, nil)
	})
}

// Steps: a href renders the row as an anchor, a state outside the
// known three is refused, and an explicit marker replaces the glyph
// the state would have drawn.
func TestStepsHrefStateAndMarker(t *testing.T) {
	got := Steps(StepsProps{Steps: []Step{
		{Label: "Source", Href: "/setup/source", Marker: "01"},
		{Label: "Build"},
	}, Current: 2}, nil)
	has(t, got, `<a href="/setup/source">`, "a step with a href did not render as an anchor")
	has(t, got, `<span aria-hidden="true">01</span>`, "the explicit marker did not replace the glyph")

	refuse(t, "Href", func() {
		Steps(StepsProps{Steps: []Step{{Label: "Source", Href: "javascript:x"}}}, nil)
	})
	refuse(t, "State", func() {
		Steps(StepsProps{Steps: []Step{{Label: "Source", State: "finished"}}}, nil)
	})
	// An explicit state still marks the current step, so a rail whose
	// later step finished while an earlier one is open says both.
	overridden := Steps(StepsProps{Steps: []Step{
		{Label: "Source"}, {Label: "Build", State: "done"}, {Label: "Deploy"},
	}, Current: 1}, nil)
	has(t, overridden, `<li aria-current="step" data-state="current">`, "the derived current state was lost")
	has(t, overridden, `<li data-state="done"><span><span aria-hidden="true">✓</span>`, "the explicit done state did not win")
}

// Exactly one step may be current: Current on one step and an
// explicit State "current" on another is refused naming both, and so
// are two explicit currents. An explicit current that names the same
// step as Current agrees, and renders one aria-current.
func TestStepsAllowsExactlyOneCurrentStep(t *testing.T) {
	refuse(t, "current", func() {
		Steps(StepsProps{Steps: []Step{
			{Label: "Source"}, {Label: "Build"}, {Label: "Deploy", State: "current"},
		}, Current: 2}, nil)
	})
	refuse(t, "current", func() {
		Steps(StepsProps{Steps: []Step{
			{Label: "Source", State: "current"}, {Label: "Build", State: "current"},
		}}, nil)
	})
	agreed := Steps(StepsProps{Steps: []Step{
		{Label: "Source"}, {Label: "Build", State: "current"},
	}, Current: 2}, nil)
	if n := strings.Count(string(agreed), `aria-current="step"`); n != 1 {
		t.Errorf("the agreed current step rendered %d aria-current marks:\n%s", n, agreed)
	}
}

// Timeline's meta rides in the header row after the title, and the
// title stays first in the tree.
func TestTimelineMetaQualifiesTheTitle(t *testing.T) {
	got := Timeline(TimelineProps{Events: []Event{{Title: "Role granted", Meta: "by dom"}}}, nil)
	has(t, got, "<p>Role granted</p>", "the title is not a paragraph in the header")
	has(t, got, "<span>by dom</span>", "the meta line did not render")
	if title, meta := strings.Index(string(got), "Role granted"), strings.Index(string(got), "by dom"); title > meta {
		t.Errorf("the attribution is read before the event it attributes:\n%s", got)
	}
}
