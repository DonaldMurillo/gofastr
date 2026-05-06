package framework

import (
	"context"
	"fmt"
)

// ValidatorFunc validates entity data and returns field-level errors.
// The returned map is field name → error message (empty map means valid).
type ValidatorFunc func(ctx context.Context, data map[string]any) map[string]string

// ValidationRegistry holds a chain of validator functions.
type ValidationRegistry struct {
	validators []ValidatorFunc
}

// NewValidationRegistry creates an empty ValidationRegistry.
func NewValidationRegistry() *ValidationRegistry {
	return &ValidationRegistry{}
}

// RegisterValidator appends a validator function to the chain.
func (vr *ValidationRegistry) RegisterValidator(fn ValidatorFunc) {
	vr.validators = append(vr.validators, fn)
}

// Validate runs all registered validators and collects every field error.
// The returned map is field name → error message. A nil/empty map means valid.
func (vr *ValidationRegistry) Validate(ctx context.Context, data map[string]any) map[string]string {
	errors := make(map[string]string)
	for _, fn := range vr.validators {
		fieldErrors := fn(ctx, data)
		for field, msg := range fieldErrors {
			errors[field] = msg
		}
	}
	return errors
}

// Validators returns the number of registered validators (for testing).
func (vr *ValidationRegistry) Validators() int {
	return len(vr.validators)
}

// --- Built-in validators ---

// Required returns a validator that checks the given fields are present and non-zero.
func Required(fields ...string) ValidatorFunc {
	return func(ctx context.Context, data map[string]any) map[string]string {
		errors := make(map[string]string)
		for _, field := range fields {
			val, ok := data[field]
			if !ok || isZero(val) {
				errors[field] = "is required"
			}
		}
		return errors
	}
}

// Unique returns a validator that checks a field value is unique using the provided check function.
// The checkFn receives the field value and returns true if the value is unique (not taken).
func Unique(field string, checkFn func(ctx context.Context, value any) bool) ValidatorFunc {
	return func(ctx context.Context, data map[string]any) map[string]string {
		errors := make(map[string]string)
		val, ok := data[field]
		if !ok {
			return errors
		}
		if !checkFn(ctx, val) {
			errors[field] = "must be unique"
		}
		return errors
	}
}

// Custom returns a validator with a given name that runs the provided function.
// The fn returns a map of field→error for any violations found.
func Custom(name string, fn func(ctx context.Context, data map[string]any) map[string]string) ValidatorFunc {
	return func(ctx context.Context, data map[string]any) map[string]string {
		return fn(ctx, data)
	}
}

// isZero checks if a value is nil or the zero value for its type.
func isZero(v any) bool {
	if v == nil {
		return true
	}
	switch val := v.(type) {
	case string:
		return val == ""
	case int:
		return val == 0
	case int64:
		return val == 0
	case float64:
		return val == 0
	case bool:
		return !val
	default:
		return false
	}
}

// FormatValidationErrors formats a map of field errors into a user-friendly string slice.
func FormatValidationErrors(errors map[string]string) []string {
	if len(errors) == 0 {
		return nil
	}
	out := make([]string, 0, len(errors))
	for field, msg := range errors {
		out = append(out, fmt.Sprintf("%s %s", field, msg))
	}
	return out
}
