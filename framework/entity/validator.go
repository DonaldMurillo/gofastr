package entity

import (
	"context"
	"fmt"
	"maps"
	"reflect"
	"slices"

	"github.com/DonaldMurillo/gofastr/core/schema"
)

// ValidatorFunc validates entity data, returning field name → message; empty means valid.
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

// Validate runs every validator and merges their field errors.
func (vr *ValidationRegistry) Validate(ctx context.Context, data map[string]any) map[string]string {
	if data == nil {
		data = map[string]any{}
	}
	errors := make(map[string]string)
	for _, fn := range vr.validators {
		fieldErrors := fn(ctx, data)
		maps.Copy(errors, fieldErrors)
	}
	return errors
}

// Validators returns the number of registered validators.
func (vr *ValidationRegistry) Validators() int {
	return len(vr.validators)
}

// Required returns a validator that checks the fields are present and non-zero.
func Required(fields ...string) ValidatorFunc {
	fields = slices.Clone(fields)
	return func(ctx context.Context, data map[string]any) map[string]string {
		errors := make(map[string]string)
		for _, field := range fields {
			val, ok := data[field]
			if !ok || isZero(val) {
				errors[field] = schema.ReasonRequired.Text(ctx)
			}
		}
		return errors
	}
}

// Unique returns a validator that refuses a value checkFn reports taken.
func Unique(field string, checkFn func(ctx context.Context, value any) bool) ValidatorFunc {
	return func(ctx context.Context, data map[string]any) map[string]string {
		errors := make(map[string]string)
		val, ok := data[field]
		if !ok {
			return errors
		}
		if !checkFn(ctx, val) {
			errors[field] = schema.ReasonUniqueTaken.Text(ctx)
		}
		return errors
	}
}

// Custom returns a validator running fn.
func Custom(name string, fn func(ctx context.Context, data map[string]any) map[string]string) ValidatorFunc {
	return func(ctx context.Context, data map[string]any) map[string]string {
		return fn(ctx, data)
	}
}

func isZero(v any) bool {
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Invalid:
		return true
	case reflect.Bool, reflect.String,
		reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr,
		reflect.Float32, reflect.Float64, reflect.Complex64, reflect.Complex128,
		reflect.Chan, reflect.Func, reflect.Map, reflect.Pointer, reflect.Slice:
		return rv.IsZero()
	default:
		return false
	}
}

// FormatValidationErrors formats field errors as "field message" strings.
func FormatValidationErrors(errors map[string]string) []string {
	if len(errors) == 0 {
		return nil
	}
	out := make([]string, 0, len(errors))
	for _, field := range slices.Sorted(maps.Keys(errors)) {
		out = append(out, fmt.Sprintf("%s %s", field, errors[field]))
	}
	return out
}
