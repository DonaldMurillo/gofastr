package widget

import (
	"reflect"
	"testing"
)

// The widget API carried dead surface: Bootstrap/BootstrapMode/BootstrapPath
// (Mount never applied the documented default; the catalog never emitted them)
// and RPCEndpoint.ResponseSignal (written by the RPCWithSignal builder, never
// read — the runtime reads data-fui-rpc-signal off the DOM, not this field).
// Zero-carryover policy: the symbols must be GONE, not deprecated. This test
// fails (red) while any of them still exist and passes once they are deleted.
// The BootstrapMode type + AutoScript/Embedded consts are deleted alongside
// (the package no longer building if they linger is its own enforcement).

func TestDeadWidgetAPIIsRemoved(t *testing.T) {
	builderMethods := []string{"Bootstrap", "RPCWithSignal"}
	bt := reflect.TypeFor[*Builder]()
	for _, name := range builderMethods {
		if m, ok := bt.MethodByName(name); ok {
			t.Errorf("dead method %s still present on *Builder — delete it (zero-carryover)", m.Name)
		}
	}

	defFields := []string{"Bootstrap", "BootstrapPath"}
	dt := reflect.TypeFor[Definition]()
	for _, name := range defFields {
		if f, ok := dt.FieldByName(name); ok {
			t.Errorf("dead field %s still present on Definition — delete it (zero-carryover)", f.Name)
		}
	}

	if _, ok := reflect.TypeFor[RPCEndpoint]().FieldByName("ResponseSignal"); ok {
		t.Error("dead field ResponseSignal still present on RPCEndpoint — delete it (zero-carryover)")
	}
}
