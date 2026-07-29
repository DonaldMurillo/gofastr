package uihost

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// mapKeyActionParent renders a child stored as a map key. Pointer keys are
// comparable even when the pointed-to component contains maps or slices.
type mapKeyActionParent struct {
	children map[*childActionComp]struct{}
}

func (p *mapKeyActionParent) Render() render.HTML {
	for child := range p.children {
		return child.Render()
	}
	return ""
}

// The boot walk follows map values but not map keys. This leaves a rendered
// child out of the reachability set even though its compiled server action is
// included in the frame's global action bundle.
func TestGateFollowsMapKeyChild(t *testing.T) {
	child := &childActionComp{}
	root := &mapKeyActionParent{children: map[*childActionComp]struct{}{child: {}}}
	application, embedHost := buildEmbedHostWithScreen(t, root, "/reports")
	application.RegisterScreen(app.NewScreen("/child-action", child), nil)

	host := New(application, WithEmbed(embedHost))
	got := panicFromMount(t, host)
	if got != "" {
		return
	}
	if js := host.GetActionJS(); !strings.Contains(js, `G._serverActionFor("child-action"`) {
		t.Fatalf("test setup: the map-key child's server action was not compiled into the frame bundle:\n%s", js)
	}
	t.Fatal("Mount accepted an embeddable screen that renders a map-key child with a server action")
}
