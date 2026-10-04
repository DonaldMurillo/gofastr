package html

import "testing"

func TestSafeExtraAttrsDropsProtectedKeys(t *testing.T) {
	got := SafeExtraAttrs(Attrs{
		"class":       "evil",
		"Class":       "evil-case-variant",
		"id":          "evil",
		"ID":          "evil-case-variant",
		"Type":        "hidden",
		"data-test":   "hook",
		"aria-label":  "override",
		"data-cui-on": "spoof",
		"DATA-CUI-X":  "spoof-case-variant",
	}, "type")

	want := Attrs{"data-test": "hook", "aria-label": "override"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("key %q: got %q, want %q", k, got[k], v)
		}
	}
}

func TestSafeCarrierAttrsKeepsWiring(t *testing.T) {
	got := SafeCarrierAttrs(Attrs{
		"class":            "evil",
		"ID":               "evil-case-variant",
		"Type":             "hidden",
		"data-cui-comp":    "spoofed-style-scope",
		"data-test":        "hook",
		"data-cui-rpc":     "/api/items/42",
		"data-cui-confirm": "Delete?",
	}, "type")

	want := Attrs{
		"data-test":        "hook",
		"data-cui-rpc":     "/api/items/42",
		"data-cui-confirm": "Delete?",
	}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("key %q: got %q, want %q", k, got[k], v)
		}
	}
}

func TestSafeCarrierAttrsNilOnEmptyResult(t *testing.T) {
	if got := SafeCarrierAttrs(nil); got != nil {
		t.Errorf("nil input: got %v, want nil", got)
	}
	if got := SafeCarrierAttrs(Attrs{"class": "x", "Name": "y"}, "name"); got != nil {
		t.Errorf("all-dropped input: got %v, want nil", got)
	}
}

func TestSafeExtraAttrsNilOnEmptyResult(t *testing.T) {
	if got := SafeExtraAttrs(nil); got != nil {
		t.Errorf("nil input: got %v, want nil", got)
	}
	if got := SafeExtraAttrs(Attrs{"class": "x"}); got != nil {
		t.Errorf("all-dropped input: got %v, want nil", got)
	}
}
