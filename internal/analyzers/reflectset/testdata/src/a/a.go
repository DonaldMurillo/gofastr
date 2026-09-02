// Package a holds the reflectset fixture reduced from the real bug
// site: core-ui/di di.go Inject as it was before fix e936f791 (probe
// TestInjectUnexportedFieldErrors).
package a

import (
	"fmt"
	"reflect"
)

// Container, reduced from di.Container.
type Container struct {
	resolved   map[reflect.Type]bool
	singletons map[reflect.Type]any
	providers  map[reflect.Type]any
}

// Inject writes provider results into inject-tagged fields. Pre-fix,
// one lowercased tagged field panicked every render with "reflect:
// reflect.Value.Set using value obtained using unexported field".
func (c *Container) Inject(target any) error {
	tv := reflect.ValueOf(target)
	if tv.Kind() != reflect.Pointer || tv.IsNil() {
		return fmt.Errorf("di: target must be a non-nil pointer to a struct")
	}
	ev := tv.Elem()
	et := ev.Type()
	for i := range et.NumField() {
		field := et.Field(i)
		if _, ok := field.Tag.Lookup("inject"); !ok {
			continue
		}
		fieldType := field.Type
		if c.resolved[fieldType] {
			ev.Field(i).Set(reflect.ValueOf(c.singletons[fieldType])) // want `reflect Set on a field value without a CanSet check`
		} else if provider, ok := c.providers[fieldType]; ok {
			c.singletons[fieldType] = provider
			c.resolved[fieldType] = true
			ev.Field(i).Set(reflect.ValueOf(provider)) // want `reflect Set on a field value without a CanSet check`
		} else {
			return fmt.Errorf("di: no provider registered for injected field %s", field.Name)
		}
	}
	return nil
}

// InjectFixed is the fix posture: report the wiring error instead of
// panicking per request.
func (c *Container) InjectFixed(target any) error {
	tv := reflect.ValueOf(target)
	if tv.Kind() != reflect.Pointer || tv.IsNil() {
		return fmt.Errorf("di: target must be a non-nil pointer to a struct")
	}
	ev := tv.Elem()
	et := ev.Type()
	for i := range et.NumField() {
		field := et.Field(i)
		if _, ok := field.Tag.Lookup("inject"); !ok {
			continue
		}
		if !ev.Field(i).CanSet() {
			return fmt.Errorf("di: injected field %s is not settable — inject-tagged fields must be exported", field.Name)
		}
		fieldType := field.Type
		if c.resolved[fieldType] {
			ev.Field(i).Set(reflect.ValueOf(c.singletons[fieldType]))
		} else if provider, ok := c.providers[fieldType]; ok {
			c.singletons[fieldType] = provider
			c.resolved[fieldType] = true
			ev.Field(i).Set(reflect.ValueOf(provider))
		} else {
			return fmt.Errorf("di: no provider registered for injected field %s", field.Name)
		}
	}
	return nil
}

// setThroughPointer mutates via Elem(), not a field: other rules apply.
func setThroughPointer(p any, val any) {
	reflect.ValueOf(p).Elem().Set(reflect.ValueOf(val))
}
