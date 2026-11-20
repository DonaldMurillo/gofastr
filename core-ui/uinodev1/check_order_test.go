package uinodev1

import (
	"strings"
	"testing"
)

// Each refusal is the named check's, not a later layer's.
func TestEachCapRefusesByName(t *testing.T) {
	for _, c := range []struct {
		name, json, want string
		lim              Limits
	}{
		{"input bytes", `{"component":"text","props":{"text":"` + strings.Repeat("x", 2000) + `"}}`, "input bytes", Limits{MaxInputBytes: 1024}},
		{"null child", `{"component":"stack","props":{},"children":[null]}`, "null node", DefaultLimits()},
		{"trailing object", `{"component":"divider"}{}`, "trailing", DefaultLimits()},
		{"trailing scalar", `{"component":"divider"} 42`, "trailing", DefaultLimits()},
		{"fat cell", `{"component":"data-table","props":{"columns":[{"key":"k","label":"K"}],"rows":[{"cells":[{"text":"` + strings.Repeat("x", 64) + `"}]}]}}`, "too long", Limits{MaxPropString: 32}},
		{"long action_ref", `{"component":"button","props":{"label":"x"},"action_ref":"` + strings.Repeat("a", 200) + `"}`, "action_ref length", DefaultLimits()},
		{"action_ref with a space", `{"component":"button","props":{"label":"x"},"action_ref":"a b"}`, "whitespace", DefaultLimits()},
		{"badge tone", `{"component":"badge","props":{"text":"x","tone":"evil"}}`, "enum", DefaultLimits()},
		{"stack align", `{"component":"stack","props":{"align":"evil"}}`, "enum", DefaultLimits()},
		{"card elevation", `{"component":"card","props":{"elevation":"evil"}}`, "enum", DefaultLimits()},
		{"button variant", `{"component":"button","props":{"label":"x","variant":"evil"},"action_ref":"a"}`, "enum", DefaultLimits()},
	} {
		_, err := Validate([]byte(c.json), c.lim)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: err = %v, want one naming %q", c.name, err, c.want)
		}
	}
}

func TestErrorsEchoABoundedInput(t *testing.T) {
	_, err := Validate([]byte(`{"component":"`+strings.Repeat("z", 5000)+`"}`), DefaultLimits())
	if err == nil || len(err.Error()) > 300 || !strings.Contains(err.Error(), "truncated") {
		t.Fatalf("err = %.400v", err)
	}
}
