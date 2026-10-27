package di

import (
	"fmt"
	"reflect"
	"sync"
)

// Container is a simple dependency injection container.
// It supports registering constructors or values and resolving them
// by type. Constructor functions are called once and cached as singletons.
type Container struct {
	mu         sync.RWMutex
	providers  map[reflect.Type]any  // type → constructor or value
	singletons map[reflect.Type]any  // type → resolved singleton
	resolved   map[reflect.Type]bool // type → whether it's been resolved
}

// NewContainer creates a new DI container.
func NewContainer() *Container {
	return &Container{
		providers:  make(map[reflect.Type]any),
		singletons: make(map[reflect.Type]any),
		resolved:   make(map[reflect.Type]bool),
	}
}

// Provide registers a constructor or value. The constructor can be:
//   - A function returning one value (used as singleton factory)
//   - A direct value (stored as singleton)
func (c *Container) Provide(constructor any) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	v := reflect.ValueOf(constructor)
	if !v.IsValid() {
		return fmt.Errorf("di: cannot provide nil")
	}

	// If it's a function, store it as a factory.
	if v.Kind() == reflect.Func {
		ft := v.Type()
		if ft.NumOut() != 1 {
			return fmt.Errorf("di: constructor must return exactly one value, got %d", ft.NumOut())
		}
		outType := ft.Out(0)
		c.providers[outType] = constructor
		return nil
	}

	// Direct value: store as both provider and pre-resolved singleton.
	outType := v.Type()
	c.providers[outType] = constructor
	c.singletons[outType] = constructor
	c.resolved[outType] = true
	return nil
}

// Resolve retrieves a value by type. The target must be a pointer.
// It uses the registered constructor to create the value (singleton pattern).
func (c *Container) Resolve(target any) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	tv := reflect.ValueOf(target)
	if tv.Kind() != reflect.Pointer || tv.IsNil() {
		return fmt.Errorf("di: target must be a non-nil pointer")
	}

	targetType := tv.Elem().Type()

	// Check if already resolved.
	if c.resolved[targetType] {
		val := reflect.ValueOf(c.singletons[targetType])
		tv.Elem().Set(val)
		return nil
	}

	// Look up provider.
	provider, ok := c.providers[targetType]
	if !ok {
		return fmt.Errorf("di: no provider registered for %v", targetType)
	}

	pv := reflect.ValueOf(provider)

	var result any
	if pv.Kind() == reflect.Func {
		// Call the constructor function.
		out := pv.Call(nil)
		result = out[0].Interface()
	} else {
		// Direct value.
		result = provider
	}

	// A provider returning a nil interface yields an untyped nil any:
	// reflect.ValueOf(nil) is an invalid Value and Set panics on it. Report
	// the wiring error BEFORE the singleton/resolved writes so a corrected
	// Provide stays usable (a typed nil pointer is fine — Set accepts it).
	sv := reflect.ValueOf(result)
	if !sv.IsValid() {
		return fmt.Errorf("di: provider for %v returned a nil interface, cannot resolve %v", pv.Type(), targetType)
	}

	c.singletons[targetType] = result
	c.resolved[targetType] = true
	tv.Elem().Set(sv)
	return nil
}

// InjectTagCount reports how many inject-tagged fields Inject would fill
// in a value of type t (or of the struct t points to), counting fields
// promoted from an embedded struct. It is 0 for a non-struct type.
func InjectTagCount(t reflect.Type) int {
	return injectTagCount(t, map[reflect.Type]bool{})
}

// injectTagCount counts through embeds, skipping a struct already on
// the path (type T struct{ *T } embeds itself).
func injectTagCount(t reflect.Type, active map[reflect.Type]bool) int {
	if t != nil && t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t == nil || t.Kind() != reflect.Struct || active[t] {
		return 0
	}
	active[t] = true
	defer delete(active, t)
	n := 0
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if _, ok := f.Tag.Lookup("inject"); ok {
			n++
		} else if f.Anonymous {
			n += injectTagCount(f.Type, active)
		}
	}
	return n
}

// Inject fills struct fields tagged with `inject:""` using the container.
// target must be a pointer to a struct. Fields promoted from an embedded
// struct are filled too; a nil embedded pointer whose struct holds
// inject tags is allocated first.
//
// Lazy first-time resolution of a func-provider writes the shared
// singletons/resolved maps, so the whole method takes the full write
// lock (c.mu.Lock), mirroring Resolve. Using an RLock here would let
// two concurrent cold-start injections of the same type write the same
// map simultaneously, an unrecoverable "concurrent map writes" crash.
func (c *Container) Inject(target any) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	tv := reflect.ValueOf(target)
	if tv.Kind() != reflect.Pointer || tv.IsNil() {
		return fmt.Errorf("di: target must be a non-nil pointer to a struct")
	}

	ev := tv.Elem()
	if ev.Kind() != reflect.Struct {
		return fmt.Errorf("di: target must point to a struct, got %s", ev.Kind())
	}
	return c.injectStruct(ev, map[reflect.Type]bool{})
}

// injectStruct fills the inject-tagged fields of the struct value ev,
// descending into embedded structs not already on the path (active).
// The caller holds c.mu.
func (c *Container) injectStruct(ev reflect.Value, active map[reflect.Type]bool) error {
	et := ev.Type()
	active[et] = true
	defer delete(active, et)
	for i := 0; i < et.NumField(); i++ {
		field := et.Field(i)
		if _, ok := field.Tag.Lookup("inject"); !ok {
			if field.Anonymous {
				if err := c.injectEmbedded(ev.Field(i), active); err != nil {
					return err
				}
			}
			continue
		}
		fieldType := field.Type
		// reflect Value.Set panics on a value obtained from an unexported
		// field, and Inject runs before the render pipeline's recover
		// (app.RenderPageResult), so one lowercased tagged field would
		// take the page down on every request. Report it as the wiring
		// error it is instead.
		if !ev.Field(i).CanSet() {
			return fmt.Errorf("di: injected field %s of type %v is not settable — inject-tagged fields must be exported", field.Name, fieldType)
		}
		if c.resolved[fieldType] {
			cached := reflect.ValueOf(c.singletons[fieldType])
			if !cached.IsValid() {
				return fmt.Errorf("di: cached singleton for injected field %s of type %v is a nil interface, cannot inject", field.Name, fieldType)
			}
			ev.Field(i).Set(cached)
		} else if provider, ok := c.providers[fieldType]; ok {
			pv := reflect.ValueOf(provider)
			var result any
			if pv.Kind() == reflect.Func {
				out := pv.Call(nil)
				result = out[0].Interface()
			} else {
				result = provider
			}
			// Same nil-interface guard as Resolve: an invalid ValueOf(result)
			// panics in Set, and the singleton/resolved writes below it would
			// poison the cache for a corrected provider. Inject runs before
			// the render pipeline's recover, so a wiring error must be an
			// error, never a panic.
			sv := reflect.ValueOf(result)
			if !sv.IsValid() {
				return fmt.Errorf("di: provider for injected field %s of type %v returned a nil interface, cannot inject", field.Name, fieldType)
			}
			c.singletons[fieldType] = result
			c.resolved[fieldType] = true
			ev.Field(i).Set(sv)
		} else {
			return fmt.Errorf("di: no provider registered for injected field %s of type %v", field.Name, fieldType)
		}
	}
	return nil
}

// injectEmbedded fills an embedded struct field: a value struct in
// place, a pointer through its target. A nil pointer is allocated when
// its struct holds inject tags; one that cannot be set (an unexported
// embedded pointer type) is reported, since its tags would stay empty.
// A struct already on the path is left alone: it is a cycle, not a
// second set of fields.
func (c *Container) injectEmbedded(fv reflect.Value, active map[reflect.Type]bool) error {
	switch {
	case fv.Kind() == reflect.Struct:
		return c.injectStruct(fv, active)
	case fv.Kind() == reflect.Pointer && fv.Type().Elem().Kind() == reflect.Struct:
		if active[fv.Type().Elem()] || InjectTagCount(fv.Type()) == 0 {
			return nil
		}
		if fv.IsNil() {
			if !fv.CanSet() {
				return fmt.Errorf("di: embedded %v holds inject-tagged fields but is nil and unexported, so it cannot be allocated", fv.Type())
			}
			fv.Set(reflect.New(fv.Type().Elem()))
		}
		return c.injectStruct(fv.Elem(), active)
	}
	return nil
}
