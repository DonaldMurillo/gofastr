package localapp

import (
	"slices"
	"testing"
)

func TestLocalUserIsTheAdmin(t *testing.T) {
	u := ForID("0123456789abcdef0123456789abcdef")
	if u.GetEmail() != "local@"+u.GetID() || !slices.Equal(u.GetRoles(), []string{"admin"}) {
		t.Fatalf("user = %q %q %v", u.GetID(), u.GetEmail(), u.GetRoles())
	}
	for _, id := range []string{"0123456789abcdef0123456789abcdeg", "0123456789ABCDEF0123456789ABCDEF"} {
		if Valid(id) {
			t.Errorf("Valid(%q) accepted a non-lowercase-hex id", id)
		}
	}
}
