package uihost

import (
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/widget"
	"github.com/DonaldMurillo/gofastr/core-ui/widget/preset"
	"github.com/DonaldMurillo/gofastr/core/router"
)

func bootedHost(t *testing.T, beforeBoot func(r *router.Router)) {
	t.Helper()
	widget.IsolateForTest(t)
	a := app.NewApp("demo")
	a.Register("/", &describedScreen{}, nil)
	ds := New(a)
	r := router.New()
	ds.Mount(r)
	if beforeBoot != nil {
		beforeBoot(r)
	}
	ds.ValidateBoot()
}

func TestBootMountsTheDefaultToastStack(t *testing.T) {
	bootedHost(t, nil)
	d, ok := widget.Lookup(DefaultToastStack)
	if !ok {
		t.Fatal("ValidateBoot mounted no toast stack; a header toast would have no region to land in")
	}
	if !preset.IsToastStack(d) {
		t.Fatalf("the default stack is not a preset toast stack: %+v", d)
	}
}

func TestBootKeepsTheAppsOwnToastStack(t *testing.T) {
	bootedHost(t, func(r *router.Router) {
		widget.MountBuilder(r, preset.ToastStack("mine"))
	})
	if _, ok := widget.Lookup(DefaultToastStack); ok {
		t.Fatal("ValidateBoot mounted the default stack beside the app's own")
	}
	if _, ok := widget.Lookup("mine"); !ok {
		t.Fatal("the app's own stack is gone")
	}
}
