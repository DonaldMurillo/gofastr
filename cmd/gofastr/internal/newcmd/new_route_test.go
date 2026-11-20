package newcmd

import (
	"os"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
)

func TestNewRouteValidatesPattern(t *testing.T) {
	dir := t.TempDir()
	clitest.CovT_chdir(t, dir)
	for _, path := range []string{"/{wx", "", "/{}/", "/{id}/{id}", "/{rest...}/more", "/a/../b", "POST /x", "/a b", "users", "example.com/x", "/a\tb", "/a\u00a0b"} {
		t.Run(path, func(t *testing.T) {
			code, out := runRoute(t, path)
			if code != 1 || !strings.Contains(out, "invalid route") || strings.Contains(out, "app.Router()") {
				t.Errorf("path %q: exit = %d, want 1 and an invalid route error without a snippet:\n%s", path, code, out)
			}
		})
	}
	for _, path := range []string{"/users/{id}", "/", "/files/{rest...}", "/{$}"} {
		code, out := runRoute(t, path)
		if code != -1 || !strings.Contains(out, routeSnippet("GET", path, "handler")) {
			t.Errorf("path %q: exit = %d, want success and a snippet:\n%s", path, code, out)
		}
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("new route wrote files: %v", entries)
	}
}

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
	// ı uppercases to I and ſ to S: the method is checked before the
	// fold, or "gıt" would pass as GIT.
	for _, m := range []string{"GET\n", "G ET", "GE(T", "GET/", "\x7f", "gıt", "poſt"} {
		if code, out := runRoute(t, "/things", "--method", m); code != 1 {
			t.Errorf("--method %q exit = %d, want 1:\n%s", m, code, out)
		}
	}
	for _, c := range []struct{ handler, method, want string }{
		{"ItemHandler", "GET", `app.Router().Handle("GET", "/things", ItemHandler)`},
		{"h.List", "post", `app.Router().Handle("POST", "/things", h.List)`},
		{"handlers.Things", "delete", `app.Router().Handle("DELETE", "/things", handlers.Things)`},
		// An extension method is a valid token the router serves.
		{"h.Propfind", "PROPFIND", `app.Router().Handle("PROPFIND", "/things", h.Propfind)`},
		// A Go identifier may hold any Unicode letter.
		{"h.Listaño", "GET", `app.Router().Handle("GET", "/things", h.Listaño)`},
		{"größe", "GET", `app.Router().Handle("GET", "/things", größe)`},
	} {
		code, out := runRoute(t, "/things", "--handler", c.handler, "--method", c.method)
		// -1: the command returned without calling exit.
		// Router.Handle takes an http.Handler: the hint names the
		// conversion a plain func such as new handler's needs.
		hint := "pass http.HandlerFunc(" + c.handler + ")"
		if code != -1 || !strings.Contains(out, c.want) || !strings.Contains(out, hint) {
			t.Errorf("--handler %q --method %q: exit %d, want %s in:\n%s", c.handler, c.method, code, c.want, out)
		}
	}
}

// new handler takes the same methods: any HTTP token, PROPFIND included.
func TestNewHandlerRefusesUnknownMethod(t *testing.T) {
	dir := t.TempDir()
	clitest.CovT_chdir(t, dir)
	code := clitest.CovT_capExit(t, func() {
		clitest.CovT_capStdout(t, func() { runNewHandler([]string{"Things", "--method", "G ET"}, false) })
	})
	if code != 1 {
		t.Fatalf("--method \"G ET\" exit = %d, want 1", code)
	}
	code = clitest.CovT_capExit(t, func() {
		clitest.CovT_capStdout(t, func() { runNewHandler([]string{"Dav", "--method", "PROPFIND"}, false) })
	})
	if code != -1 {
		t.Fatalf("--method PROPFIND exit = %d, want success", code)
	}
}
