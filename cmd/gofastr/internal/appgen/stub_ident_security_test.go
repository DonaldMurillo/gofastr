package appgen

import (
	"fmt"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/appspec"
	"github.com/DonaldMurillo/gofastr/framework"
)

// Pins: every app spec string emitted into Go identifier position is gated (skipped) at the emitter.
// (tier T3).
//
// CONTRACT-QUESTION: is assertAppSpecGoParses an acceptable substitute for
// an emitter-side identifier gate on a path that skips validateAppSpec
// (loadAppSpecPath(path, false), tests, --add fragments)? The file's own
// convention answers no, twice: the endpoint stub loop refuses to emit a
// non-identifier because "assertAppSpecGoParses is the backstop, not the
// guard" (appspec.go:7327-7336), and the resource-only mount applies the
// "same rule as the hook stubs" for exactly this reachability reason
// (:5600-5608). The middleware/plugins/helpers loops and the screen
// type/mount declarations sit between those two gates with none of their
// own — divergence, not decision.
//
// Property: every app spec string emitted into Go identifier position is
// gated (skipped) at the emitter whenever render is reachable without
// validateAppSpec — toCamelCase transforms, it does not validate.
// Surfaces: appspec.go::renderAppSpecStubs — middleware `func
// %sMiddleware` :7345-7350, plugins `type %s struct{}` :7351-7356, helpers
// `func %s()` :7357-7360, all via toCamelCase(item.Name) straight into
// identifier position with no isGoIdentifier gate; ::renderAppSpecApp
// :8027-8032 registers the same ungated names (`fwApp.Use(%sMiddleware)`);
// screen typeName/decls :5228/:5248-5257 and mount declarations
// :5559-5561/:5589-5596 are ungated while the resource-only mount in the
// SAME function (:5598-5608) is guarded.
// Finding: Middleware{Name:"2bad"} — a digit-leading name toCamelCase passes
// through untouched — emits `func 2badMiddleware(next http.Handler)` into
// stubs.go, so renderAppSpecFiles fails WHOLESALE at assertAppSpecGoParses
// (:3637 → :3668) with "generated stubs.go does not parse … Please report
// this with the app spec that produced it": the documented per-field refusal
// (validateAppSpec rejects the name; the endpoint arm skips it) becomes a
// whole-generate failure with an unactionable message. The same-shaped
// endpoint Handler is skipped and renders fine.
// Fix direction: gate the middleware/plugins/helper stub loops (and the
// app.go registrations, and the screen type/mount declarations) on
// isGoIdentifier exactly as the endpoint, hook, and resource-only-mount arms
// already do: skip, never emit.

func TestStubIdentSkipsBadMiddleware(t *testing.T) {
	// Deterministic: no network in the render path (font fetch is stubbed
	// out the way the audit tests stub it).
	prev := FontFetcher
	FontFetcher = func(string) ([]byte, error) { return nil, fmt.Errorf("offline") }
	t.Cleanup(func() { FontFetcher = prev })

	base := func() appspec.AppSpec {
		return appspec.AppSpec{
			App: appspec.AppSpecApp{Name: "app", Module: "m"},
			Entities: []framework.EntityDeclaration{{
				Name:   "tickets",
				Fields: []framework.FieldDeclaration{{Name: "title", Type: "string"}},
			}},
			Screens: []appspec.AppSpecScreen{{Name: "home", Route: "/", Type: "page"}},
		}
	}

	// Leg 1 (finding): a middleware name that survives toCamelCase as a
	// non-identifier must be skipped at the emitter, mirroring the endpoint
	// stub: render succeeds, the identifier never appears.
	bp := base()
	bp.Middleware = []appspec.AppSpecNamedStub{{Name: "2bad"}}
	files, err := RenderAppSpecFiles(bp)
	if err != nil {
		t.Errorf("SECURITY: [stub-ident-guard-asymmetry] Middleware{Name:%q} fails the WHOLE generate: %v — the hook stub loop in the same function is isGoIdentifier-gated and skips (assertAppSpecGoParses is the backstop, not the guard), but the middleware loop emits `func 2badMiddleware(...)` into identifier position. Skip like the hook arm, never emit.", "2bad", err)
	} else {
		for _, f := range files {
			if strings.Contains(f.Content, "2badMiddleware") {
				t.Errorf("SECURITY: [stub-ident-guard-asymmetry] %s carries the ungated identifier `2badMiddleware` — the hook convention skips the same shape at this exact emitter", f.Name)
			}
		}
	}
}
