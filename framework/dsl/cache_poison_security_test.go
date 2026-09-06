package dsl

import "testing"

// F28 time-of-use of configuration (shared cross-request state) — pinned 2026-09-05 round 4.
// Fix (this suite's green state): ParseDSL returns a caller-isolated shallow copy with
// fresh Filters/Includes/Orders slices on every hit (cloneDSLQuery), the HooksFor posture.
// Property: a package-level cache must hand every caller an isolated result — mutating a
// returned DSLQuery must never change what a subsequent ParseDSL of the same input yields.
// Surfaces: dsl.go:ParseDSL (returns the cached DSLQuery struct by value, but its Filters /
//
//	Includes / Orders slices are the cache's backing arrays), reached from
//	dsl.go:BuildDSLQuery and every host/agent caller of the exported ParseDSL.
//
// Finding: verified below — after one caller sets first.Filters[0].Value = "PWNED", the
//
//	next ParseDSL of the same query string returns "PWNED". The cache entry is shared
//	mutable state across requests; any caller that adjusts a parsed query in place
//	(a natural thing to do with a value-typed result) silently rewrites the answer every
//	later caller of that query receives. The repo already enforces this property for the
//	same shape elsewhere: hook.HooksFor returns a defensive copy (pinned by
//	TestHooksForReturnsDefensiveCopy) and datexport.All copies its slices.
//
// Severity: low — no in-tree caller mutates the result today (BuildDSLQuery is read-only),
// so the poison needs a host caller that adjusts a parsed query; when it happens it is
// cross-request data contamination with no error anywhere.
func TestParseDSLReturnsCallerIsolated(t *testing.T) {
	const q = `posts.where(status="draft").limit(5)`

	first, err := ParseDSL(q)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(first.Filters) != 1 {
		t.Fatalf("want 1 filter, got %d", len(first.Filters))
	}

	// A caller legitimately adjusts its local, value-typed result.
	first.Filters[0].Value = "PWNED"

	second, err := ParseDSL(q)
	if err != nil {
		t.Fatalf("re-parse: %v", err)
	}
	if len(second.Filters) != 1 {
		t.Fatalf("re-parse: want 1 filter, got %d", len(second.Filters))
	}
	if second.Filters[0].Value != "draft" {
		t.Errorf("SECURITY: [dsl-cache]: cached ParseDSL result is shared mutable state: after one caller set Filters[0].Value=%q, every later caller of the same query gets %q (want \"draft\")", first.Filters[0].Value, second.Filters[0].Value)
	}
}
