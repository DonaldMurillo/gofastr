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
	if tv.Kind() != reflect.Ptr || tv.IsNil() {
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

	c.singletons[targetType] = result
	c.resolved[targetType] = true
	tv.Elem().Set(reflect.ValueOf(result))
	return nil
}

// Inject fills struct fields tagged with `inject:""` using the container.
// target must be a pointer to a struct.
//
// Lazy first-time resolution of a func-provider writes the shared
// singletons/resolved maps, so the whole method takes the full write
// lock (c.mu.Lock), mirroring Resolve. Using an RLock here would let
// two concurrent cold-start injections of the same type write the same
// map simultaneously — an unrecoverable "concurrent map writes" crash.
func (c *Container) Inject(target any) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	tv := reflect.ValueOf(target)
	if tv.Kind() != reflect.Ptr || tv.IsNil() {
		return fmt.Errorf("di: target must be a non-nil pointer to a struct")
	}

	ev := tv.Elem()
	if ev.Kind() != reflect.Struct {
		return fmt.Errorf("di: target must point to a struct, got %s", ev.Kind())
	}

	et := ev.Type()
	for i := 0; i < et.NumField(); i++ {
		field := et.Field(i)
		if _, ok := field.Tag.Lookup("inject"); !ok {
			continue
		}
		fieldType := field.Type
		if c.resolved[fieldType] {
			ev.Field(i).Set(reflect.ValueOf(c.singletons[fieldType]))
		} else if provider, ok := c.providers[fieldType]; ok {
			pv := reflect.ValueOf(provider)
			var result any
			if pv.Kind() == reflect.Func {
				out := pv.Call(nil)
				result = out[0].Interface()
			} else {
				result = provider
			}
			c.singletons[fieldType] = result
			c.resolved[fieldType] = true
			ev.Field(i).Set(reflect.ValueOf(result))
		} else {
			return fmt.Errorf("di: no provider registered for injected field %s of type %v", field.Name, fieldType)
		}
	}
	return nil
}
