package style

import (
	"reflect"
	"sync"
)

// optionalSetDefaults maps each optional token set's type to the default
// theme's value for it. A theme that leaves a slot of such a set fully
// unset (a theme.go written before the set existed) emits, lists and
// edits the default value for that slot, so every reader of the token
// resolves: kit CSS, the style.Use utilities, owned sheets, host CSS.
var optionalSetDefaults = sync.OnceValue(func() map[reflect.Type]reflect.Value {
	d := DefaultTheme()
	return map[reflect.Type]reflect.Value{
		reflect.TypeFor[StrokeSet]():   reflect.ValueOf(d.Strokes),
		reflect.TypeFor[LeadingSet]():  reflect.ValueOf(d.Leading),
		reflect.TypeFor[TrackingSet](): reflect.ValueOf(d.Tracking),
		reflect.TypeFor[OpacitySet]():  reflect.ValueOf(d.Opacities),
	}
})

// withOptionalDefaults returns v with every fully unset slot of an
// optional set taken from the default theme. Any other value comes back
// unchanged.
func withOptionalDefaults(v reflect.Value) reflect.Value {
	def, ok := optionalSetDefaults()[v.Type()]
	if !ok {
		return v
	}
	out := reflect.New(v.Type()).Elem()
	out.Set(v)
	for i := range out.NumField() {
		f := out.Field(i)
		if !f.IsZero() {
			continue
		}
		if !f.CanSet() {
			panic("style: optional token set " + v.Type().Name() + " has an unexported slot " + v.Type().Field(i).Name)
		}
		f.Set(def.Field(i))
	}
	return out
}

// fillOptionalDefaults writes the default theme's value into every fully
// unset slot of t's optional sets.
func fillOptionalDefaults(t *Theme) {
	v := reflect.ValueOf(t).Elem()
	for i := range v.NumField() {
		if f := v.Field(i); f.CanSet() {
			f.Set(withOptionalDefaults(f))
		}
	}
}
