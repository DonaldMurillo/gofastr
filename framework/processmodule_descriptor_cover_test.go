package framework

import (
	"errors"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/access"
)

// This file adds unit coverage for the descriptor validation helpers, error
// types, and the intersect/canonicalize logic in processmodule.go that are
// not already covered by the existing ValidateProcessModuleDescriptor tests.

// ---- DescriptorValidationError ----

func TestDescriptorValidationError_methods(t *testing.T) {
	e := descErr("name", "empty", "descriptor: name is required")
	if e.Error() != "descriptor: name is required" {
		t.Errorf("Error() = %q", e.Error())
	}
	// Is matches on Field+Rule.
	if !errors.Is(e, descErr("name", "empty", "other msg")) {
		t.Error("Is must match same Field+Rule")
	}
	// Is does not match a different rule.
	if errors.Is(e, descErr("name", "pattern", "x")) {
		t.Error("Is must not match different Rule")
	}
	// Is does not match an unrelated error type.
	if errors.Is(e, errors.New("plain")) {
		t.Error("Is must not match a plain error")
	}
	// nil receiver renders safely.
	var nilE *DescriptorValidationError
	if nilE.Error() != "<nil>" {
		t.Errorf("nil Error() = %q, want <nil>", nilE.Error())
	}
}

// ---- matchIdent / matchID ----

func TestMatchIdent_validatesNames(t *testing.T) {
	valid := []string{"a", "Demo", "my-mod", "my_mod", "Mod1", "A1-2_3"}
	for _, s := range valid {
		if !matchIdent(s) {
			t.Errorf("matchIdent(%q) = false, want true", s)
		}
		if !matchID(s) {
			t.Errorf("matchID(%q) = false, want true", s)
		}
	}
	invalid := []string{"", "1leading-digit", "-leading-hyphen", "_leading-underscore", "has space", "has/slash", "has:colon"}
	for _, s := range invalid {
		if matchIdent(s) {
			t.Errorf("matchIdent(%q) = true, want false", s)
		}
	}
}

// ---- validateHexSHA256 ----

func TestValidateHexSHA256_branches(t *testing.T) {
	if err := validateHexSHA256("f", ""); err == nil {
		t.Error("empty string must error")
	}
	if err := validateHexSHA256("f", "tooshort"); err == nil {
		t.Error("wrong length must error")
	}
	if err := validateHexSHA256("f", strings.Repeat("z", 64)); err == nil {
		t.Error("non-hex 64-char must error")
	}
	if err := validateHexSHA256("f", strings.Repeat("a", 64)); err != nil {
		t.Errorf("valid 64-hex returned error: %v", err)
	}
}

// ---- nonGrantableReason ----

func TestNonGrantableReason_carveOuts(t *testing.T) {
	denied := []access.Permission{
		"CrossOwnerRead",
		"*",
		"*:*",
		"*:read",
	}
	for _, p := range denied {
		if reason := nonGrantableReason(p); reason == "" {
			t.Errorf("nonGrantableReason(%q) = empty, want a reason", p)
		}
	}
	// Resource-scoped wildcards and concrete scopes are grantable.
	grantable := []access.Permission{
		"articles:*",
		"articles:read",
		"tickets:read:all",
	}
	for _, p := range grantable {
		if reason := nonGrantableReason(p); reason != "" {
			t.Errorf("nonGrantableReason(%q) = %q, want empty", p, reason)
		}
	}
	// Malformed (no colon) is not carved out here — ValidScope rejects it.
	if reason := nonGrantableReason(access.Permission("garbage")); reason != "" {
		t.Errorf("nonGrantableReason(garbage) = %q, want empty (ValidScope handles)", reason)
	}
}

// ---- splitResourceVerb ----

func TestSplitResourceVerb(t *testing.T) {
	cases := []struct {
		in   string
		res  string
		verb string
		ok   bool
	}{
		{"articles:read", "articles", "read", true},
		{"tickets:read:all", "tickets", "read:all", true},
		{"no-colon", "", "", false},
		{":verb", "", "", false},     // empty resource
		{"resource:", "", "", false}, // empty verb
	}
	for _, c := range cases {
		res, verb, ok := splitResourceVerb(c.in)
		if res != c.res || verb != c.verb || ok != c.ok {
			t.Errorf("splitResourceVerb(%q) = %q,%q,%t want %q,%q,%t",
				c.in, res, verb, ok, c.res, c.verb, c.ok)
		}
	}
}

// ---- intersectGrants ----

func TestIntersectGrants_emptyInputs(t *testing.T) {
	if got := intersectGrants(nil, ApprovedGrants{"a:read"}); got != nil {
		t.Errorf("intersect(nil, …) = %+v, want nil", got)
	}
	if got := intersectGrants([]access.Permission{"a:read"}, nil); got != nil {
		t.Errorf("intersect(…, nil) = %+v, want nil", got)
	}
}

func TestIntersectGrants_preservesOrderDropsDups(t *testing.T) {
	requested := []access.Permission{"a:read", "b:read", "a:read", "c:read"}
	approved := ApprovedGrants{"a:*", "b:read", "c:write"}
	got := intersectGrants(requested, approved)
	// a:read matches a:* (ScopeMatch), b:read matches, a:read dup dropped, c:read not approved.
	want := []access.Permission{"a:read", "b:read"}
	if len(got) != len(want) {
		t.Fatalf("intersect = %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("intersect[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

// ---- ComputeSurfaceSHA256 determinism + tool omission ----

func TestComputeSurfaceSHA256_deterministic(t *testing.T) {
	d := validDescriptor()
	a, err := ComputeSurfaceSHA256(d)
	if err != nil {
		t.Fatalf("compute: %v", err)
	}
	b, _ := ComputeSurfaceSHA256(d)
	if a != b {
		t.Error("ComputeSurfaceSHA256 not deterministic")
	}
	// Changing a route changes the digest.
	d2 := d
	d2.Routes = append(d2.Routes, RouteDeclaration{ID: "new", Method: "POST", Path: "/new"})
	if c, _ := ComputeSurfaceSHA256(d2); c == a {
		t.Error("adding a route did not change the surface digest")
	}
}

func TestComputeSurfaceSHA256_omitsToolsWhenEmpty(t *testing.T) {
	d := validDescriptor()
	d.Tools = nil
	raw, err := surfaceCanonical(d)
	if err != nil {
		t.Fatalf("surfaceCanonical: %v", err)
	}
	// omitempty tag: an empty Tools slice must not emit a "tools" key.
	if strings.Contains(string(raw), `"tools"`) {
		t.Errorf("surface canonical emitted tools key for empty Tools: %s", string(raw))
	}
}

// ---- ValidateProcessModuleDescriptor remaining branches ----

func TestValidate_rejectsBadRouteMethodAndPath(t *testing.T) {
	d := validDescriptor()
	d.Routes = []RouteDeclaration{{ID: "r", Method: "get", Path: "/r"}} // lowercase method
	if _, err := ValidateProcessModuleDescriptor(d, nil); err == nil {
		t.Error("lowercase method must error")
	}
	d = validDescriptor()
	d.Routes = []RouteDeclaration{{ID: "r", Method: "GET", Path: "no-slash"}} // missing leading /
	if _, err := ValidateProcessModuleDescriptor(d, nil); err == nil {
		t.Error("path without leading slash must error")
	}
}

func TestValidate_rejectsDuplicateRouteAndToolIDs(t *testing.T) {
	d := validDescriptor()
	d.Routes = []RouteDeclaration{
		{ID: "dup", Method: "GET", Path: "/a"},
		{ID: "dup", Method: "GET", Path: "/b"},
	}
	if _, err := ValidateProcessModuleDescriptor(d, nil); err == nil {
		t.Error("duplicate route id must error")
	}
	d = validDescriptor()
	d.Tools = []ToolDigest{
		{ID: "t", SHA256: strings.Repeat("a", 64)},
		{ID: "t", SHA256: strings.Repeat("b", 64)},
	}
	if _, err := ValidateProcessModuleDescriptor(d, nil); err == nil {
		t.Error("duplicate tool id must error")
	}
}

func TestValidate_rejectsGrantsCapAndBadScope(t *testing.T) {
	// Over the cap (maxModuleGrants = 32).
	d := validDescriptor()
	grants := make([]access.Permission, maxModuleGrants+1)
	for i := range grants {
		grants[i] = access.Permission("x:read")
	}
	d.RequestedGrants = grants
	if _, err := ValidateProcessModuleDescriptor(d, nil); err == nil {
		t.Error("over-cap grants must error")
	}
	// Non-grantable in the list.
	d = validDescriptor()
	d.RequestedGrants = []access.Permission{"CrossOwnerRead"}
	if _, err := ValidateProcessModuleDescriptor(d, nil); err == nil {
		t.Error("CrossOwnerRead must error (carve-out)")
	}
	// Invalid scope shape.
	d = validDescriptor()
	d.RequestedGrants = []access.Permission{"nocolon"}
	if _, err := ValidateProcessModuleDescriptor(d, nil); err == nil {
		t.Error("invalid scope must error")
	}
	// Empty grant.
	d = validDescriptor()
	d.RequestedGrants = []access.Permission{""}
	if _, err := ValidateProcessModuleDescriptor(d, nil); err == nil {
		t.Error("empty grant must error")
	}
}

func TestValidate_rejectsBadLimits(t *testing.T) {
	d := validDescriptor()
	d.Limits.Deadline = -1
	if _, err := ValidateProcessModuleDescriptor(d, nil); err == nil {
		t.Error("negative deadline must error")
	}
	d = validDescriptor()
	d.Limits.Deadline = maxModuleCallDeadline + 1
	if _, err := ValidateProcessModuleDescriptor(d, nil); err == nil {
		t.Error("over-ceiling deadline must error")
	}
	d = validDescriptor()
	d.Limits.FrameBytes = -1
	if _, err := ValidateProcessModuleDescriptor(d, nil); err == nil {
		t.Error("negative frame_bytes must error")
	}
	d = validDescriptor()
	d.Limits.Inflight = -1
	if _, err := ValidateProcessModuleDescriptor(d, nil); err == nil {
		t.Error("negative inflight must error")
	}
}

func TestValidate_rejectsBadMigrationGroup(t *testing.T) {
	d := validDescriptor()
	d.MigrationGroup = "1bad" // leading digit
	if _, err := ValidateProcessModuleDescriptor(d, nil); err == nil {
		t.Error("malformed migration_group must error")
	}
}

func TestValidate_acceptsValidWithEffectiveGrants(t *testing.T) {
	d := validDescriptor()
	eff, err := ValidateProcessModuleDescriptor(d, ApprovedGrants{"articles:*"})
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	// Both requested scopes match articles:*.
	if len(eff) != 2 {
		t.Errorf("effective grants = %+v, want 2", eff)
	}
}
