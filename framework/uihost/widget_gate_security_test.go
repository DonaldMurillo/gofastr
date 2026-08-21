package uihost

import (
	"net/http"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/widget"
	"github.com/DonaldMurillo/gofastr/core/router"
)

// widget.Definition.RequireSession fails closed when no host installed a
// session predicate, so uihost must install one at mount time;
// otherwise the gate can only ever say no and the knob is unusable.
//
// This pins the wiring, not the gate itself (core-ui/widget owns that).
func TestUIHostInstallsWidgetSessionCheck(t *testing.T) {
	widget.SetSessionCheck(nil)
	t.Cleanup(func() { widget.SetSessionCheck(nil) })

	a := app.NewApp("x")
	a.Register("/", &plainComp{}, nil)
	ds := New(a)
	ds.Mount(router.New())

	check := widget.SessionCheck()
	if check == nil {
		t.Fatal("uihost.Routes did not install a widget session check; RequireSession would 403 forever")
	}
	// An unauthenticated request must still be refused by it.
	if check(&http.Request{Header: http.Header{}}) {
		t.Error("installed check accepted a request with no session cookie")
	}
}
