package entityui

import (
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// gadgetUI is a record whose stored values are shapes a native control
// cannot hold: a NULL optional bool, colors that are not a lowercase
// #rrggbb, and a timestamp with milliseconds.
func gadgetUI(t *testing.T) *testUI {
	t.Helper()
	cfg := entity.EntityConfig{
		Table: "gadgets",
		Fields: []schema.Field{
			{Name: "name", Type: schema.String, Required: true},
			{Name: "flag", Type: schema.Bool},
			{Name: "live", Type: schema.Bool},
			{Name: "hue", Type: schema.String},
			{Name: "tint", Type: schema.String},
			{Name: "shade", Type: schema.String},
			{Name: "paint", Type: schema.String},
			{Name: "at", Type: schema.Timestamp},
			{Name: "sec", Type: schema.Timestamp},
		},
		Display: &entity.DisplayConfig{Fields: map[string]entity.FieldDisplay{
			"hue": {Input: "color"}, "tint": {Input: "color"},
			"shade": {Input: "color"}, "paint": {Input: "color"},
		}},
	}
	return newTestUI(t, map[string]entity.EntityConfig{"gadgets": cfg.WithTimestamps(false)},
		map[string][]map[string]any{"gadgets": {{
			"id": "g-1", "name": "Gadget", "live": true,
			"tint": "brand-red", "shade": "#A1B2C3", "paint": "#a1b2c3",
			"at": "2026-10-27T10:00:00.531000Z", "sec": "2026-10-27T10:00:30.000000Z",
		}}},
		withAPI(map[string]string{"gadgets": "/api/gadgets"}))
}

func renderGadget(t *testing.T, x *testUI) string {
	t.Helper()
	return string(x.ui.Record("gadgets", "g-1").Base("/rec/gadgets").
		RenderCtx(x.userCtx("/rec/gadgets/g-1", "", "u1")))
}

func storedGadget(t *testing.T, x *testUI) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, c := range []string{"flag", "live", "hue", "tint", "shade", "paint", "at", "sec"} {
		var v any
		if err := x.db.QueryRow(`SELECT ` + c + ` FROM gadgets WHERE id = 'g-1'`).Scan(&v); err != nil {
			t.Fatal(err)
		}
		if b, ok := v.([]byte); ok {
			v = string(b)
		}
		out[c] = fmt.Sprint(v)
	}
	return out
}

// An untouched save of a record holding those shapes writes back what
// is stored. A NULL optional bool drew a switch with a hidden "false"
// and saved false; a color input turned "brand-red" and "#A1B2C3" into
// "#000000" and "#a1b2c3"; a timestamp drew to the second and lost its
// 531 ms.
func TestUntouchedSaveKeepsStoredShapes(t *testing.T) {
	x := gadgetUI(t)
	before := storedGadget(t, x)
	fields := formFields(renderGadget(t, x), "eui-gadgets-form")
	putRecord(t, x, "gadgets", "g-1", fields)
	after := storedGadget(t, x)
	for _, c := range []string{"flag", "live", "tint", "shade", "paint", "at", "sec"} {
		if after[c] != before[c] {
			t.Errorf("%s = %q after an untouched save, want %q (sent %q)", c, after[c], before[c], fields[c])
		}
	}
	// A NULL string stores "" from any text input; it must not become a
	// color nobody picked.
	if after["hue"] == "#000000" {
		t.Errorf("the unset color saved as black")
	}
}

// The unset bool's control offers the unset choice selected, and yes
// and no; a set bool keeps its switch.
func TestNullBoolDrawsUnsetChoice(t *testing.T) {
	x := gadgetUI(t)
	body := renderGadget(t, x)
	sel := regexp.MustCompile(`(?s)<select[^>]*name="flag".*?</select>`).FindString(body)
	for _, want := range []string{`value="true"`, `value="false"`} {
		if !strings.Contains(sel, want) {
			t.Fatalf("the unset bool's control lacks %s:\n%s", want, sel)
		}
	}
	if opt := regexp.MustCompile(`<option[^>]*value=""[^>]*>`).FindString(sel); opt == "" || !strings.Contains(opt, "selected") || strings.Contains(opt, "disabled") {
		t.Fatalf("the unset bool's control does not hold the unset choice:\n%s", sel)
	}
	if !regexp.MustCompile(`<input[^>]*name="live"[^>]*type="checkbox"|<input[^>]*type="checkbox"[^>]*name="live"`).MatchString(body) {
		t.Fatalf("the set bool lost its switch:\n%s", body)
	}
	fields := formFields(body, "eui-gadgets-form")
	fields["flag"] = "true"
	putRecord(t, x, "gadgets", "g-1", fields)
	if got := storedGadget(t, x)["flag"]; got != "1" && got != "true" {
		t.Fatalf("flag = %q after picking yes", got)
	}
}

// A color the picker can hold draws the picker; any other stored text
// draws a text input that keeps it, and an empty one stays empty.
func TestColorPickerOnlyForSimpleColor(t *testing.T) {
	x := gadgetUI(t)
	body := renderGadget(t, x)
	if tag := tagFor(t, body, "paint"); !strings.Contains(tag, `type="color"`) {
		t.Errorf("paint #a1b2c3 lost its picker: %s", tag)
	}
	for _, name := range []string{"hue", "tint", "shade"} {
		if tag := tagFor(t, body, name); strings.Contains(tag, `type="color"`) {
			t.Errorf("%s draws a picker that rewrites its value: %s", name, tag)
		}
	}
	for s, want := range map[string]bool{
		"#a1b2c3": true, "#09afaf": true, "xa1b2c3": false, "#A1B2C3": false,
		"#a1b2c": false, "#a1b2c3d": false, "#a1b2cg": false, "": false,
	} {
		if simpleColor(s) != want {
			t.Errorf("simpleColor(%q) = %v, want %v", s, !want, want)
		}
	}
	create := string(x.ui.Create("gadgets").Base("/rec/gadgets").RenderCtx(x.userCtx("/rec/gadgets/create", "", "u1")))
	if got := formFields(create, "eui-gadgets-form")["hue"]; got != "" {
		t.Errorf("an empty optional color submits %q on create, want empty", got)
	}
}

// The timestamp input carries the milliseconds and a step that admits
// them; whole seconds keep the one-second step.
func TestTimestampKeepsMilliseconds(t *testing.T) {
	x := gadgetUI(t)
	body := renderGadget(t, x)
	at := tagFor(t, body, "at")
	if !strings.Contains(at, `value="2026-10-27T10:00:00.531"`) || !strings.Contains(at, `step="0.001"`) {
		t.Errorf("at = %s, want the milliseconds and step 0.001", at)
	}
	if sec := tagFor(t, body, "sec"); !strings.Contains(sec, `value="2026-10-27T10:00:30"`) || !strings.Contains(sec, `step="1"`) {
		t.Errorf("sec = %s, want whole seconds and step 1", sec)
	}
	// Milliseconds are the finest the input holds; a longer fraction
	// would make the browser blank the value, so it is cut to them.
	f := schema.Field{Name: "at", Type: schema.Timestamp}
	for in, want := range map[string]string{
		"2026-10-27T10:00:00.531250Z": "2026-10-27T10:00:00.531",
		"2026-10-27T10:00:00.000250Z": "2026-10-27T10:00",
		"2026-10-27T10:00:07.500000Z": "2026-10-27T10:00:07.500",
	} {
		if got := timestampInputValue(formValueText(f, in)); got != want {
			t.Errorf("input value for %s = %q, want %q", in, got, want)
		}
	}
}
