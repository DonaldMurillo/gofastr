package main

import (
	"strings"
	"testing"
)

// The emitted seed hook's CreateOne failure path must surface the
// per-field detail the CRUD validator already computed. A bare
// fmt.Errorf("seed %s: %w", ...) wraps ValidationError, whose Error() is
// the literal string "validation failed". The generated app dies with a
// message containing zero actionable information. The hook must
// errors.As the *crud.ValidationError, print each field's messages, and
// identify the offending row.
func TestSeedCreateErrorCarriesFieldDetail(t *testing.T) {
	main := renderBlueprintMain(seedDetailBlueprint())
	for _, want := range []string{
		"crud.ValidationError",
		"errors.As(",
		".Fields()",
		"seedCreateError(",
	} {
		if !strings.Contains(main, want) {
			t.Errorf("emitted main.go missing %q:\n%s", want, main)
		}
	}
	// The failure path must route through the detail helper, not wrap bare.
	// The row is identified by its one-based position in the blueprint's
	// seed: block, at i+1, matching how a human counts YAML list entries.
	if !strings.Contains(main, "return seedCreateError(s.Entity, i+1, err)") {
		t.Errorf("CreateOne error path does not route through seedCreateError:\n%s", main)
	}
	// Field messages must be ordered deterministically (sorted), so the
	// boot message is stable across runs and grep-able in tests.
	if !strings.Contains(main, "sort.Strings(") {
		t.Errorf("field detail is not sorted — nondeterministic boot messages:\n%s", main)
	}
	// The row identity must be its one-based index, not any row value.
	if !strings.Contains(main, "func seedCreateError(entity string, index int, err error) error") {
		t.Errorf("emitted helper does not take a row index:\n%s", main)
	}
}

// A failed seed aborts boot through log.Fatal, so anything in the message
// lands in application logs. Seed rows carry admin emails and seed
// passwords: the row's VALUES must never appear; only its position.
func TestSeedErrorOmitsRowValues(t *testing.T) {
	main := renderBlueprintMain(seedDetailBlueprint())
	if strings.Contains(main, "seedRowLabel") {
		t.Errorf("emitted main.go labels failing rows by value (seedRowLabel):\n%s", main)
	}
	if strings.Contains(main, `fmt.Sprintf("%v", row)`) {
		t.Errorf("emitted main.go formats the row map — values would reach the logs:\n%s", main)
	}
	if !strings.Contains(main, `"seed %s row %d failed validation:"`) {
		t.Errorf("emitted main.go does not label the failing row by index:\n%s", main)
	}
}

// The non-validation error path keeps the %w wrap: WithSeed errors abort
// App.Start and callers up the stack may errors.As the cause.
func TestSeedCreateErrorWrapsNonValidationCauses(t *testing.T) {
	main := renderBlueprintMain(seedDetailBlueprint())
	if !strings.Contains(main, `return fmt.Errorf("seed %s: %w", entity, err)`) {
		t.Errorf("non-validation seed errors must keep the wrapping form:\n%s", main)
	}
}

func seedDetailBlueprint() Blueprint {
	return Blueprint{
		App: BlueprintApp{Name: "SeedDetail", Module: "example.com/seeddetail"},
		Seed: []BlueprintSeedEntity{{
			Entity: "posts",
			Rows:   []map[string]any{{"title": "Hello"}},
		}},
	}
}
