package di

import (
	"errors"
	"fmt"
	"reflect"
	"sync"
)

// Container is a simple dependency injection container.
// It supports registering constructors or values and resolving them
// by type. Constructor functions are called once and cached as singletons.
//
// A constructor runs outside the container's lock, so it may Resolve or
// Inject the services it needs. A constructor that needs its own type,
// directly or through another constructor, waits on itself forever.
type Container struct {
	mu         sync.Mutex
	providers  map[reflect.Type]any    // type → constructor or value
	singletons map[reflect.Type]any    // type → resolved singleton
	resolved   map[reflect.Type]bool   // type → whether it's been resolved
	building   map[reflect.Type]*build // type → constructor call in flight
}

// build is one in-flight constructor call. Callers that find it wait on
// done and share its outcome.
type build struct {
	done chan struct{}
	val  any
	err  error
}

// errNilSingleton marks a constructor that returned a nil interface;
// Resolve and Inject each word it for their caller.
var errNilSingleton = errors.New("di: nil interface singleton")

// NewContainer creates a new DI container.
func NewContainer() *Container {
	return &Container{
		providers:  make(map[reflect.Type]any),
		singletons: make(map[reflect.Type]any),
		resolved:   make(map[reflect.Type]bool),
		building:   make(map[reflect.Type]*build),
	}
}

// singleton returns the value for t and whether a provider exists. A
// constructor is called at most once at a time, never under c.mu; a
// second caller for a type still building waits for the first and
// shares its result. A failed or panicking call caches nothing, so the
// next caller tries again.
func (c *Container) singleton(t reflect.Type) (any, bool, error) {
	c.mu.Lock()
	if c.resolved[t] {
		// Never a nil interface: one is reported below, not cached.
		v := c.singletons[t]
		c.mu.Unlock()
		return v, true, nil
	}
	provider, ok := c.providers[t]
	if !ok {
		c.mu.Unlock()
		return nil, false, nil
	}
	if b := c.building[t]; b != nil {
		c.mu.Unlock()
		<-b.done
		return b.val, true, b.err
	}
	b := &build{done: make(chan struct{})}
	c.building[t] = b
	c.mu.Unlock()

	settled := false
	defer func() {
		if settled {
			return
		}
		// The constructor panicked: release the waiters before the panic
		// continues up this caller's stack.
		b.err = fmt.Errorf("di: constructor for %v panicked", t)
		c.mu.Lock()
		delete(c.building, t)
		c.mu.Unlock()
		close(b.done)
	}()
	result := provider
	if pv := reflect.ValueOf(provider); pv.Kind() == reflect.Func {
		result = pv.Call(nil)[0].Interface()
	}
	settled = true

	// A nil interface is reported, not cached, so a corrected Provide
	// stays usable.
	var err error
	if !reflect.ValueOf(result).IsValid() {
		err = errNilSingleton
	}
	c.mu.Lock()
	delete(c.building, t)
	if err == nil {
		c.singletons[t] = result
		c.resolved[t] = true
	}
	c.mu.Unlock()
	b.val, b.err = result, err
	close(b.done)
	return result, true, err
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
	tv := reflect.ValueOf(target)
	if tv.Kind() != reflect.Pointer || tv.IsNil() {
		return fmt.Errorf("di: target must be a non-nil pointer")
	}
	targetType := tv.Elem().Type()
	v, ok, err := c.singleton(targetType)
	switch {
	case !ok:
		return fmt.Errorf("di: no provider registered for %v", targetType)
	case errors.Is(err, errNilSingleton):
		// reflect.ValueOf(nil) is an invalid Value and Set panics on it;
		// a typed nil pointer is fine.
		return fmt.Errorf("di: provider for %v returned a nil interface, cannot resolve it", targetType)
	case err != nil:
		return err
	}
	tv.Elem().Set(reflect.ValueOf(v))
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
// Each field resolves the way Resolve does, so its constructor runs
// outside the container's lock and may itself Resolve or Inject.
func (c *Container) Inject(target any) error {
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
		v, ok, err := c.singleton(fieldType)
		switch {
		case !ok:
			return fmt.Errorf("di: no provider registered for injected field %s of type %v", field.Name, fieldType)
		case errors.Is(err, errNilSingleton):
			// Inject runs before the render pipeline's recover, so a
			// wiring error must be an error, never a Set panic.
			return fmt.Errorf("di: provider for injected field %s of type %v returned a nil interface, cannot inject", field.Name, fieldType)
		case err != nil:
			return err
		}
		ev.Field(i).Set(reflect.ValueOf(v))
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
