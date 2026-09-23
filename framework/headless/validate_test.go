package headless

import (
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
)

// The shared refusal helpers: each refusal is configuration the caller
// typed, and each one proven here is one a primitive renders through
// the helper, so the primitive's own tests assert its behaviour and
// these assert the refusal itself.

func TestCheckSelectorRefusesBrokenValues(t *testing.T) {
	for _, tc := range []struct {
		name string
		sel  string
	}{
		{"empty", ""},
		{"control bytes", "#a\r\nb"},
		{"markup opener", "#a < b"},
	} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("%s: checkSelector accepted %q", tc.name, tc.sel)
				}
			}()
			checkSelector("Rail", "ObserveSelector", tc.sel)
		}()
	}
	// A selector with characters a selector legitimately carries —
	// combinators, attribute tests — passes: the module's try/catch
	// owns malformed-input behaviour beyond this refusal.
	checkSelector("Rail", "ObserveSelector", `main section[id]:not([data-skip])`)
}

func TestCheckFragmentIDRefusesBrokenValues(t *testing.T) {
	for _, tc := range []struct {
		name string
		id   string
	}{
		{"empty", ""},
		{"control bytes", "ov\rerview"},
		{"selector shape", "#overview"},
		{"markup", "a<b"},
		{"quoted", `"a"`},
		{"spaced", "two words"},
	} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("%s: checkFragmentID accepted %q", tc.name, tc.id)
				}
			}()
			checkFragmentID("Rail", "Anchor", tc.id)
		}()
	}
	checkFragmentID("Rail", "Anchor", "overview-2")
}

func TestCheckStorageKeyRefusesBrokenValues(t *testing.T) {
	for _, key := range []string{"", " ", "\r\n"} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("checkStorageKey accepted %q", key)
				}
			}()
			checkStorageKey("Disclosure", key)
		}()
	}
	checkStorageKey("Disclosure", "nav.section.open")
}

func TestCheckEnumRefusesValuesOutsideTheSet(t *testing.T) {
	func() {
		defer func() {
			if recover() == nil {
				t.Error("checkEnum accepted a value outside the set")
			}
		}()
		checkEnum("Menu", "Position", "diagonal", "bottom-start", "bottom-end")
	}()
	checkEnum("Menu", "Position", "bottom-start", "bottom-start", "bottom-end")
}

func TestCheckNoDuplicateIDsRefusesARepeatedID(t *testing.T) {
	func() {
		defer func() {
			if recover() == nil {
				t.Error("checkNoDuplicateIDs accepted a repeated id")
			}
		}()
		checkNoDuplicateIDs("Menu", []string{"a", "b", "a"})
	}()
	checkNoDuplicateIDs("Menu", []string{"a", "b", "c"})
}

func TestCheckLabelRefusesWhitespaceOnlyLabels(t *testing.T) {
	for _, label := range []string{"", " ", "\t\n "} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("checkLabel accepted %q", label)
				}
			}()
			checkLabel("Rail", "Label", label)
		}()
	}
	// A label with edges — punctuation, non-breaking shape, control
	// bytes the render path scrubs — is a label; only nothing-but-
	// whitespace is refused here.
	checkLabel("Rail", "Label", "Getting started")
	checkLabel("Rail", "Label", "Sea\rch")
}

// The on* family is refused by Safe itself, not only by the render
// layer that would drop it anyway: the refusal is this package's
// contract (the same posture as ui.scrubAttrs and kiln/world), and a
// caller reading Safe's output — not just the bytes it rendered —
// must never see a handler it did not write survive the fold.
func TestSafeRefusesInlineHandlerKeys(t *testing.T) {
	got := Safe(html.Attrs{
		"onclick": "alert(1)",
		"ONLOAD":  "alert(2)",
		"on":      "bare",
		"data-ok": "kept",
	})
	if len(got) != 1 || got["data-ok"] != "kept" {
		t.Errorf("Safe kept refused handler keys or dropped the ordinary one: %v", got)
	}
}
