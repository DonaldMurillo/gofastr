package entity

import (
	"context"
	"testing"
)

func TestValidateTakesNilData(t *testing.T) {
	vr := NewValidationRegistry()
	vr.RegisterValidator(func(_ context.Context, data map[string]any) map[string]string {
		data["seen"] = true
		return nil
	})
	if errs := vr.Validate(context.Background(), nil); len(errs) != 0 {
		t.Fatalf("errs = %v", errs)
	}
}
