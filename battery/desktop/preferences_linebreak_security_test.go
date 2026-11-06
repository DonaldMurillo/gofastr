package desktop

import (
	"encoding/json"
	"errors"
	"os"
	"testing"
)

func TestStringWritesRefuseLineBreaks(t *testing.T) {
	for name, text := range map[string]string{"lf": "north\nsouth", "cr": "left\rright", "crlf": "one\r\ntwo", "edges": "\rstart\n"} {
		t.Run(name, func(t *testing.T) {
			battery := newPrefsBattery(t)
			store := withStore(t, battery)
			preferences := battery.Preferences()
			if err := preferences.Set("export_folder", "kept"); err != nil {
				t.Fatal(err)
			}
			if err := store.Flush(); err != nil {
				t.Fatal(err)
			}
			before, err := os.ReadFile(store.Path())
			if err != nil {
				t.Fatal(err)
			}
			var refusal *preferenceError
			if err := preferences.Set("export_folder", text); !errors.As(err, &refusal) || refusal.key != "export_folder" {
				t.Errorf("Set accepted line breaks or lost the key: %v", err)
			}
			raw, _ := json.Marshal(text)
			if _, err := preferences.setRaw(map[string]json.RawMessage{"export_folder": raw, "work_minutes": json.RawMessage(`40`)}); !errors.As(err, &refusal) || refusal.key != "export_folder" {
				t.Errorf("batch accepted line breaks or lost the key: %v", err)
			}
			if preferences.String("export_folder") != "kept" || preferences.Int("work_minutes") != 25 {
				t.Error("refused writes changed preferences")
			}
			if err := store.Flush(); err != nil {
				t.Fatal(err)
			}
			after, err := os.ReadFile(store.Path())
			if err != nil || string(before) != string(after) {
				t.Errorf("refused writes changed persisted bytes: %v", err)
			}
		})
	}
}

func TestStringDefaultsRefuseLineBreaks(t *testing.T) {
	for _, text := range []string{"first\nsecond", "first\rsecond", "first\r\nsecond"} {
		t.Run(text, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Error("a multiline string default was accepted")
				}
			}()
			New(Config{ID: "prefs.lines.test", Preferences: []Preference{{Key: "title", Label: testPhrase("Title"), Kind: PreferenceString, Default: text}}})
		})
	}
}

func TestLegacyStringAPIRepair(t *testing.T) {
	battery := newPrefsBattery(t)
	store := withStore(t, battery)
	legacy := "first\r\nsecond"
	if err := store.Set(settingsStateKey, map[string]string{"export_folder": legacy}); err != nil {
		t.Fatal(err)
	}
	preferences := battery.Preferences()
	if preferences.String("export_folder") != legacy || preferences.All()["export_folder"] != legacy {
		t.Fatal("legacy reads lost line breaks")
	}
	if err := preferences.Set("export_folder", "repaired"); err != nil {
		t.Fatalf("explicit API repair refused: %v", err)
	}
	if preferences.String("export_folder") != "repaired" {
		t.Fatal("API repair was not stored")
	}
}
