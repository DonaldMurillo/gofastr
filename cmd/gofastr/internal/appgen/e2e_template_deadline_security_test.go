package appgen

import (
	"strings"
	"testing"
)

// Pins: the emitted e2e_test.go probes the booted app through Timeout-bearing transports only.
// (family enumeration; tier T3).
// Family: every outbound probe carries an operative per-attempt deadline —
// the repo already patched this exact shape for its own boot gate.
// Pinned sibling (green today): TestBootProbeClientTimesOut
// (cmd/gofastr/examples_appspec_test.go:684) pins bootProbeClient
// (&http.Client{Timeout: 5s}, :522): "a raw http.Get has no timeout: an
// app that accepts the connection but never responds would block the probe
// forever". The app spec e2e template is the same probe, one emitter
// away, and reaches every project generated from an app spec.
// Property: the emitted e2e_test.go probes the booted app through a
// Timeout-bearing client — no bare http.Get/http.Post call and no
// http.DefaultClient remains in the emitted file.
// Surfaces: cmd/gofastr/internal/appgen/appspec.go e2e template — e2eWaitReady :1221
// (bare http.Get inside the readiness loop), axePagesFromSitemap :908
// (bare http.Get), and every e2eDo call site
// passing http.DefaultClient (zero timeout).
// Finding: a generated app whose server accepts the connection but never
// responds hangs the emitted e2e suite forever — e2eWaitReady's 100×100ms
// budget is as dead mid-attempt as the bench's 5s deadline, so `go test`
// sits until the CI job kills it, in every downstream project.
// Fix direction: emit the bootProbeClient shape — one &http.Client{Timeout: …}
// used by e2eWaitReady, the sitemap fetch, and every e2eDo call — exactly
// what examples_appspec_test.go did for the repo's own gate.

func TestEmittedE2ERedProbesBounded(t *testing.T) {
	src := e2eTestGo(t, "sqlite")

	// Controls: the emitted suite still boots and probes the app — the
	// demand is bounded probes, not no probes.
	if !strings.Contains(src, "e2eWaitReady") || !strings.Contains(src, "e2eDo(") {
		t.Fatalf("setup broken: emitted e2e_test.go lost its boot/readiness probes")
	}

	if strings.Contains(src, "http.Get(") || strings.Contains(src, "http.Post(") || strings.Contains(src, "http.DefaultClient") {
		t.Errorf("SECURITY: [app-spec-e2e-deadline] emitted e2e_test.go still probes the booted app through unbounded transports (bare http.Get/http.Post or http.DefaultClient: appspec.go :1221 e2eWaitReady, :908 axePagesFromSitemap, :1224 e2eDo and its call sites) — a generated app that accepts but never responds hangs the suite forever, the exact shape TestBootProbeClientTimesOut pinned for the repo's own boot gate (bootProbeClient, &http.Client{Timeout: 5s})")
	}
	if !strings.Contains(src, "Timeout:") {
		t.Errorf("SECURITY: [app-spec-e2e-deadline] emitted e2e_test.go contains no Timeout-bearing http.Client literal — every probe runs on zero-timeout transports, so the emitted readiness loop (appspec.go:1221) can never fail a wedged app on its own; emit the bootProbeClient shape (cmd/gofastr/examples_appspec_test.go:522)")
	}
}
