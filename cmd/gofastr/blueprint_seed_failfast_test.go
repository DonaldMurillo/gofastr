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
	if !strings.Contains(main, `return fmt.Errorf("seed %s: %w", s.Entity, err)`) {
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
