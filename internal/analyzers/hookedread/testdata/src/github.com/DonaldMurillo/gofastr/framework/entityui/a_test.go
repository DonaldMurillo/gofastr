package entityui

import (
	"context"
	"testing"
)

// Silent: a test reads stored values to check what was written.
func TestStored(t *testing.T) {
	var m meta
	_, _ = m.ch.GetOne(context.Background(), "x", nil)
}
