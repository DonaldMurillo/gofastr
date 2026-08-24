package framework

import (
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/access"
	"github.com/DonaldMurillo/gofastr/framework/event"
	"github.com/DonaldMurillo/gofastr/framework/hook"
	"github.com/DonaldMurillo/gofastr/framework/semcov"
)

// semanticCoverageOptOut disables recording for a suite that does not
// want the manifest written, a benchmark run, or a package whose routes
// are fixtures rather than the app's real surface.
const semanticCoverageOptOut = "GOFASTR_NO_SEMANTIC_COVERAGE"

// RecordSemanticCoverage turns on semantic-coverage recording for an app
// and arranges for the manifest to be written when the test binary
// finishes.
//
// [TestHarness] calls this automatically, so a suite already using the
// harness gets route and entity coverage with no change at all. Call it
// directly only when driving an app some other way, an httptest.Server,
// a chromedp suite against a real listener:
//
//	func TestMain(m *testing.M) { … }
//	func TestFlow(t *testing.T) {
//	    framework.RecordSemanticCoverage(t, app)
//	    …
//	}
//
// Recording is per-process and additive. `go test ./...` runs one process
// per package, and each flush merges with what is already on disk, so the
// manifest ends up describing the whole suite rather than whichever
// package happened to finish last.
//
// Nothing here runs outside tests: production binaries never call it, and
// the hooks it installs are nil until they do.
func RecordSemanticCoverage(t testing.TB, app *App) {
	t.Helper()
	if app == nil || app.router == nil || os.Getenv(semanticCoverageOptOut) != "" {
		return
	}
	semcov.Enable("")
	app.router.SetServeHook(func(method, pattern string) {
		semcov.RecordRoute(method, pattern)
		if name := app.entityForPattern(pattern); name != "" {
			semcov.RecordEntityOp(name, entityOpFor(method, pattern))
		}
	})
	// Permissions are recorded whatever the verdict. A test that asserts
	// a *denial* proves the boundary just as well as one that asserts a
	// grant, and the failure this rule exists to catch is a check that
	// is never reached at all.
	access.SetObserver(func(e access.Evaluation) {
		semcov.RecordPermission(string(e.Permission))
		for _, role := range e.Roles {
			semcov.RecordRole(role)
		}
	})
	hook.SetObserver(func(f hook.Firing) {
		semcov.RecordHook(f.Entity, f.Type.String())
	})
	event.SetObserver(func(e event.Emission) {
		semcov.RecordEvent(e.Type)
	})
	// Written at the end of each enabling test rather than once at exit,
	// because testing.TB has no process-exit hook. Flush is a no-op when
	// nothing new was recorded, so the common case costs nothing, and
	// writing incrementally means a suite that panics halfway still
	// leaves a usable manifest behind.
	t.Cleanup(func() {
		if err := semcov.Flush(); err != nil {
			t.Logf("semantic coverage: %v", err)
		}
	})
}

// FlushSemanticCoverage writes the accumulated manifest. Call it from
// TestMain after m.Run() when a suite wants the manifest written exactly
// once at the end rather than incrementally.
func FlushSemanticCoverage() error { return semcov.Flush() }

// semanticCoverageEnv turns recording on for a *serving* process.
const semanticCoverageEnv = "GOFASTR_SEMANTIC_COVERAGE"

// enableSemanticCoverageFromEnv wires the recorder into a running server
// when GOFASTR_SEMANTIC_COVERAGE=1.
//
// This exists because the most common integration-test shape does not go
// through [TestHarness] at all: `gofastr generate` emits an e2e test that
// builds the binary and runs it as a subprocess, driving it over real
// HTTP. Every route that test exercises was invisible to coverage, the
// requests happened in a process that had never called Enable, so a
// suite with thorough e2e coverage still reported every route as
// unreached. Setting the variable on the subprocess fixes that with no
// change to how the test drives it.
//
// The manifest is flushed from the serve hook rather than at shutdown,
// because a test harness typically SIGKILLs the server and no shutdown
// path runs. Flush is a no-op unless something new was recorded, so the
// cost is one write per newly-seen route, not one per request.
func (a *App) enableSemanticCoverageFromEnv() {
	if a.router == nil || os.Getenv(semanticCoverageEnv) != "1" {
		return
	}
	semcov.Enable("")
	a.router.SetServeHook(func(method, pattern string) {
		semcov.RecordRoute(method, pattern)
		if name := a.entityForPattern(pattern); name != "" {
			semcov.RecordEntityOp(name, entityOpFor(method, pattern))
		}
		// Written now, not at shutdown: the process is usually killed.
		if err := semcov.Flush(); err != nil {
			a.Logger().Warn("semantic coverage: flush failed", "error", err)
		}
	})
	access.SetObserver(func(e access.Evaluation) {
		semcov.RecordPermission(string(e.Permission))
		for _, role := range e.Roles {
			semcov.RecordRole(role)
		}
	})
	hook.SetObserver(func(f hook.Firing) {
		semcov.RecordHook(f.Entity, f.Type.String())
	})
	event.SetObserver(func(e event.Emission) {
		semcov.RecordEvent(e.Type)
	})
	a.Logger().Info("semantic coverage recording enabled",
		"manifest", semcov.FileName, "dir", semcov.DefaultDir())
}

// entityForPattern maps a served route pattern back to the entity that
// mounted it, so a request to /api/posts/{id} records against "posts".
// Returns "" for a route no entity owns.
func (a *App) entityForPattern(pattern string) string {
	if a.Registry == nil {
		return ""
	}
	trimmed := strings.Trim(pattern, "/")
	if trimmed == "" {
		return ""
	}
	segments := strings.Split(trimmed, "/")
	// Walk from the end so a prefixed mount ("/api/v1/posts/{id}") finds
	// "posts" rather than "api".
	for _, seg := range slices.Backward(segments) {

		if seg == "" || strings.HasPrefix(seg, "{") {
			continue
		}
		for name, ent := range a.Registry.All() {
			if ent != nil && ent.GetTable() == seg {
				return ent.GetName()
			}
			if strings.EqualFold(name, seg) {
				return name
			}
		}
	}
	return ""
}

// entityOpFor names the CRUD operation a (method, pattern) pair performs,
// using the shape auto-CRUD mounts: a trailing path parameter means the
// route addresses one row.
func entityOpFor(method, pattern string) string {
	item := strings.HasSuffix(strings.TrimSuffix(pattern, "/"), "}")
	switch strings.ToUpper(method) {
	case "POST":
		return "create"
	case "PUT", "PATCH":
		return "update"
	case "DELETE":
		return "delete"
	case "GET", "HEAD":
		if item {
			return "get"
		}
		return "list"
	default:
		return strings.ToLower(method)
	}
}
