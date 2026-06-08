package search_test

import (
	"testing"

	"github.com/DonaldMurillo/gofastr/battery/search"
	"github.com/DonaldMurillo/gofastr/framework"
)

// TestSearchBatteryInterface verifies that NewBattery returns a value that
// satisfies framework.Battery.
func TestSearchBatteryInterface(t *testing.T) {
	b := search.NewBattery(search.NewMemory())

	var _ framework.Battery = b

	if b.Name() != "search" {
		t.Errorf("Name() = %q, want 'search'", b.Name())
	}
}
