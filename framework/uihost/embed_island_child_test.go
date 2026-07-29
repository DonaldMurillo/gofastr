package uihost

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/island"
	"github.com/DonaldMurillo/gofastr/core/render"
	fembed "github.com/DonaldMurillo/gofastr/framework/embed"
)

// reachStopPackages in framework/uihost/embed_actions.go used to list
// "core-ui/island" as a package the walk refuses to descend into. The stated
// reason covers core-ui/app only ("a back-reference to the App would make
// every component reachable"); island.Island is not a host back-reference —
// it is a one-field wrapper, `Component component.Component`, and it is the
// framework's primary composition primitive for exactly the interactive
// surfaces an embed hosts.
//
// So a root that HOLDS an island (the shape the commit says the value walk
// handles — "struct fields including unexported ones") is not covered: the
// walk stops at *island.Island and never reaches the component inside it.
type islandHoldingParent struct{ isl *island.Island }

func (c *islandHoldingParent) Render() render.HTML { return c.isl.Render() }

func TestGateFollowsIslandChild(t *testing.T) {
	child := &childActionComp{}
	application := app.NewApp("embed island child")
	screen := app.NewScreen("/reports", &islandHoldingParent{isl: island.NewIsland("panel", child)})
	application.RegisterScreen(screen, nil)
	application.RegisterScreen(app.NewScreen("/child-action", child), nil)

	eh, err := fembed.New(fembed.Config{
		Surfaces: []fembed.Surface{{
			Name:    "reports",
			Screen:  screen,
			Origins: []string{embedTestOrigin},
		}},
		BurnStore: fembed.NewMemoryBurnStore(),
	})
	if err != nil {
		t.Fatalf("embed.New: %v", err)
	}
	eh.SetKeys([]byte("nonce-key-nonce-key-nonce-key-32"), []byte("grant-key-grant-key-grant-key-32"))

	host := New(application, WithEmbed(eh))
	got := panicFromMount(t, host)

	if js := host.GetActionJS(); !strings.Contains(js, `G._serverActionFor("child-action"`) {
		t.Fatalf("test setup: the child's server action was not compiled into the bundle:\n%s", js)
	}
	if got == "" {
		t.Fatal("boot gate accepted an embeddable screen whose island wraps a component with a " +
			"registered server action — reachStopPackages stops the walk at core-ui/island, so the " +
			"child inside island.Island.Component is never seen, and the action still ships to the frame")
	}
}
