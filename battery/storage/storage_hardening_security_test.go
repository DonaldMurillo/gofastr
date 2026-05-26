package storage_test

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/battery/storage"
)

// TestStorage_RegisterNilFactoryPanics verifies Register refuses a nil
// factory at registration time — every other code path that resolves
// the type would otherwise nil-pointer at New().
func TestStorage_RegisterNilFactoryPanics(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil {
			t.Fatalf("SECURITY: [storage] Register(nil factory) did not panic")
		}
		msg, _ := r.(string)
		if !strings.Contains(msg, "nil factory") {
			t.Errorf("panic message %q does not mention nil factory", msg)
		}
	}()
	storage.Register(storage.StorageType(9991), nil)
}

// TestStorage_NewUnknownTypeReturnsError verifies New returns a typed
// error rather than panicking for an unregistered type.
func TestStorage_NewUnknownTypeReturnsError(t *testing.T) {
	t.Parallel()
	_, err := storage.New(storage.StorageType(9992), nil)
	if err == nil {
		t.Fatalf("expected error for unknown type")
	}
	if !strings.Contains(err.Error(), "no backend registered") {
		t.Errorf("err = %v; want 'no backend registered'", err)
	}
}
