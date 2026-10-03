package runtime_test

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core/middleware"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/uihost"
)

// The requirement-cycle panic fires where the behaviours block is
// first built — inside the host's manifestOnce on the first
// /__gofastr/manifest.js request (or first render that inlines it),
// NOT at process startup, because the registry is only complete once
// every package's init has run and BehaviorsJSON is the first reader
// of the whole graph. What that means operationally, pinned here
// through a real host: wrapped in the framework's recovery middleware
// (the wiring framework.App puts in front of every route), the panic
// surfaces as a 500 and the log line carries the cycle path, so the
// operator can name the culprit from the request alone.
type cycleScreen struct{}

func (cycleScreen) Render() render.HTML {
	return render.HTML(`<p id="mark">screen</p>`)
}

func TestCyclePanicSurfacesAs500ThroughARealHost(t *testing.T) {
	registry.IsolateForTest(t)
	// aa -> bb -> cc -> aa: the same cycle TestBehaviorsJSONRequirements
	// refuses at BehaviorsJSON time.
	registry.RegisterBehavior("aa", `(function(){})();`, registry.Markers("[data-aa]"), registry.Requires("bb"))
	registry.RegisterBehavior("bb", `(function(){})();`, registry.Markers("[data-bb]"), registry.Requires("cc"))
	registry.RegisterBehavior("cc", `(function(){})();`, registry.Markers("[data-cc]"), registry.Requires("aa"))

	a := app.NewApp("CycleHost")
	a.Register("/", &cycleScreen{}, nil)
	ds := uihost.New(a)

	var logbuf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logbuf, nil))
	h := middleware.RecoveryFn(func() *slog.Logger { return logger })(ds)

	req := httptest.NewRequest("GET", "/__gofastr/manifest.js", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500: the cycle panic must surface through the recovery chain, not kill the connection", w.Code)
	}
	if body := w.Body.String(); !strings.Contains(body, "Internal Server Error") {
		t.Fatalf("body = %q, want the recovery screen", body)
	}
	if !strings.Contains(logbuf.String(), "aa -> bb -> cc -> aa") {
		t.Fatalf("the recovery log line does not carry the cycle path; log: %s", logbuf.String())
	}
}
