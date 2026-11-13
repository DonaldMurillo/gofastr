package newcmd

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
)

// runRoute runs `gofastr new route` and returns its exit code and output.
func runRoute(t *testing.T, args ...string) (int, string) {
	t.Helper()
	var code int
	out := clitest.CovT_capStdout(t, func() {
		code = clitest.CovT_capExit(t, func() { runNewRoute(args) })
	})
	return code, out
}

// The handler lands at an expression slot of the printed Go line and the
// method in its route pattern, so both are checked before printing.
func TestNewRouteRefusesBadHandler(t *testing.T) {
	for _, h := range []string{"list-things", "a b", "func", "main", "_", "h.", ".List", "a..b", "h.func", "x)"} {
		code, out := runRoute(t, "/things", "--handler", h)
		if code != 1 {
			t.Errorf("--handler %q exit = %d, want 1:\n%s", h, code, out)
		}
		if strings.Contains(out, "app.Router()") {
			t.Errorf("--handler %q printed a snippet:\n%s", h, out)
		}
	}
	for _, m := range []string{"G_ET", "GET\n", "FETCH"} {
		if code, out := runRoute(t, "/things", "--method", m); code != 1 {
			t.Errorf("--method %q exit = %d, want 1:\n%s", m, code, out)
		}
	}
	for _, c := range []struct{ handler, method, want string }{
		{"ItemHandler", "GET", `app.Router().Handle("GET", "/things", ItemHandler)`},
		{"h.List", "post", `app.Router().Handle("POST", "/things", h.List)`},
		{"handlers.Things", "delete", `app.Router().Handle("DELETE", "/things", handlers.Things)`},
	} {
		code, out := runRoute(t, "/things", "--handler", c.handler, "--method", c.method)
		// -1: the command returned without calling exit.
		if code != -1 || !strings.Contains(out, c.want) {
			t.Errorf("--handler %q --method %q: exit %d, want %s in:\n%s", c.handler, c.method, code, c.want, out)
		}
	}
}

// new handler takes the same method set.
func TestNewHandlerRefusesUnknownMethod(t *testing.T) {
	dir := t.TempDir()
	clitest.CovT_chdir(t, dir)
	code := clitest.CovT_capExit(t, func() {
		clitest.CovT_capStdout(t, func() { runNewHandler([]string{"Things", "--method", "G_ET"}, false) })
	})
	if code != 1 {
		t.Fatalf("--method G_ET exit = %d, want 1", code)
	}
}
