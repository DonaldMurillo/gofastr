package theme

import (
	"reflect"
	"testing"
)

// The typed component options: merging semantics (zero = unspecified,
// explicit = reset), completeness of Default, and the round trip
// through the flattened map style.Theme.Components carries.

func TestDefaultCarriesCompleteOptions(t *testing.T) {
	th := Default()
	want := map[string]string{
		"density":          "comfortable",
		"button.treatment": "filled",
		"button.radius":    "round",
	}
	if !reflect.DeepEqual(th.Components, want) {
		t.Fatalf("Default() Components = %#v, want %#v", th.Components, want)
	}
}

func TestComponentOptionsOverrideAndReset(t *testing.T) {
	dense := Default(Overrides{Components: ComponentOptions{Density: Compact}})
	if dense.Components["density"] != "compact" {
		t.Fatalf("compact override lost: %#v", dense.Components)
	}
	// A later explicit Comfortable RESETS the earlier override — zero
	// means unspecified, not "keep the previous override".
	reset := Default(
		Overrides{Components: ComponentOptions{Density: Compact, Button: ButtonOptions{Treatment: Outline, Radius: Pill}}},
		Overrides{Components: ComponentOptions{Density: Comfortable}},
	)
	if reset.Components["density"] != "comfortable" {
		t.Errorf("explicit Comfortable did not reset Compact: %#v", reset.Components)
	}
	// Options the second override left unset keep the first override's
	// values; the result stays complete.
	if reset.Components["button.treatment"] != "outline" || reset.Components["button.radius"] != "pill" {
		t.Errorf("unset options dropped the earlier override: %#v", reset.Components)
	}
	for _, k := range []string{"density", "button.treatment", "button.radius"} {
		if _, ok := reset.Components[k]; !ok {
			t.Errorf("merged Components missing %q: completeness is what makes scopes nest", k)
		}
	}
}

func TestComponentOptionsStringParseRoundTrip(t *testing.T) {
	for _, d := range []Density{Comfortable, Compact} {
		back, err := ParseDensity(d.String())
		if err != nil || back != d {
			t.Errorf("Density round trip failed: %v → %q → %v (%v)", d, d.String(), back, err)
		}
	}
	for _, tr := range []ButtonTreatment{Filled, Outline, Soft} {
		back, err := ParseButtonTreatment(tr.String())
		if err != nil || back != tr {
			t.Errorf("Treatment round trip failed: %v → %q → %v (%v)", tr, tr.String(), back, err)
		}
	}
	for _, r := range []ButtonRadius{Round, Square, Pill} {
		back, err := ParseButtonRadius(r.String())
		if err != nil || back != r {
			t.Errorf("Radius round trip failed: %v → %q → %v (%v)", r, r.String(), back, err)
		}
	}
	if (DensityUnset).String() != "" || (TreatmentUnset).String() != "" || (RadiusUnset).String() != "" {
		t.Error("unset enums must flatten to the empty string (the key is omitted)")
	}
}

func TestOptionsFromFlattenedRejectsUnknowns(t *testing.T) {
	if _, err := OptionsFromFlattened(map[string]string{"density": "cozy"}); err == nil {
		t.Error("unknown density value accepted")
	}
	if _, err := OptionsFromFlattened(map[string]string{"field.density": "compact"}); err == nil {
		t.Error("unknown option key accepted: the vocabulary grows with its family's change, not by ignoring data")
	}
	o, err := OptionsFromFlattened(DefaultOptions.Flattened())
	if err != nil || o != DefaultOptions {
		t.Errorf("Flattened/OptionsFromFlattened are not inverses: %#v (%v)", o, err)
	}
}

func TestCompleteFillsUnset(t *testing.T) {
	got := ComponentOptions{Button: ButtonOptions{Radius: Pill}}.Complete()
	if got != (ComponentOptions{Density: Comfortable, Button: ButtonOptions{Treatment: Filled, Radius: Pill}}) {
		t.Errorf("Complete() = %#v", got)
	}
}

// An unknown VALUE in Components fails style.Theme.Validate: the
// grammar check is the theme-shape gate every host runs at boot. (A
// grammatical non-member like "cozy" is refused at Validate where the
// compiler is registered — this package's tests link no compiler — and
// at emit everywhere else; both tests live in framework/ui, where the
// compiler is.)
func TestUnknownComponentValueFailsValidate(t *testing.T) {
	th := Default()
	th.Components["density"] = "Compact" // uppercase: not [a-z][a-z0-9-]*
	if err := th.Validate(); err == nil {
		t.Error("uppercase component value passed Theme.Validate")
	}
}
