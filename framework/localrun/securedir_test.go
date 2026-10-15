package localrun

import (
	"strings"
	"testing"
)

// The owner comparison every OS uses: a uid on Unix, the token's user
// or default-owner SID on Windows, where it cannot run in CI.
func TestCheckOwner(t *testing.T) {
	const user, admins = "S-1-5-21-1-2-3-1001", "S-1-5-32-544"
	if err := checkOwner("d", user, user, admins); err != nil {
		t.Fatalf("own user: %v", err)
	}
	if err := checkOwner("d", admins, user, admins); err != nil {
		t.Fatalf("token default owner: %v", err)
	}
	if err := checkOwner("d", "S-1-5-21-1-2-3-1002", user, admins); err == nil || !strings.Contains(err.Error(), "not this user") {
		t.Fatalf("another user: err = %v", err)
	}
	// An identity that failed to resolve matches nothing, not an empty
	// owner.
	if err := checkOwner("d", "", user, ""); err == nil {
		t.Fatal("an empty owner matched an unresolved identity")
	}
}
