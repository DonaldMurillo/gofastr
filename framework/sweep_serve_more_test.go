package framework

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/semcov"
)

// A GET answered as JSON never joins the open-stream set, so the drain
// leaves it to finish.
func TestSweepServeJSONGetStaysOutOfTheStreamSet(t *testing.T) {
	set := &openStreams{}
	cancelled := false
	l := &longLived{cancel: func() { cancelled = true }, set: set}
	w := &streamWriter{ResponseWriter: httptest.NewRecorder(), life: l}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	set.endAll(t.Context())
	if cancelled {
		t.Fatal("the drain ended a JSON GET")
	}
}

// A serving process under GOFASTR_SEMANTIC_COVERAGE=1 has the manifest on
// disk after a request, with no Flush call and no shutdown.
func TestSweepServeServingCoverageFlushesPerRoute(t *testing.T) {
	dir := t.TempDir()
	semcov.Reset()
	t.Cleanup(semcov.Reset)
	t.Setenv("GOFASTR_SEMANTIC_COVERAGE_DIR", dir)
	t.Setenv(semanticCoverageEnv, "1")
	app := NewApp(WithConfig(AppConfig{Name: "serving-flush"}))
	app.Router().Get("/parts/{id}", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	app.enableSemanticCoverageFromEnv()
	rec := httptest.NewRecorder()
	app.Router().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/parts/1", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	m, err := semcov.Read(dir)
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	if !m.CoveredRoute("GET", "/parts/{id}") {
		t.Fatalf("manifest lacks the served route: %v", m.Routes)
	}
}
