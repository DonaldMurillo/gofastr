package ui

import (
	"strings"
	"testing"
)

func TestOptimisticRequiresFields(t *testing.T) {
	cases := []OptimisticActionConfig{
		{IdleLabel: "x", SuccessLabel: "y"},          // no endpoint
		{Endpoint: "/x", SuccessLabel: "y"},          // no idle
		{Endpoint: "/x", IdleLabel: "y"},             // no success
	}
	for i, c := range cases {
		func() {
			defer func() {
				if r := recover(); r == nil {
					t.Errorf("case %d: expected panic for incomplete cfg", i)
				}
			}()
			OptimisticAction(c)
		}()
	}
}

func TestOptimisticInitialMarkup(t *testing.T) {
	got := string(OptimisticAction(OptimisticActionConfig{
		Endpoint: "/follow", IdleLabel: "Follow", SuccessLabel: "Following",
	}))
	for _, want := range []string{
		`data-fui-comp="ui-optimistic-action"`,
		`data-state="idle"`,
		`data-fui-optimistic-endpoint="/follow"`,
		`data-fui-optimistic-method="POST"`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q, got: %s", want, got)
		}
	}
}

func TestSuccessHiddenByDefault(t *testing.T) {
	got := string(OptimisticAction(OptimisticActionConfig{
		Endpoint: "/x", IdleLabel: "Follow", SuccessLabel: "Following",
	}))
	if !strings.Contains(got, "Follow</span>") {
		t.Errorf("expected idle label, got: %s", got)
	}
	if !strings.Contains(got, "Following</span>") {
		t.Errorf("expected success label, got: %s", got)
	}
	// Success span must be hidden by default — runtime un-hides it on
	// commit. Attribute order isn't guaranteed; check for both pieces.
	if !strings.Contains(got, `data-fui-optimistic-success=""`) ||
		!strings.Contains(got, `hidden=""`) {
		t.Errorf("expected success span hidden by default, got: %s", got)
	}
}

func TestOptimisticCustomMethod(t *testing.T) {
	got := string(OptimisticAction(OptimisticActionConfig{
		Endpoint: "/x", Method: "DELETE", IdleLabel: "Remove", SuccessLabel: "Removed",
	}))
	if !strings.Contains(got, `data-fui-optimistic-method="DELETE"`) {
		t.Errorf("expected DELETE method, got: %s", got)
	}
}

func TestOptimisticVariantClass(t *testing.T) {
	got := string(OptimisticAction(OptimisticActionConfig{
		Endpoint: "/x", IdleLabel: "x", SuccessLabel: "y",
		Variant: ButtonSecondary,
	}))
	if !strings.Contains(got, "ui-button--secondary") {
		t.Errorf("expected secondary variant class, got: %s", got)
	}
}

// Regression: explicit Variant=ButtonPrimary was being silently
// dropped by the `Variant != "" && Variant != ButtonPrimary` guard,
// rendering the button without ui-button--primary even though the
// caller asked for it.
func TestOptimisticExplicitPrimary(t *testing.T) {
	got := string(OptimisticAction(OptimisticActionConfig{
		Endpoint: "/x", IdleLabel: "x", SuccessLabel: "y",
		Variant: ButtonPrimary,
	}))
	if !strings.Contains(got, "ui-button--primary") {
		t.Errorf("expected ui-button--primary on explicit primary variant, got: %s", got)
	}
}
