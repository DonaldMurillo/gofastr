//go:build linux && (amd64 || arm64)

package linux

import (
	"errors"
	"github.com/DonaldMurillo/gofastr/battery/desktop/internal/ffi"
	"github.com/DonaldMurillo/gofastr/battery/desktop/internal/gtk"
	"runtime"
	"sync"
	"unsafe"
)

var trayGIO struct {
	sync.Once
	symbols map[string]uintptr
	err     error
}

func loadTrayGIO() error {
	trayGIO.Do(func() {
		if err := loadActivationGIO(); err != nil {
			trayGIO.err = err
			return
		}
		// Copy rather than mutate activation's live, frozen map.
		trayGIO.symbols = make(map[string]uintptr, len(activationGIO.symbols)+20)
		for n, p := range activationGIO.symbols {
			trayGIO.symbols[n] = p
		}
		for lib, names := range map[string][]string{
			"libglib-2.0.so.0":    {"g_variant_new_int32", "g_variant_new_boolean", "g_variant_new_object_path", "g_variant_new_array", "g_variant_new_dict_entry", "g_variant_new_variant", "g_variant_new_fixed_array", "g_variant_get_variant", "g_variant_get_int32", "g_variant_get_boolean", "g_variant_n_children", "g_variant_get_size", "g_variant_is_of_type"},
			"libgobject-2.0.so.0": {"g_value_get_string", "g_value_take_variant"},
			"libgio-2.0.so.0":     {"g_dbus_connection_get_unique_name", "g_dbus_connection_emit_signal"},
			"libgtk-3.so.0":       {"gtk_widget_hide", "gtk_widget_show"},
		} {
			h, err := gtk.DlopenGlobal(lib)
			if err != nil {
				trayGIO.err = err
				return
			}
			for _, n := range names {
				p, err := gtk.Dlsym(h, n)
				if err != nil {
					trayGIO.err = err
					return
				}
				trayGIO.symbols[n] = p
			}
		}
	})
	return trayGIO.err
}

// Keep out-parameters and arrays stable across callbacks from native code.
//
//go:uintptrescapes
func trayCall(name string, args ...uintptr) uintptr {
	r, _, _ := ffi.Call(trayGIO.symbols[name], args, nil)
	return r
}
func trayString(v string) uintptr {
	b, p := activationCString(v)
	r := trayCall("g_variant_new_string", p)
	runtime.KeepAlive(b)
	return r
}
func trayObjectPath(v string) uintptr {
	b, p := activationCString(v)
	r := trayCall("g_variant_new_object_path", p)
	runtime.KeepAlive(b)
	return r
}
func trayType(v string) uintptr {
	b, p := activationCString(v)
	r := trayCall("g_variant_type_new", p)
	runtime.KeepAlive(b)
	return r
}
func trayInt(v int32) uintptr { return trayCall("g_variant_new_int32", uintptr(uint32(v))) }
func trayBool(v bool) uintptr { return trayCall("g_variant_new_boolean", boolean(v)) }
func trayTuple(children ...uintptr) uintptr {
	if len(children) == 0 {
		return trayCall("g_variant_new_tuple", 0, 0)
	}
	r := trayCall("g_variant_new_tuple", uintptr(unsafe.Pointer(&children[0])), uintptr(len(children)))
	runtime.KeepAlive(children)
	return r
}
func trayArray(t string, children ...uintptr) uintptr {
	typ := trayType(t)
	defer trayCall("g_variant_type_free", typ)
	if len(children) == 0 {
		return trayCall("g_variant_new_array", typ, 0, 0)
	}
	r := trayCall("g_variant_new_array", typ, uintptr(unsafe.Pointer(&children[0])), uintptr(len(children)))
	runtime.KeepAlive(children)
	return r
}
func trayBytes(data []byte) uintptr {
	t := trayType("y")
	defer trayCall("g_variant_type_free", t)
	if len(data) == 0 {
		return trayCall("g_variant_new_fixed_array", t, 0, 0, 1)
	}
	r := trayCall("g_variant_new_fixed_array", t, uintptr(unsafe.Pointer(&data[0])), uintptr(len(data)), 1)
	runtime.KeepAlive(data)
	return r
}
func trayDictEntry(k string, v uintptr) uintptr {
	return trayCall("g_variant_new_dict_entry", trayString(k), trayCall("g_variant_new_variant", v))
}
func trayUnref(v uintptr) {
	if v != 0 {
		trayCall("g_variant_unref", v)
	}
}
func trayChild(v uintptr, i int) uintptr { return trayCall("g_variant_get_child_value", v, uintptr(i)) }
func trayReadString(v uintptr, limit int) (string, bool) {
	var n uintptr
	p := trayCall("g_variant_get_string", v, uintptr(unsafe.Pointer(&n)))
	if p == 0 || n > uintptr(limit) {
		return "", false
	}
	return string(unsafe.Slice((*byte)(nativePointer(p)), int(n))), true
}
func trayValueString(values uintptr, index int) string {
	p := trayCall("g_value_get_string", values+uintptr(index)*activationGValueSize)
	s, ok := readBoundedString(p, trayTextLimit+1)
	if !ok {
		return ""
	}
	return s
}
func trayNativeError(op string, p uintptr) error {
	if p != 0 {
		trayCall("g_error_free", p)
	}
	return errors.New("desktop/linux: " + op + " failed")
}
