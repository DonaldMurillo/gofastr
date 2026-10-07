package headless

import (
	"testing"

	"github.com/DonaldMurillo/gofastr/core/render"
)

// The kit marking gate renders every slot filled or every slot empty;
// these pin the mixed cases between, where one slot's content decides
// which of its neighbours stay the component's own.

// A card's Action makes the header a slot ancestor: the header is left
// unmarked, the heading beside the action is marked, and an empty body
// is the component's own.
func TestCardActionLeavesHeaderReachable(t *testing.T) {
	cls := Classes{PartCardHeader: "h", PartTitle: "t", PartCardAction: "a", PartCardBody: "b"}
	got := Card(CardProps{Title: "Recent", Action: render.HTML("<b>all</b>")}, cls)
	has(t, got, `<div class="h"><h3 class="t" data-cui-internal="">Recent</h3>`, "the header holding an action was marked, or its heading was not")
	has(t, got, `<div class="a"><b>all</b></div>`, "the action was marked")
	has(t, got, `<div class="b" data-cui-internal=""></div>`, "the empty body was not marked")

	got = Card(CardProps{Title: "Recent"}, cls, render.HTML("<p>x</p>"))
	has(t, got, `<div class="h" data-cui-internal=""><h3 class="t">Recent</h3>`, "a header with no action was not marked whole")
	has(t, got, `<div class="b"><p>x</p></div>`, "the body holding content was marked")
}

// An event's Icon makes the item a slot ancestor: the item and the
// mark holding the icon stay reachable, and the text beside it is the
// component's own.
func TestTimelineIconLeavesMarkReachable(t *testing.T) {
	cls := Classes{PartTimelineItem: "i", PartTimelineMark: "m", PartTimelineBody: "bd"}
	got := Timeline(TimelineProps{Events: []Event{{Title: "Edited", Icon: render.HTML("<i>p</i>")}}}, cls)
	has(t, got, `<li class="i">`, "the item holding an icon was marked")
	has(t, got, `<span aria-hidden="true" class="m"><i>p</i></span>`, "the mark holding an icon was marked")
	has(t, got, `<div class="bd" data-cui-internal="">`, "the text beside the icon was not marked")

	got = Timeline(TimelineProps{Events: []Event{{Title: "Edited"}}}, cls)
	has(t, got, `<li class="i" data-cui-internal="">`, "an item with no icon or body was not marked whole")
}
