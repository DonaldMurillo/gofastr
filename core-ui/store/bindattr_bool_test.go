package store

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// A boolean attribute is on whenever it is present, so BindAttr's SSR
// stamp of disabled="false" shipped a disabled control until the runtime
// ran. A false value omits the attribute; a true one writes it with an
// empty value, matching the runtime's attr mode.
func TestBindAttrBooleanOmitsFalse(t *testing.T) {
	ctx := context.Background()
	for _, attr := range []string{"disabled", "hidden", "checked", "open", "selected", "Disabled"} {
		resetForTest()
		st := New("t")
		off := string(st.Bool("off", false).BindAttr(ctx, "input", attr, map[string]string{attr: ""}))
		if strings.Contains(strings.ToLower(off), " "+strings.ToLower(attr)+"=") {
			t.Errorf("%s false: attribute present: %s", attr, off)
		}
		on := string(st.Bool("on", true).BindAttr(ctx, "input", attr, nil))
		if !strings.Contains(on, " "+attr+`=""`) {
			t.Errorf("%s true: want %s=\"\": %s", attr, attr, on)
		}
		for name, v := range map[string]string{"empty": "", "false": "false"} {
			s := string(st.String("s"+name, v).BindAttr(ctx, "input", attr, nil))
			if strings.Contains(strings.ToLower(s), " "+strings.ToLower(attr)+"=") {
				t.Errorf("%s=%q: attribute present: %s", attr, v, s)
			}
		}
		if z := string(st.Int("zero", 0).BindAttr(ctx, "input", attr, nil)); strings.Contains(strings.ToLower(z), " "+strings.ToLower(attr)+"=") {
			t.Errorf("%s=0: attribute present: %s", attr, z)
		}
	}
	// A non-boolean attribute keeps its stringified value.
	resetForTest()
	if v := string(New("t").Bool("b", false).BindAttr(ctx, "div", "data-state", nil)); !strings.Contains(v, `data-state="false"`) {
		t.Errorf("data-state false: %s", v)
	}
}

type lockedFlag bool

// booleanAttrOn decides on the JSON the client receives, so every Go type
// that encodes to false, null, "", "false" or a zero number is off
// (2026-11-01 review: uint, int32, float32 and a named bool fell through
// to on, and the control arrived hidden or disabled).
func TestBooleanAttrOnFollowsJSON(t *testing.T) {
	off := []any{nil, false, "", "false", 0, int32(0), uint(0), uint8(0), float32(0), 0.0, lockedFlag(false), json.Number("0"), json.Number("1e-400")}
	for _, v := range off {
		if booleanAttrOn(v) {
			t.Errorf("%T(%v) reads on, want off", v, v)
		}
	}
	on := []any{true, "x", "0", 1, uint(3), float32(0.5), lockedFlag(true), json.Number("2"), json.Number("1e400"), json.Number("-1e400"), []int{}, map[string]int{}}
	for _, v := range on {
		if !booleanAttrOn(v) {
			t.Errorf("%T(%v) reads off, want on", v, v)
		}
	}
}
