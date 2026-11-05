package uihost

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
)

func TestMountRefusesUnknownIntercept(t *testing.T) {
	for _, also := range []bool{false, true} {
		application := app.NewApp("intercepts")
		application.Register("/known", &errdocPage{}, nil)
		option := app.InterceptFrom("/misspelled", app.ScreenDrawer)
		if also {
			option = app.InterceptFrom("/known", app.ScreenDrawer, "/misspelled")
		}
		application.Register("/panel", &errdocPage{}, nil, option)
		if got := panicFromMount(t, New(application)); !strings.Contains(got, "/misspelled") || !strings.Contains(got, "/panel") {
			t.Errorf("mount did not diagnose the target and missing origin: %q", got)
		}
	}
}
