package uihost

import (
	"reflect"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/core/router"
	fembed "github.com/DonaldMurillo/gofastr/framework/embed"
)

// serverActionScreenComp is a screen component that registers an action whose
// client handler calls G.serverAction — the one thing that does not work inside
// a frame, because the action registry is app-global with no surface
// relationship. Actions() with component.On makes it InteractiveComponent, so
// the host compiles (and the boot walk extracts) its actions.
type serverActionScreenComp struct{}

func (serverActionScreenComp) Render() render.HTML { return render.HTML("<p>save</p>") }

func (c *serverActionScreenComp) Actions() {
	// The ClientJS carries G.serverAction( — exactly the call the action
	// compiler recognises (it rewrites it to G._serverActionFor) and the one
	// the runtime ships to POST /__gofastr/action, which a frame cannot reach.
	component.On("save", func(_ *component.ComponentContext) {},
		component.WithClientJS(`G.serverAction("save")`))
}

// plainActionScreenComp registers an ordinary client-only action (no
// G.serverAction). A surface rendering it must boot cleanly — the walk targets
// the server-action property, not action registration in general.
type plainActionScreenComp struct{}

func (plainActionScreenComp) Render() render.HTML { return render.HTML("<p>ok</p>") }

func (c *plainActionScreenComp) Actions() {
	component.On("click", func(_ *component.ComponentContext) {},
		component.WithClientJS(`G.setState("n", 1)`))
}

type changingActionScreenComp struct {
	calls int
}

func (changingActionScreenComp) Render() render.HTML { return render.HTML("<p>save</p>") }

func (c *changingActionScreenComp) Actions() {
	c.calls++
	js := `G.setState("saved", true)`
	if c.calls == 1 {
		js = `G.serverAction("save")`
	}
	component.On("save", func(_ *component.ComponentContext) {}, component.WithClientJS(js))
}

type whitespaceServerActionScreenComp struct{}

func (whitespaceServerActionScreenComp) Render() render.HTML {
	return render.HTML("<p>save</p>")
}

func (c *whitespaceServerActionScreenComp) Actions() {
	component.On("save", func(_ *component.ComponentContext) {},
		component.WithClientJS(`G.serverAction ("save")`))
}

type routeOnlyEmbedScreen string

func (s routeOnlyEmbedScreen) RoutePath() string { return string(s) }

func buildEmbedHostWithScreen(t *testing.T, comp component.Component, route string) (*app.App, *fembed.Host) {
	t.Helper()
	application := app.NewApp("server-action embed")
	scr := app.NewScreen(route, comp)
	application.RegisterScreen(scr, nil)
	eh, err := fembed.New(fembed.Config{
		Surfaces: []fembed.Surface{{
			Name:    "reports",
			Screen:  scr,
			Origins: []string{embedTestOrigin},
		}},
		BurnStore: fembed.NewMemoryBurnStore(),
	})
	if err != nil {
		t.Fatalf("embed.New: %v", err)
	}
	eh.SetKeys([]byte("nonce-key-nonce-key-nonce-key-32"), []byte("grant-key-grant-key-grant-key-32"))
	return application, eh
}

// A surface whose screen registers a server action must panic at boot, not be
// discovered at runtime inside a customer's page.
func TestEmbedSurfaceWithServerActionPanicsAtBoot(t *testing.T) {
	application, eh := buildEmbedHostWithScreen(t, &serverActionScreenComp{}, "/reports")
	ds := New(application, WithEmbed(eh))

	got := panicFromMount(t, ds)
	if got == "" {
		t.Fatal("Mount did not panic for a surface whose screen registers " +
			"G.serverAction — the failure must surface at boot, not in a customer's page")
	}
	// The panic names every link a developer needs to find it: the surface, the
	// component (by route), the action, and a working alternative.
	for _, want := range []string{"reports", "/reports", "save", "island"} {
		if !strings.Contains(got, want) {
			t.Errorf("panic message missing %q:\n%s", want, got)
		}
	}
}

// The walk targets the server-action property, not action registration: a
// surface whose component registers an ordinary client action boots cleanly.
func TestEmbedSurfaceWithPlainActionBootsCleanly(t *testing.T) {
	application, eh := buildEmbedHostWithScreen(t, &plainActionScreenComp{}, "/reports")
	ds := New(application, WithEmbed(eh))

	if got := panicFromMount(t, ds); got != "" {
		t.Fatalf("Mount panicked for a surface whose component registers no "+
			"server action — the walk flagged a plain client action:\n%s", got)
	}
}

func TestEmbedGateInspectsCompiledActionRegistry(t *testing.T) {
	comp := &changingActionScreenComp{}
	application, eh := buildEmbedHostWithScreen(t, comp, "/reports")
	ds := New(application, WithEmbed(eh))

	got := panicFromMount(t, ds)
	if got == "" {
		t.Fatal("Mount did not reject the server action from the registry compiled into actions.js")
	}
	if comp.calls != 1 {
		t.Fatalf("Actions called %d times, want 1; the gate must inspect the compiled registry", comp.calls)
	}
}

func TestEmbedGateResolvesCustomScreenThroughRouter(t *testing.T) {
	application := app.NewApp("custom embed screen")
	application.RegisterScreen(app.NewScreen("/reports", &serverActionScreenComp{}), nil)
	eh, err := fembed.New(fembed.Config{
		Surfaces: []fembed.Surface{{
			Name:    "reports",
			Screen:  routeOnlyEmbedScreen("/reports"),
			Origins: []string{embedTestOrigin},
		}},
		BurnStore: fembed.NewMemoryBurnStore(),
	})
	if err != nil {
		t.Fatalf("embed.New: %v", err)
	}
	eh.SetKeys([]byte("nonce-key-nonce-key-nonce-key-32"), []byte("grant-key-grant-key-grant-key-32"))

	got := panicFromMount(t, New(application, WithEmbed(eh)))
	if got == "" {
		t.Fatal("Mount did not reject the server action rendered at a custom embed screen's route")
	}
}

func TestEmbedGateRejectsWhitespaceServerActionCall(t *testing.T) {
	application, eh := buildEmbedHostWithScreen(t, &whitespaceServerActionScreenComp{}, "/reports")

	got := panicFromMount(t, New(application, WithEmbed(eh)))
	if got == "" {
		t.Fatal("Mount did not reject G.serverAction with whitespace before the opening parenthesis")
	}
	if !strings.Contains(got, "G.serverAction(") {
		t.Fatalf("panic must name the canonical spelling G.serverAction(:\n%s", got)
	}
}

// childActionComp is registered as a screen of its own AND rendered inside an
// embeddable root. AutoCompileActions compiles it under its own id, and
// handleEmbedRuntimeJS ships every compiled registry into the frame — so its
// server action reaches the customer's page even though the embeddable root
// declares none.
type childActionComp struct{}

func (*childActionComp) Render() render.HTML {
	return render.HTML(`<button data-component="child-action" data-action="save">Save</button>`)
}

func (*childActionComp) Actions() {
	component.On("save", func(*component.ComponentContext) {},
		component.WithClientJS(`G.serverAction("save")`))
}

type parentOfActionComp struct{ child *childActionComp }

func (c *parentOfActionComp) Render() render.HTML { return c.child.Render() }

// Guards the gate that only inspected the SURFACE ROOT's own action registry: a
// root that renders a child passed boot while the child's server action shipped
// to the frame and failed in the customer's page.
func TestEmbedGateFlagsRenderedChildAction(t *testing.T) {
	child := &childActionComp{}
	application := app.NewApp("embed child action")
	screen := app.NewScreen("/reports", &parentOfActionComp{child: child})
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
	if got == "" {
		if js := host.GetActionJS(); !strings.Contains(js, `G._serverActionFor("child-action"`) {
			t.Fatalf("test setup: the child's server action was not compiled into the runtime:\n%s", js)
		}
		t.Fatal("Mount accepted an embeddable screen whose rendered child has a registered server action")
	}
	// The panic has to name the CHILD, not just the surface — "somewhere in
	// this tree" is not a message anyone can act on.
	for _, want := range []string{"reports", "/reports", "child-action", "save", "island"} {
		if !strings.Contains(got, want) {
			t.Errorf("panic message missing %q:\n%s", want, got)
		}
	}
}

// A component that is NOT in the surface's tree keeps its server action, even
// though its compiled JS sits in the same app-global bundle. The gate keys on
// reachability from the surface; flagging every server action in the app would
// make an embeddable surface anywhere ban them everywhere.
func TestEmbedGateIgnoresUnreachedServerAction(t *testing.T) {
	application := app.NewApp("embed unreached action")
	screen := app.NewScreen("/reports", &plainActionScreenComp{})
	application.RegisterScreen(screen, nil)
	application.RegisterScreen(app.NewScreen("/elsewhere", &serverActionScreenComp{}), nil)

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

	if got := panicFromMount(t, New(application, WithEmbed(eh))); got != "" {
		t.Fatalf("Mount panicked for a server action on an unrelated screen:\n%s", got)
	}
}

// The walk follows values, not types: a nil child contributes nothing. A gate
// that walked declared FIELD TYPES would refuse to boot on a component that
// merely can hold a child it does not hold.
func TestEmbedGateSkipsNilChild(t *testing.T) {
	application := app.NewApp("embed nil child")
	screen := app.NewScreen("/reports", &parentOfActionComp{})
	application.RegisterScreen(screen, nil)
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

	if got := panicFromMount(t, New(application, WithEmbed(eh))); got != "" {
		t.Fatalf("Mount panicked for a child field that is nil:\n%s", got)
	}
}

// A cycle between two components must not hang the boot walk.
type cyclicComp struct {
	peer *cyclicComp
	kid  *childActionComp
}

func (c *cyclicComp) Render() render.HTML { return render.HTML("<p>cycle</p>") }

func TestReachWalkTerminatesOnCycle(t *testing.T) {
	a, b := &cyclicComp{}, &cyclicComp{kid: &childActionComp{}}
	a.peer, b.peer = b, a
	got := reachableComponentTypes(a)
	if !got[reflect.TypeFor[childActionComp]()] {
		t.Fatal("walk did not reach the child on the far side of a cycle")
	}
}

func TestServerActionCallScansPastNonCallAndUnicodeWhitespace(t *testing.T) {
	clientJS := "// G.serverAction helper\nG.serverAction\u00a0(\"save\")"
	if !serverActionCall(clientJS) {
		t.Fatal("serverActionCall missed a later call separated by Unicode whitespace")
	}
}

// panicFromMount runs ds.Mount and returns the recovered panic as a string
// ("" when Mount did not panic).
func panicFromMount(t *testing.T, ds *UIHost) string {
	t.Helper()
	var got any
	func() {
		defer func() { got = recover() }()
		ds.Mount(router.New())
	}()
	if got == nil {
		return ""
	}
	return strings.TrimSpace(panicString(got))
}

func panicString(r any) string {
	switch v := r.(type) {
	case string:
		return v
	case error:
		return v.Error()
	default:
		return ""
	}
}
