package runtime

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
	"github.com/chromedp/chromedp"
)

// The lightbox module lives in framework/ui now, a registered behaviour
// this package cannot import (core-ui/runtime sits below it). These
// helpers read the REAL module and the REAL descriptor out of the
// tree — the module source by path, the registration by parsing the
// Go that declares it — so the regressions below run against what
// framework/ui actually ships, not a copy that would keep passing when
// the real descriptor drifted.

// lightboxModuleSource returns the module's source as shipped beside
// its Go.
func lightboxModuleSource(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "framework", "ui", "lightbox.js"))
	if err != nil {
		t.Fatalf("read framework/ui/lightbox.js (the lightbox module moved there as a registered behaviour): %v", err)
	}
	return string(raw)
}

// requiresInSource lists the string literals a file's registry.Requires
// call names, and every argument that is not one.
func requiresInSource(src string) (names, nonLiteral []string) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "", src, 0)
	if err != nil {
		return nil, []string{"unparseable source: " + err.Error()}
	}
	local := ""
	for _, imp := range f.Imports {
		if path, err := strconv.Unquote(imp.Path.Value); err == nil && path == registryImportPath {
			local = "registry"
			if imp.Name != nil {
				local = imp.Name.Name
			}
		}
	}
	if local == "" || local == "_" {
		return nil, nil
	}
	ast.Inspect(f, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		match := false
		switch fn := call.Fun.(type) {
		case *ast.SelectorExpr:
			if id, ok := fn.X.(*ast.Ident); ok && id.Name == local && fn.Sel.Name == "Requires" {
				match = true
			}
		case *ast.Ident:
			if local == "." && fn.Name == "Requires" {
				match = true
			}
		}
		if !match {
			return true
		}
		for _, a := range call.Args {
			if lit, ok := a.(*ast.BasicLit); ok && lit.Kind == token.STRING {
				if s, err := strconv.Unquote(lit.Value); err == nil {
					names = append(names, s)
					continue
				}
			}
			nonLiteral = append(nonLiteral, "requirement is not a string literal")
		}
		return true
	})
	return names, nonLiteral
}

// registerFrameworkLightbox isolates the registry and registers the
// lightbox behaviour exactly as framework/ui/lightbox.go declares it:
// source, markers, requirements and interactions all read from the
// tree. A drift in the real registration changes what these tests run
// against — the descriptor is never re-typed here.
func registerFrameworkLightbox(t *testing.T) {
	t.Helper()
	goSrc, err := os.ReadFile(filepath.Join("..", "..", "framework", "ui", "lightbox.go"))
	if err != nil {
		t.Fatalf("read framework/ui/lightbox.go: %v", err)
	}
	markers, non := markerSelectorsInSource(string(goSrc))
	if len(markers) == 0 || len(non) != 0 {
		t.Fatalf("could not read the lightbox registration's markers from framework/ui/lightbox.go: %v / unreadable %v", markers, non)
	}
	interactions, non := interactionsInSource(string(goSrc))
	if len(interactions) == 0 || len(non) != 0 {
		t.Fatalf("could not read the lightbox registration's interactions from framework/ui/lightbox.go: unreadable %v", non)
	}
	requires, non := requiresInSource(string(goSrc))
	if len(non) != 0 {
		t.Fatalf("could not read the lightbox registration's requirements: %v", non)
	}
	opts := []registry.BehaviorOption{registry.Markers(markers...)}
	if len(requires) > 0 {
		opts = append(opts, registry.Requires(requires...))
	}
	if len(interactions) > 0 {
		opts = append(opts, registry.Interactions(interactions...))
	}
	registry.IsolateForTest(t)
	registry.RegisterBehavior("lightbox", lightboxModuleSource(t), opts...)
}

// TestLightboxClickBeforeModuleLoadIsReplayed covers the cold-cache gap in
// issue #161. The marker scanner starts the lightbox module, but the
// module's own document click listener does not exist until that request
// completes. A click on the navigation control during that window must be
// prevented now, then replayed after the module arrives.
//
// Re-headed for the registered-behaviour world: the page carries the
// inline #gofastr-behaviors block built from the real registration (the
// kernel's own table no longer knows the lightbox), and the module is
// served from framework/ui/lightbox.js.
func TestLightboxClickBeforeModuleLoadIsReplayed(t *testing.T) {
	registerFrameworkLightbox(t)
	core, err := RuntimeJS()
	if err != nil {
		t.Fatal(err)
	}
	lightbox, ok := Module("lightbox")
	if !ok {
		t.Fatal("lightbox module not served after registration")
	}
	block := inlineBehaviorsBlock(t)

	moduleRequested := make(chan struct{})
	requestOnce := sync.Once{}
	moduleGate := make(chan struct{})
	var releaseOnce sync.Once
	releaseModule := func() { releaseOnce.Do(func() { close(moduleGate) }) }
	defer releaseModule()

	mux := http.NewServeMux()
	mux.HandleFunc("/__gofastr/runtime.js", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		_, _ = w.Write([]byte(core))
	})
	mux.HandleFunc("/__gofastr/runtime/lightbox.js", func(w http.ResponseWriter, _ *http.Request) {
		requestOnce.Do(func() { close(moduleRequested) })
		<-moduleGate
		w.Header().Set("Content-Type", "application/javascript")
		_, _ = w.Write([]byte(lightbox))
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<!doctype html><html><head>`+block+`</head><body>
<div id="viewer" data-fui-widget="viewer"></div>
<a data-fui-lightbox-group="photos" data-fui-deeplink="src=one.jpg&group=photos">one</a>
<a data-fui-lightbox-group="photos" data-fui-deeplink="src=two.jpg&group=photos">two</a>
<script src="/__gofastr/runtime.js"></script>
<script>
window.__gofastr.loadedModules.widgets = true;
window.__gofastr._signals = {
  group: { value: 'photos' },
  src: { value: 'one.jpg' }
};
  window.__lbCall = '';
  window.__lbCallCount = 0;
  window.__gofastr.openWidget = function (name, opts) {
    window.__lbCallCount++;
    window.__lbCall = name + ':' + ((((opts || {}).params || {}).src) || '');
  };
  window.__nextDefaultPrevented = false;
document.addEventListener('click', function (e) {
  if (e.target && e.target.closest && e.target.closest('#next')) {
    window.__nextDefaultPrevented = e.defaultPrevented;
  }
});
document.getElementById('viewer').innerHTML =
  '<div data-fui-comp="ui-lightbox" data-fui-lightbox="viewer" data-fui-lightbox-nav="true">' +
  '<button id="next" type="button" data-fui-lightbox-next>Next</button></div>';
</script>
</body></html>`)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	ctx := chromedptest.Context(t, chromedptest.Timeout(90*time.Second))
	if err := chromedp.Run(ctx,
		// Use location assignment so this setup action does not wait for the
		// deliberately held dynamic module response to finish the load event.
		chromedp.Evaluate(fmt.Sprintf("location.href = %q", srv.URL+"/"), nil),
		chromedp.WaitVisible(`#next`, chromedp.ByID),
	); err != nil {
		t.Fatalf("chromedp setup: %v", err)
	}

	select {
	case <-moduleRequested:
	case <-time.After(5 * time.Second):
		t.Fatal("lightbox module request did not start")
	}

	var prevented bool
	if err := chromedp.Run(ctx,
		chromedp.Click(`#next`, chromedp.ByID),
		chromedp.Evaluate(`window.__nextDefaultPrevented`, &prevented),
	); err != nil {
		t.Fatalf("chromedp cold-cache click: %v", err)
	}
	if !prevented {
		t.Fatal("lightbox navigation click was not prevented synchronously while the module was loading")
	}

	releaseModule()
	if err := chromedp.Run(ctx,
		chromedp.Poll(`!!(window.__gofastr&&window.__gofastr.loadedModules&&window.__gofastr.loadedModules.lightbox)`, nil,
			chromedp.WithPollingTimeout(5*time.Second), chromedp.WithPollingInterval(25*time.Millisecond)),
		chromedp.Poll(`window.__lbCall === 'viewer:two.jpg'`, nil,
			chromedp.WithPollingTimeout(5*time.Second), chromedp.WithPollingInterval(25*time.Millisecond)),
		chromedp.Evaluate(`document.getElementById('next').click()`, nil),
		chromedp.Poll(`window.__lbCallCount === 2`, nil,
			chromedp.WithPollingTimeout(5*time.Second), chromedp.WithPollingInterval(25*time.Millisecond)),
	); err != nil {
		t.Fatalf("chromedp replay: %v", err)
	}
}
