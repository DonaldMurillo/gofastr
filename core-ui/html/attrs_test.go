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
		"data-fui-on": "spoof",
		"DATA-FUI-X":  "spoof-case-variant",
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

func TestSafeExtraAttrsNilOnEmptyResult(t *testing.T) {
	if got := SafeExtraAttrs(nil); got != nil {
		t.Errorf("nil input: got %v, want nil", got)
	}
	if got := SafeExtraAttrs(Attrs{"class": "x"}); got != nil {
		t.Errorf("all-dropped input: got %v, want nil", got)
	}
}
