package main

import (
	"strings"
	"testing"
)

// Property family: a blueprint flag whose FALSE value is the permissive state
// must never be reached by silent coercion. If the decoder cannot tell what the
// author meant, it fails the generate — it does not guess "off".
//
// This is the same shape as the screen-layout finding in
// emitter_output_context_security_test.go (an unvalidated config value silently
// taking a default), with the difference that these defaults are the
// *permissive* ones, so guessing is fail-OPEN rather than merely wrong.
//
// boolValue is the lax decoder: anything that is not the bool true or the
// literal string "true" becomes false. core/yaml is YAML 1.2, so it does NOT
// type `yes` / `on` / `y` / `1` as booleans — they arrive as strings and coerce
// to false. Those spellings are the most common way a human (or an agent
// transcribing a spec, which is a documented GoFastr workflow) writes "true" in
// YAML.
//
// strictBoolValue already existed for exactly this, but was wired to ONE key
// (app.auth.dev_mode). Its doc comment framed the rule as "keys whose default is
// true", which is why every key below was missed: the polarity that matters is
// not what the default is, it is whether the coercion lands on the UNSAFE side.
// `access.auth: yes` defaults to false, and false is a PUBLIC screen.

// The surfaces, and what a silently-false value costs at each.
var protectiveFlagSurfaces = []struct {
	name string
	yaml func(value string) string
	// cost is what the author loses when the flag silently reads false.
	cost string
}{
	{"screens[].access.auth", func(v string) string {
		return "app:\n  name: app\n  module: local/app\nscreens:\n  - name: secret\n    route: /secret\n    type: page\n    access:\n      auth: " + v + "\n"
	}, "the screen is registered with no policy — publicly reachable"},
	{"entities[].scope.multi_tenant", func(v string) string {
		return "app:\n  name: app\n  module: local/app\nentities:\n  - name: tickets\n    fields:\n      - name: title\n        type: string\n    scope:\n      multi_tenant: " + v + "\n"
	}, "tenant scoping is off — every tenant reads every row"},
	{"entities[].scope.soft_delete", func(v string) string {
		return "app:\n  name: app\n  module: local/app\nentities:\n  - name: tickets\n    fields:\n      - name: title\n        type: string\n    scope:\n      soft_delete: " + v + "\n"
	}, "deletes are permanent — no forensic trail"},
	{"entities[].fields[].hidden", func(v string) string {
		return "app:\n  name: app\n  module: local/app\nentities:\n  - name: tickets\n    fields:\n      - name: secret_note\n        type: string\n        hidden: " + v + "\n"
	}, "the field is serialized to every API response"},
	{"entities[].fields[].no_query", func(v string) string {
		return "app:\n  name: app\n  module: local/app\nentities:\n  - name: tickets\n    fields:\n      - name: secret_note\n        type: string\n        no_query: " + v + "\n"
	}, "the field is filterable — a value oracle"},
	{"entities[].fields[].read_only", func(v string) string {
		return "app:\n  name: app\n  module: local/app\nentities:\n  - name: tickets\n    fields:\n      - name: role\n        type: string\n        read_only: " + v + "\n"
	}, "clients may write the field"},
}

// One case shape per YAML-1.1 truthy spelling that YAML 1.2 leaves a string.
var yamlTruthyNotBool = []string{"yes", "on", "y", "1", "Yes", "TRUE"}

func TestProtectiveBoolsRejectYamlTruthy(t *testing.T) {
	for _, s := range protectiveFlagSurfaces {
		for _, v := range yamlTruthyNotBool {
			t.Run(s.name+"/"+v, func(t *testing.T) {
				bp, err := decodeBlueprintString(s.yaml(v))
				if err != nil {
					return // rejected at the boundary — the point
				}
				// Accepted. That is only OK if it was read as TRUE.
				if !protectiveFlagIsSet(t, bp, s.name) {
					t.Fatalf("SECURITY: [fail-open] %s: %q was accepted and read as FALSE — %s",
						s.name, v, s.cost)
				}
			})
		}
	}
}

// Control: the spellings YAML 1.2 really does type as booleans still work, and
// an explicit false is still an explicit false. Without this the test above
// passes on a decoder that rejects every value.
func TestProtectiveBoolsAcceptRealBools(t *testing.T) {
	for _, s := range protectiveFlagSurfaces {
		for _, v := range []string{"true", "True", "TRUE"} {
			bp, err := decodeBlueprintString(s.yaml(v))
			if err != nil {
				continue // TRUE is accepted by strictBoolValue; True/TRUE may not be
			}
			if !protectiveFlagIsSet(t, bp, s.name) {
				t.Errorf("%s: %q decoded as false", s.name, v)
			}
		}
		bp, err := decodeBlueprintString(s.yaml("false"))
		if err != nil {
			t.Errorf("%s: explicit false rejected: %v", s.name, err)
			continue
		}
		if protectiveFlagIsSet(t, bp, s.name) {
			t.Errorf("%s: explicit false decoded as true", s.name)
		}
	}
}

func protectiveFlagIsSet(t *testing.T, bp Blueprint, surface string) bool {
	t.Helper()
	switch {
	case strings.HasSuffix(surface, "access.auth"):
		return bp.Screens[0].Access.Auth
	case strings.HasSuffix(surface, "multi_tenant"):
		return entityDeclarationScope(bp.Entities[0]).MultiTenant
	case strings.HasSuffix(surface, "soft_delete"):
		return entityDeclarationScope(bp.Entities[0]).SoftDelete
	case strings.HasSuffix(surface, "hidden"):
		return bp.Entities[0].Fields[0].Hidden
	case strings.HasSuffix(surface, "no_query"):
		return bp.Entities[0].Fields[0].NoQuery
	case strings.HasSuffix(surface, "read_only"):
		return bp.Entities[0].Fields[0].ReadOnly
	}
	t.Fatalf("no reader for surface %q", surface)
	return false
}
