package uihost

import (
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
