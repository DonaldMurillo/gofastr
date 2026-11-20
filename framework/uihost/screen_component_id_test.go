package uihost

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// undescribedIDComp is the screen the app generator emits for a screen
// with no description: it declares its ComponentID but no
// ScreenDescription.
type undescribedIDComp struct{}

func (c *undescribedIDComp) Render() render.HTML {
	return render.Raw(`<div data-component="screen-home"><form data-action-mount="load"></form></div>`)
}
func (c *undescribedIDComp) Actions()                               { component.On("load", nil) }
func (c *undescribedIDComp) ScreenTitle(ctx context.Context) string { return "Home" }
func (c *undescribedIDComp) ScreenType() app.ScreenType             { return app.ScreenPage }
func (c *undescribedIDComp) ComponentID() string                    { return "screen-home" }

func TestDeclaredComponentIDNeedsNoDescription(t *testing.T) {
	a := app.NewApp("component-id")
	a.RegisterScreen(app.NewScreen("/", &undescribedIDComp{}), nil)
	ds := New(a)

	w := httptest.NewRecorder()
	ds.ServeHTTP(w, httptest.NewRequest("GET", "/", nil))

	if tag := `src="/__gofastr/widget/screen-home.js?v=`; !strings.Contains(w.Body.String(), tag) {
		ds.mu.RLock()
		defer ds.mu.RUnlock()
		t.Fatalf("page renders data-component=%q but gets no %s tag, so its data-action-mount never fires; actions compiled under %v",
			"screen-home", tag, ds.actionHash)
	}
}
