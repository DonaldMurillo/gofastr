package framework

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/contracts"
	_ "github.com/DonaldMurillo/gofastr/framework/contracts/analyzers"
	"github.com/DonaldMurillo/gofastr/framework/semcov"
)

// The producer (RecordSemanticCoverage → semcov → the manifest file) and
// the consumer (the testing analyzers) are tested separately everywhere
// else, and the analyzer suite reads a hand-written manifest string. That
// leaves the seam between them untested: if the written shape drifted
// from the parsed shape, both suites would still pass while the feature
// reported every route untested.
//
// This drives the whole loop, exercise one of two routes, flush the
// manifest the real code writes, and let the real analyzer read it.
func TestSemanticCoverageLoopFromRecordingToRule(t *testing.T) {
	dir := t.TempDir()
	const source = `package main

import (
	"net/http"

	"github.com/DonaldMurillo/gofastr/framework"
)

func build() *framework.App {
	app := framework.NewApp()
	app.Router().Get("/covered", http.HandlerFunc(nil))
	app.Router().Get("/never-tested", http.HandlerFunc(nil))
	return app
}
`
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/app\n\ngo 1.26\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}

	// Record against a real app with the same two routes, exercising one.
	app := NewApp()
	app.Router().Get("/covered", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	app.Router().Get("/never-tested", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))

	semcov.Reset()
	t.Cleanup(semcov.Reset)
	semcov.Enable(dir)
	app.router.SetServeHook(func(method, pattern string) { semcov.RecordRoute(method, pattern) })
	t.Cleanup(func() { app.router.SetServeHook(nil) })

	rec := httptest.NewRecorder()
	app.Router().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/covered", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("fixture request returned %d", rec.Code)
	}
	if err := semcov.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}

	manifest := filepath.Join(dir, ".gofastr", "semantic-coverage.json")
	if _, err := os.Stat(manifest); err != nil {
		t.Fatalf("the manifest the analyzers read was not written: %v", err)
	}

	pass, err := contracts.NewPass(dir, contracts.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	report, err := contracts.Run(pass, contracts.RunOptions{Analyzers: []string{"testing"}})
	if err != nil {
		t.Fatal(err)
	}

	var untested []string
	sawNoManifest := false
	for _, d := range report.Diagnostics {
		switch d.RuleID {
		case contracts.RuleRouteNotExercised:
			untested = append(untested, d.Message)
		case contracts.RuleNoCoverageManifest:
			sawNoManifest = true
		}
	}
	if sawNoManifest {
		t.Error("a manifest was written but the analyzer reported none — producer and consumer disagree on the path")
	}
	if len(untested) != 1 {
		t.Fatalf("want exactly one untested route, got %d: %v", len(untested), untested)
	}
	if !strings.Contains(untested[0], "/never-tested") {
		t.Errorf("the wrong route was reported untested: %s", untested[0])
	}
	if strings.Contains(untested[0], "/covered") {
		t.Errorf("the exercised route was reported untested: %s", untested[0])
	}
}
