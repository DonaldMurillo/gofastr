package uihost

import (
	"context"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/framework/axecov"
)

// bareParamScreen is bareScreen for a dynamic route.
type bareParamScreen struct{ bareScreen }

func (s *bareParamScreen) SetParams(map[string]string) {}

// intPathsScreen declares a concrete instance of an :id:int route.
type intPathsScreen struct{ describedScreen }

func (s *intPathsScreen) StaticPaths(ctx context.Context) []map[string]string {
	return []map[string]string{{"id": "1"}}
}

// Strict resolved each pattern Paths() returned as if it were a URL;
// the text "/notes/:id:int" fails its own int constraint, so the screen
// was skipped and its missing title passed boot.
func TestStrictChecksConstrainedRoutes(t *testing.T) {
	a := app.NewApp("demo")
	a.Register("/", &describedScreen{}, nil)
	a.Register("/notes/:id:int", &bareParamScreen{}, nil)
	msg := mountPanic(t, New(a, strictSiteOptions()...))
	if !strings.Contains(msg, `"/notes/:id:int": no title`) {
		t.Fatalf("constrained route's missing title not flagged:\n%s", msg)
	}
}

func TestStrictAxeDemandsConstrainedRoute(t *testing.T) {
	t.Setenv("GOFASTR_AXE_COVERAGE_DIR", "") // cmd/affected sets it; this test resolves from cwd
	t.Chdir(t.TempDir())
	t.Setenv("GOFASTR_DEV", "1")
	if err := axecov.Record(".", "/", "dark"); err != nil {
		t.Fatal(err)
	}
	a := app.NewApp("demo")
	a.Register("/", &describedScreen{}, nil)
	a.Register("/notes/:id:int", &intPathsScreen{}, nil)
	msg := mountPanic(t, New(a, strictSiteOptions()...))
	if !strings.Contains(msg, `axe coverage: screen "/notes/:id:int"`) {
		t.Fatalf("uncovered constrained route not demanded:\n%s", msg)
	}
	// A scan of a concrete URL covers it.
	if err := axecov.Record(".", "/notes/1", "dark"); err != nil {
		t.Fatal(err)
	}
	if msg := mountPanic(t, New(a, strictSiteOptions()...)); msg != "" {
		t.Fatalf("scanned constrained route still flagged:\n%s", msg)
	}
}

// A brace that never closes is a literal, not a parameter:
// normalizeRoutePath leaves "/lit/{x" untouched and the router serves it
// as an exact route, so the dynamic-route rule (":seg" or a complete
// "{seg}") must read it static. Strict then demands its axe scan like
// any literal page instead of warning it invisible as a dynamic route
// that declares no StaticPaths.
func TestStrictAxeDemandsLiteralBraceRoute(t *testing.T) {
	t.Setenv("GOFASTR_AXE_COVERAGE_DIR", "") // cmd/affected sets it; this test resolves from cwd
	t.Chdir(t.TempDir())
	t.Setenv("GOFASTR_DEV", "1")
	if err := axecov.Record(".", "/", "dark"); err != nil {
		t.Fatal(err)
	}
	a := app.NewApp("demo")
	a.Register("/", &describedScreen{}, nil)
	a.Register("/lit/{x", &describedScreen{}, nil)
	msg := mountPanic(t, New(a, strictSiteOptions()...))
	if !strings.Contains(msg, `axe coverage: screen "/lit/{x"`) {
		t.Fatalf("literal-brace route not demanded:\n%s", msg)
	}
	// A scan of the literal path covers it.
	if err := axecov.Record(".", "/lit/{x", "dark"); err != nil {
		t.Fatal(err)
	}
	if msg := mountPanic(t, New(a, strictSiteOptions()...)); msg != "" {
		t.Fatalf("scanned literal-brace route still flagged:\n%s", msg)
	}
}
