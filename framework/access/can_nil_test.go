package access_test

import (
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/access"
)

// Can is the gate every sibling (GetPermissions, GetRoles, CanResource,
// CanResourceExact) already holds closed on a nil context; Can itself read
// ctx.Value and panicked.
func TestCanNilContextFailsClosed(t *testing.T) {
	t.Parallel()
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("access.Can(nil, …) panicked: %v", r)
		}
	}()
	//nolint:staticcheck // intentionally passing nil to verify guard
	if access.Can(nil, "posts:read") {
		t.Fatal("access.Can(nil, …) granted")
	}
}
