//go:build red

package main

import (
	"strings"
	"testing"
)

// RED TEST — open finding, 2026-09-06/07 adversarial round 5, phase 2
// (family enumeration; tier T3).
// Family: every outbound probe carries an operative per-attempt deadline —
// the repo already patched this exact shape for its own boot gate.
// Pinned sibling (green today): TestBootProbeClientTimesOut
// (cmd/gofastr/examples_blueprint_test.go:601) pins bootProbeClient
// (&http.Client{Timeout: 5s}, :533): "a raw http.Get has no timeout: an
// app that accepts the connection but never responds would block the probe
// forever". The blueprint e2e template is the same probe, one emitter
// away, and reaches every project generated from a blueprint.
// Property: the emitted e2e_test.go probes the booted app through a
// Timeout-bearing client — no bare http.Get/http.Post call and no
// http.DefaultClient remains in the emitted file.
// Surfaces: cmd/gofastr/blueprint.go e2e template — e2eWaitReady :4544
// (bare http.Get inside the readiness loop), axePagesFromSitemap :4251
// (bare http.Get), and every e2eDo call site :4440/:4445/:4452/:4535/:4555
// passing http.DefaultClient (zero timeout).
// Finding: a generated app whose server accepts the connection but never
// responds hangs the emitted e2e suite forever — e2eWaitReady's 100×100ms
// budget is as dead mid-attempt as the bench's 5s deadline, so `go test`
// sits until the CI job kills it, in every downstream project.
// Fix direction: emit the bootProbeClient shape — one &http.Client{Timeout: …}
// used by e2eWaitReady, the sitemap fetch, and every e2eDo call — exactly
// what examples_blueprint_test.go did for the repo's own gate.

func TestEmittedE2ERedProbesBounded(t *testing.T) {
	src := e2eTestGo(t, "sqlite")

	// Controls: the emitted suite still boots and probes the app — the
	// demand is bounded probes, not no probes.
	if !strings.Contains(src, "e2eWaitReady") || !strings.Contains(src, "e2eDo(") {
		t.Fatalf("setup broken: emitted e2e_test.go lost its boot/readiness probes")
	}

	if strings.Contains(src, "http.Get(") || strings.Contains(src, "http.Post(") || strings.Contains(src, "http.DefaultClient") {
		t.Errorf("SECURITY: [blueprint-e2e-deadline] emitted e2e_test.go still probes the booted app through unbounded transports (bare http.Get/http.Post or http.DefaultClient: blueprint.go :4544 e2eWaitReady, :4251 axePagesFromSitemap, :4440+ e2eDo call sites) — a generated app that accepts but never responds hangs the suite forever, the exact shape TestBootProbeClientTimesOut pinned for the repo's own boot gate (bootProbeClient, &http.Client{Timeout: 5s})")
	}
	if !strings.Contains(src, "Timeout:") {
		t.Errorf("SECURITY: [blueprint-e2e-deadline] emitted e2e_test.go contains no Timeout-bearing http.Client literal — every probe runs on zero-timeout transports, so the emitted readiness loop (blueprint.go:4544) can never fail a wedged app on its own; emit the bootProbeClient shape (cmd/gofastr/examples_blueprint_test.go:533)")
	}
}
