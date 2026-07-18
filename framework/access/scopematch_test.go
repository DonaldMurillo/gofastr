package access

import "testing"

// TestScopeMatch_Algebra pins the resource:verb wildcard grammar that
// ScopeMatch owns: exact match, resource wildcard "posts:*", verb
// wildcard "*:read", and grant-all "*:*". These mirror the cases
// battery/auth's TestScopeMatches asserts against the SAME matcher (now
// delegated here), so the two stay byte-for-byte equivalent.
func TestScopeMatch_Algebra(t *testing.T) {
	cases := []struct {
		granted  []Permission
		required Permission
		want     bool
	}{
		// Exact match.
		{[]Permission{"posts:read"}, "posts:read", true},
		{[]Permission{"posts:read"}, "posts:write", false},
		// Resource wildcard: any verb on the named resource.
		{[]Permission{"posts:*"}, "posts:read", true},
		{[]Permission{"posts:*"}, "posts:delete", true},
		{[]Permission{"posts:*"}, "users:read", false},
		// Verb wildcard: the named verb across any resource.
		{[]Permission{"*:read"}, "posts:read", true},
		{[]Permission{"*:read"}, "users:read", true},
		{[]Permission{"*:read"}, "posts:write", false},
		// Grant-all.
		{[]Permission{"*:*"}, "anything:here", true},
		{[]Permission{"*:*"}, "posts:read", true},
		// Multiple granted scopes; any one satisfies.
		{[]Permission{"posts:read", "users:*"}, "users:delete", true},
		{[]Permission{"posts:read", "users:*"}, "posts:write", false},
	}
	for _, c := range cases {
		if got := ScopeMatch(c.granted, c.required); got != c.want {
			t.Errorf("ScopeMatch(%v, %q) = %v, want %v", c.granted, c.required, got, c.want)
		}
	}
}

// TestScopeMatch_EmptyGrantsDeny locks down the secure-by-default answer:
// no granted scopes ⇒ nothing is satisfied, regardless of the required
// scope. Both nil and a non-nil empty slice deny.
func TestScopeMatch_EmptyGrantsDeny(t *testing.T) {
	if ScopeMatch(nil, "posts:read") {
		t.Error("ScopeMatch(nil, ...) = true; empty grants must deny")
	}
	if ScopeMatch([]Permission{}, "posts:read") {
		t.Error("ScopeMatch([], ...) = true; empty grants must deny")
	}
}

// TestScopeMatch_InvalidRequiredDenies confirms a required scope that does
// not parse as "resource:verb" can never be satisfied — the matcher fails
// closed rather than mis-handling malformed input. Note splitScope splits on
// the FIRST colon, so a multi-segment value like "a:b:c" parses as
// resource="a", verb="b:c" and is therefore matchable by "*:*" — that is
// the documented behavior, not a bug, and is excluded from the deny list.
func TestScopeMatch_InvalidRequiredDenies(t *testing.T) {
	cases := []Permission{"no-colon", ":verb", "res:", ""}
	granted := []Permission{"*:*"}
	for _, req := range cases {
		if ScopeMatch(granted, req) {
			t.Errorf("ScopeMatch(*:*, %q) = true; malformed required must deny", req)
		}
	}
}

// TestScopeMatch_DoesNotExpandWildcards is the registry-independence guard.
// RolePolicy.Grant expands resource wildcards like "teams:*" against the
// capability registry at GRANT time. ScopeMatch must NOT: it matches the
// wildcard literally and never consults the registry. So "teams:*" grants
// "teams:<anything>" purely through the algebra, with zero registered
// capabilities and no RolePolicy in play.
func TestScopeMatch_DoesNotExpandWildcards(t *testing.T) {
	// Deliberately no registry, no RolePolicy — ScopeMatch is a pure
	// function of its two arguments.
	granted := []Permission{"teams:*"}
	for _, req := range []Permission{"teams:read", "teams:write", "teams:delete"} {
		if !ScopeMatch(granted, req) {
			t.Errorf("ScopeMatch(teams:*, %q) = false; literal wildcard must match without registry", req)
		}
	}
	// And it does not over-grant into another resource.
	if ScopeMatch(granted, "posts:read") {
		t.Error("ScopeMatch(teams:*, posts:read) = true; wildcard must stay resource-scoped")
	}
}

// TestValidScope mirrors battery/auth's scopePattern so the mint-time
// validator and the matcher share one closed vocabulary in one place.
func TestValidScope(t *testing.T) {
	valid := []string{
		"posts:read",
		"posts:*",
		"*:read",
		"*:*",
		"resource-name:verb-name",
		"abc123:x-y_z",
	}
	for _, s := range valid {
		if !ValidScope(s) {
			t.Errorf("ValidScope(%q) = false, want true", s)
		}
	}
	invalid := []string{
		"",                 // empty
		"posts",            // no colon
		":verb",            // empty resource
		"res:",             // empty verb
		"a:b:c",            // second colon
		"POSTS:read",       // uppercase
		"posts:read extra", // space
		"posts.read:write", // dot not in vocab
	}
	for _, s := range invalid {
		if ValidScope(s) {
			t.Errorf("ValidScope(%q) = true, want false", s)
		}
	}
}
