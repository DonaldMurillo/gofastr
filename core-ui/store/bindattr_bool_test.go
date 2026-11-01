package store

import (
	"context"
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
