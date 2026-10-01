package uihost

// Mount must refuse a group whose Requires key has no resolver
// declaration: the check Requires exists to add (404/refuse
// in the policy phase, before any Load) would otherwise be silently
// dropped, and nothing downstream can tell. The unit twin over
// Router.ValidateRequires lives in core-ui/app.

import (
	"context"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
)

// TestMountPanicsOnRequiresNameNothingDeclares: mounting a host whose
// app has a group requiring a key with no resolver (issue, beside a
// resolver only for project) panics at Mount, before any request is
// served — the same refuse-at-boot posture ValidateFills takes.
func TestMountPanicsOnRequiresNameNothingDeclares(t *testing.T) {
	application := app.NewApp("requires-check")
	application.RegisterScreen(app.NewScreen("/", &errdocPage{}), nil)
	project := app.NewKey[string]("project")
	issue := app.NewKey[string]("issue")
	g := app.NewScreenGroup("/x", nil)
	g.Resolve(project.From(func(ctx context.Context) (string, error) {
		return "p", nil
	}))
	g.Requires(issue) // declared nowhere
	g.Screen(app.NewScreen("/x", &errdocPage{}), nil)
	application.Router.ScreenGroup(g)

	got := panicFromMount(t, New(application))
	want := `app: group "/x/" requires resolver "issue", but no resolver for that key is visible to "/x"`
	if got != want {
		t.Fatalf("Mount must refuse the undeclared Requires key:\n got %q\nwant %q", got, want)
	}
}
