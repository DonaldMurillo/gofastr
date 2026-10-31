package desktop_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/battery/desktop"
)

// Native events, posted messages and deep links were evaluated into a
// window whatever page it showed. An external link the runtime lets
// through navigates the window off the app, and a page there that
// defined window.__gofastr.desktop._dispatch received every event and
// every deep link, an OAuth callback's code included. Every script the
// battery evals into a page now runs only on the app's own origin.
func TestEventsReachOnlyTheAppOrigin(t *testing.T) {
	h, d := chromeApp(t, desktop.Config{Title: "Origin"})
	w2 := openPath(t, h, "/two")
	guard := "if (location.origin === " + strconv.Quote(h.URL("")) + ") { "

	if err := d.Emit("ping", map[string]any{"n": 1}); err != nil {
		t.Fatal(err)
	}
	if err := d.EmitTo(w2, "pong", nil); err != nil {
		t.Fatal(err)
	}
	h.CallFrom(w2, "windows", "broadcast", map[string]any{"name": "hello"}).AssertOK(t)
	h.CallFrom("main", "windows", "post", map[string]any{"to": w2, "name": "hi"}).AssertOK(t)

	for _, id := range []string{"main", w2} {
		evals := h.Window(id).Evals()
		if len(evals) == 0 {
			t.Fatalf("window %s received no evals", id)
		}
		for _, js := range evals {
			if !strings.HasPrefix(js, guard) {
				t.Errorf("window %s eval is not origin-gated:\n%s", id, js)
			}
		}
	}
}

// The behavioural test above covers the event paths; this one keeps
// every other call site honest. Every Window.Eval in the battery takes
// its script straight from inAppJS, which adds the origin gate.
//
//gofastr:test-reads battery/desktop/
func TestEveryPageEvalIsOriginGated(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	gated := 0
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		src, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		f, err := parser.ParseFile(fset, name, src, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				sel, ok := call.Fun.(*ast.SelectorExpr)
				if !ok || sel.Sel.Name != "Eval" || len(call.Args) != 1 {
					return true
				}
				if arg, ok := call.Args[0].(*ast.CallExpr); ok {
					if s, ok := arg.Fun.(*ast.SelectorExpr); ok && s.Sel.Name == "inAppJS" {
						gated++
						return true
					}
				}
				t.Errorf("%s: %s evaluates a script inAppJS did not wrap", fset.Position(call.Pos()), fn.Name.Name)
				return true
			})
		}
	}
	// Emit, emitToWindowID, broadcast, deliverDeepLink and dispatchMenu.
	if gated < 5 {
		t.Fatalf("found %d gated Eval calls, want at least 5: the scan is not reading the battery", gated)
	}
}
