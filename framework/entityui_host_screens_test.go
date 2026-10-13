package framework

import (
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/framework/entityui"
	"github.com/DonaldMurillo/gofastr/framework/uihost"
)

type paramScreen struct{ isAgenticScreen }

func (*paramScreen) SetParams(map[string]string) {}

// The entityui host answers HasScreen from the mounted uihost's screen
// router, so a list can warn when its row links lead nowhere (#503).
func TestEntityUIHostHasScreen(t *testing.T) {
	a := NewApp(WithoutDefaultMiddleware())
	if _, known := (entityUIHost{a: a}).HasScreen("/orders/1"); known {
		t.Fatal("no UI host mounted: HasScreen must answer unknown")
	}

	site := app.NewApp("screens")
	site.RegisterScreen(app.NewScreen("/orders", &isAgenticScreen{}), nil)
	site.RegisterScreen(app.NewScreen("/orders/:id", &paramScreen{}), nil)
	a.Mount(uihost.New(site))

	h := entityUIHost{a: a}
	if has, known := h.HasScreen("/orders/42"); !known || !has {
		t.Errorf("HasScreen(/orders/42) = %v, %v; want true, true", has, known)
	}
	if has, known := h.HasScreen("/issues/42"); !known || has {
		t.Errorf("HasScreen(/issues/42) = %v, %v; want false, true", has, known)
	}
}

// Both halves of the bridge must keep the method set entityui looks for:
// a renamed method compiles fine and silently turns the warning off.
var (
	_ entityui.ScreenHost = entityUIHost{}
	_ entityui.ScreenHost = (*uihost.UIHost)(nil)
)
