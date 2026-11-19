package piicheck

import "testing"

func TestFieldTokens(t *testing.T) {
	for _, tc := range []struct {
		name string
		want bool
	}{
		{"SSNBackup", true},
		{"user_EMAIL", true},
		{"shipping-address", true},
		{"contact.phone2", true},
		{"cardinality", false},
		{"external_id", false},
	} {
		if got := Field(tc.name); got != tc.want {
			t.Errorf("Field(%q) = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestScopeAndAccess(t *testing.T) {
	for _, tc := range []struct {
		owner, permission string
		tenant, want      bool
	}{
		{"", "", false, false},
		{"", " anyone ", false, false},
		{"", " people:read ", false, true},
		{"user_id", "anyone", false, true},
		{"", "", true, true},
	} {
		if got := Scoped(tc.owner, tc.tenant, GatesAccess(tc.permission)); got != tc.want {
			t.Errorf("scope for owner %q, tenant %v, permission %q = %v, want %v",
				tc.owner, tc.tenant, tc.permission, got, tc.want)
		}
	}
}
