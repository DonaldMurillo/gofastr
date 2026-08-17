package main

import (
	"strings"
	"testing"
)

// A seed row that fails CreateOne must abort startup, not be logged and
// skipped: the CountAll idempotency gate marks the entity non-empty after a
// partial insert, so a dropped row never retries — the app then reports
// ready on every boot with bootstrap data silently missing. WithSeed errors
// abort App.Start, so returning the error is the whole fix.
func TestSeedHookReturnsRowErrors(t *testing.T) {
	main := renderBlueprintMain(websitesBlueprint())
	if strings.Contains(main, "skipping row") {
		t.Fatalf("seed hook logs-and-skips failed rows — fail-open boot:\n%s", main)
	}
	if !strings.Contains(main, "return seedCreateError(s.Entity, i+1, err)") {
		t.Fatalf("seed hook does not return CreateOne errors:\n%s", main)
	}
}

// A missing CRUD handler (entity renamed, registration drifted) must abort
// for the same reason — `continue` silently unseeds the whole entity.
func TestSeedHookReturnsHandlerErrors(t *testing.T) {
	main := renderBlueprintMain(websitesBlueprint())
	if !strings.Contains(main, `return fmt.Errorf("seed %s: no handler: %w", s.Entity, err)`) {
		t.Fatalf("seed hook swallows a missing CRUD handler instead of failing the boot:\n%s", main)
	}
}

// A CountAll ERROR must abort startup, not fall through into inserts: the
// idempotency gate's premise is "skip if non-empty", and an unreadable
// table is uncertainty about exactly that. Guessing "empty" on a
// transient read failure re-seeds — duplicates for non-unique rows — the
// fail-open the fail-fast comment above promises to prevent. Sibling of
// the CreateOne / missing-handler fixes (v0.62) on the same seed path.
func TestSeedHookReturnsCountAllErrors(t *testing.T) {
	main := renderBlueprintMain(websitesBlueprint())
	if strings.Contains(main, "err == nil && n > 0") {
		t.Fatalf("seed gate uses fail-open `err == nil && n > 0` — a CountAll error falls through to inserts:\n%s", main)
	}
	if !strings.Contains(main, `return fmt.Errorf("seed %s: count: %w", s.Entity, err)`) {
		t.Fatalf("seed hook must return the CountAll error instead of guessing empty:\n%s", main)
	}
}
