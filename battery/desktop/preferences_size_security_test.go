package desktop

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/battery/desktop/appstate"
)

func TestPreferenceEncodedSizeLimit(t *testing.T) {
	for name, text := range map[string]string{
		"plain":   strings.Repeat("x", appstate.MaxValueBytes),
		"escaped": strings.Repeat("<", appstate.MaxValueBytes/6),
		"unicode": strings.Repeat("界", appstate.MaxValueBytes/3),
	} {
		t.Run(name, func(t *testing.T) {
			battery := newPrefsBattery(t)
			withStore(t, battery)
			preferences := battery.Preferences()
			if err := preferences.Set("export_folder", "kept"); err != nil {
				t.Fatal(err)
			}
			err := preferences.Set("export_folder", text)
			var refusal *preferenceError
			if !errors.As(err, &refusal) || refusal.key != "export_folder" {
				t.Fatalf("oversized preference returned %T %v, want a keyed validation refusal", err, err)
			}
			if preferences.String("export_folder") != "kept" {
				t.Fatal("refused value changed storage")
			}
		})
	}
	battery := newPrefsBattery(t)
	withStore(t, battery)
	boundary := strings.Repeat("x", appstate.MaxValueBytes-len(`{"export_folder":""}`))
	if err := battery.Preferences().Set("export_folder", boundary); err != nil {
		t.Fatalf("exact encoded boundary refused: %v", err)
	}
}

func TestPreferenceSizeBatchIsAtomic(t *testing.T) {
	battery := New(Config{ID: "prefs.size.test", Preferences: []Preference{
		{Key: "alpha", Label: testPhrase("Alpha"), Kind: PreferenceString, Default: ""},
		{Key: "omega", Label: testPhrase("Omega"), Kind: PreferenceString, Default: ""},
	}})
	store := withStore(t, battery)
	preferences := battery.Preferences()
	if err := store.Set(settingsStateKey, map[string]string{"legacy": "retained"}); err != nil {
		t.Fatal(err)
	}
	apply := func(alpha, omega string) error {
		t.Helper()
		alphaJSON, _ := json.Marshal(alpha)
		omegaJSON, _ := json.Marshal(omega)
		_, err := preferences.setRaw(map[string]json.RawMessage{"alpha": alphaJSON, "omega": omegaJSON})
		return err
	}
	if err := apply(strings.Repeat("a", 40<<10), strings.Repeat("b", 20<<10)); err != nil {
		t.Fatal(err)
	}
	if err := apply(strings.Repeat("c", 60<<10), ""); err != nil {
		t.Fatalf("a batch shrinking the later key should fit: %v", err)
	}
	err := apply(strings.Repeat("d", 40<<10), strings.Repeat("e", 40<<10))
	var refusal *preferenceError
	if !errors.As(err, &refusal) || refusal.key != "omega" {
		t.Fatalf("aggregate overflow returned %T %v, want omega validation refusal", err, err)
	}
	if preferences.String("alpha") != strings.Repeat("c", 60<<10) || preferences.String("omega") != "" {
		t.Fatal("refused batch changed a preference")
	}
	var stored map[string]string
	if _, err := store.Get(settingsStateKey, &stored); err != nil || stored["legacy"] != "retained" {
		t.Fatalf("undeclared stored key lost: %v", err)
	}
}
