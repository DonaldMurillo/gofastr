package style

import (
	"reflect"
	"sync"
)

var optionalSetDefaults = sync.OnceValue(func() map[reflect.Type]reflect.Value {
	d := DefaultTheme()
	return map[reflect.Type]reflect.Value{
		reflect.TypeFor[StrokeSet]():   reflect.ValueOf(d.Strokes),
		reflect.TypeFor[LeadingSet]():  reflect.ValueOf(d.Leading),
		reflect.TypeFor[TrackingSet](): reflect.ValueOf(d.Tracking),
		reflect.TypeFor[OpacitySet]():  reflect.ValueOf(d.Opacities),
	}
})

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

func fillOptionalDefaults(t *Theme) {
	v := reflect.ValueOf(t).Elem()
	for i := range v.NumField() {
		if f := v.Field(i); f.CanSet() {
			f.Set(withOptionalDefaults(f))
		}
	}
}
