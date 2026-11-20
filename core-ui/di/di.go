package di

import (
	"errors"
	"fmt"
	"reflect"
	"sync"
)

// Container holds one singleton per type, built from a provided constructor or value.
type Container struct {
	mu         sync.Mutex
	providers  map[reflect.Type]any
	singletons map[reflect.Type]any
	resolved   map[reflect.Type]bool
	building   map[reflect.Type]*build
}

type build struct {
	done chan struct{}
	val  any
	err  error
}

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

func (c *Container) singleton(t reflect.Type) (any, bool, error) {
	c.mu.Lock()
	if c.resolved[t] {
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

// Provide registers a no-argument constructor returning one value, or a
// value itself, for that value's type.
func (c *Container) Provide(constructor any) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	v := reflect.ValueOf(constructor)
	if !v.IsValid() {
		return fmt.Errorf("di: cannot provide nil")
	}

	if v.Kind() == reflect.Func {
		ft := v.Type()
		if ft.NumOut() != 1 {
			return fmt.Errorf("di: constructor must return exactly one value, got %d", ft.NumOut())
		}
		outType := ft.Out(0)
		if ft.NumIn() != 0 {
			return fmt.Errorf("di: constructor for %v must take no parameters, got %d; resolve dependencies inside it", outType, ft.NumIn())
		}
		if err := c.refuseResolvedLocked(outType); err != nil {
			return err
		}
		c.providers[outType] = constructor
		return nil
	}

	outType := v.Type()
	if err := c.refuseResolvedLocked(outType); err != nil {
		return err
	}
	c.providers[outType] = constructor
	c.singletons[outType] = constructor
	c.resolved[outType] = true
	return nil
}

func (c *Container) refuseResolvedLocked(t reflect.Type) error {
	if c.building[t] != nil {
		return fmt.Errorf("di: %v is being built; Provide each type before its first use", t)
	}
	if c.resolved[t] {
		return fmt.Errorf("di: %v already has a value; Provide each type once, before its first use", t)
	}
	return nil
}

// Resolve sets *target to the singleton for its type; target must be a non-nil pointer.
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
		return fmt.Errorf("di: provider for %v returned a nil interface, cannot resolve it", targetType)
	case err != nil:
		return err
	}
	tv.Elem().Set(reflect.ValueOf(v))
	return nil
}

// InjectTagCount reports how many inject-tagged fields Inject would fill in t.
func InjectTagCount(t reflect.Type) int {
	return injectTagCount(t, map[reflect.Type]bool{})
}

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

// Inject fills the `inject:""` fields of the struct target points to,
// embedded structs included.
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
		if !ev.Field(i).CanSet() {
			return fmt.Errorf("di: injected field %s of type %v is not settable — inject-tagged fields must be exported", field.Name, fieldType)
		}
		v, ok, err := c.singleton(fieldType)
		switch {
		case !ok:
			return fmt.Errorf("di: no provider registered for injected field %s of type %v", field.Name, fieldType)
		case errors.Is(err, errNilSingleton):
			return fmt.Errorf("di: provider for injected field %s of type %v returned a nil interface, cannot inject", field.Name, fieldType)
		case err != nil:
			return err
		}
		ev.Field(i).Set(reflect.ValueOf(v))
	}
	return nil
}

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
