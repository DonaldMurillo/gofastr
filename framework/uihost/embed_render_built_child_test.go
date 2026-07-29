package uihost

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core/render"
	fembed "github.com/DonaldMurillo/gofastr/framework/embed"
)

// The boot gate walks the surface component's VALUE graph — pointers,
// interfaces, struct fields, slices, arrays, map keys and map values. That
// finds a child a parent HOLDS.
//
// It cannot find a child a parent BUILDS, because composing in Render is the
// normal Go spelling and the child does not exist until Render runs:
//
//	func (c *Root) Render() render.HTML { return Panel{}.Render() }
//
// Nothing in the value graph references Panel, so reachableComponentTypes
// never sees it — while AutoCompileActions still compiled Panel's registry and
// GetActionJS concatenates every compiled registry into the ONE bundle
// handleEmbedRuntimeJS ships to the frame.
//
// This test pins that limit deliberately, so nobody re-derives the claim that
// the boot walk is exact. The covering gate for this shape is the static
// analyzer at build time: cmd/check-embed/embedcheck emits an Unresolved note
// for a Render-constructed child it cannot read, and `gofastr build` now FAILS
// on a note (see cmd/gofastr/build.go). Neither gate is total on its own; the
// pair is.
type renderBuiltChildParent struct{}

func (renderBuiltChildParent) Render() render.HTML {
	// The child is constructed here, at render time. No field holds it.
	return (&childActionComp{}).Render()
}

func TestBootWalkCannotSeeRenderBuiltChild(t *testing.T) {
	application := app.NewApp("embed render-built child")
	screen := app.NewScreen("/reports", renderBuiltChildParent{})
	application.RegisterScreen(screen, nil)
	// The child is a screen of its own too, so its registry is compiled and
	// lands in the app-global bundle.
	application.RegisterScreen(app.NewScreen("/child-action", &childActionComp{}), nil)

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

	// The action really does ship into the frame's bundle — this is not a
	// hypothetical shape.
	js := host.GetActionJS()
	if !strings.Contains(js, `G._serverActionFor("child-action"`) {
		t.Fatalf("test setup: the child's server action was not compiled into the runtime bundle:\n%s", js)
	}
	// The boot walk does NOT catch it, and that is a property of walking
	// values rather than an oversight. If this ever starts panicking the walk
	// gained render-time visibility — good news, but update the build gate's
	// reasoning and embed.md before deleting this test.
	if got != "" {
		t.Fatalf("boot walk unexpectedly caught a render-built child: %s\n"+
			"If this is a real improvement, cmd/gofastr/build.go and embed.md both "+
			"describe the note-fails-build split that exists to cover this shape.", got)
	}
}
