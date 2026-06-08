package storage_test

import (
	"testing"

	"github.com/DonaldMurillo/gofastr/battery/storage"
	"github.com/DonaldMurillo/gofastr/framework"
)

// TestStorageBatteryInterface verifies that NewBattery returns a value that
// satisfies framework.Battery.
func TestStorageBatteryInterface(t *testing.T) {
	b := storage.NewBattery(storage.NewMemoryStorage())

	var _ framework.Battery = b

	if b.Name() != "storage" {
		t.Errorf("Name() = %q, want 'storage'", b.Name())
	}
}
