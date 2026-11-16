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

const semanticCoverageOptOut = "GOFASTR_NO_SEMANTIC_COVERAGE"

// RecordSemanticCoverage records which routes, entity operations,
// permissions, roles, hooks and events app exercises, and merges them
// into the manifest on disk when the test ends. TestHarness calls it;
// GOFASTR_NO_SEMANTIC_COVERAGE turns it off.
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
	t.Cleanup(func() {
		if err := semcov.Flush(); err != nil {
			t.Logf("semantic coverage: %v", err)
		}
	})
}

// FlushSemanticCoverage writes the manifest recorded so far.
func FlushSemanticCoverage() error { return semcov.Flush() }

const semanticCoverageEnv = "GOFASTR_SEMANTIC_COVERAGE"

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

func (a *App) entityForPattern(pattern string) string {
	if a.Registry == nil {
		return ""
	}
	trimmed := strings.Trim(pattern, "/")
	if trimmed == "" {
		return ""
	}
	segments := strings.Split(trimmed, "/")
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
