package desktop

import (
	"encoding/json"
	"errors"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/battery/desktop/appstate"
)

// The declared-preferences core: validation at New, typed reads before
// and after a store exists, Set's coercion and refusal rules, and the
// stored-value fallbacks. The screen and capability suites cover the
// surfaces built on this.

// testPreferenceDecls is one declaration of every kind, the focus
// example's shape.
func testPreferenceDecls() []Preference {
	min := func(n int) *int { return &n }
	return []Preference{
		{Key: "work_minutes", Label: "Work minutes", Kind: PreferenceInt, Default: 25, Min: min(1), Max: min(180)},
		{Key: "break_minutes", Label: "Break minutes", Kind: PreferenceInt, Default: 5, Min: min(1), Max: min(60)},
		{Key: "notify_on_done", Label: "Notify when a session ends", Kind: PreferenceBool, Default: true},
		{Key: "sound", Label: "Session sound", Kind: PreferenceChoice, Default: "chime", Choices: []string{"none", "chime", "bell"}},
		{Key: "export_folder", Label: "Export folder", Kind: PreferenceString, Default: ""},
	}
}

// newPrefsBattery builds a battery with the test declarations and no
// shell (nothing runs).
func newPrefsBattery(t *testing.T) *Battery {
	t.Helper()
	return New(Config{ID: "prefs.unit.test", Preferences: testPreferenceDecls()})
}

// withStore opens a store on a temp dir and hands it to the battery the
// way Run would.
func withStore(t *testing.T, b *Battery) *appstate.Store {
	t.Helper()
	s := appstate.Open(filepath.Join(t.TempDir(), stateFileName), slog.New(slog.NewTextHandler(&strings.Builder{}, nil)))
	b.state.Store(s)
	return s
}

func TestNewRejectsBadPreferenceDeclarations(t *testing.T) {
	min := func(n int) *int { return &n }
	cases := []struct {
		name string
		decl Preference
		want string
	}{
		{"bad key", Preference{Key: "1x", Label: "L", Kind: PreferenceBool, Default: true}, "must match"},
		{"dot in key", Preference{Key: "a.b", Label: "L", Kind: PreferenceBool, Default: true}, "must match"},
		{"missing label", Preference{Key: "ok_key", Kind: PreferenceBool, Default: true}, "requires Label"},
		{"unknown kind", Preference{Key: "ok_key", Label: "L", Kind: "float", Default: 1.5}, "unknown kind"},
		{"bool default mismatch", Preference{Key: "ok_key", Label: "L", Kind: PreferenceBool, Default: 1}, "bool Default"},
		{"int default mismatch", Preference{Key: "ok_key", Label: "L", Kind: PreferenceInt, Default: "25"}, "int Default"},
		{"string default mismatch", Preference{Key: "ok_key", Label: "L", Kind: PreferenceString, Default: 5}, "string Default"},
		{"choice without choices", Preference{Key: "ok_key", Label: "L", Kind: PreferenceChoice, Default: "a"}, "requires Choices"},
		{"choice default not offered", Preference{Key: "ok_key", Label: "L", Kind: PreferenceChoice, Default: "x", Choices: []string{"a", "b"}}, "not one of Choices"},
		{"choice default wrong type", Preference{Key: "ok_key", Label: "L", Kind: PreferenceChoice, Default: 1, Choices: []string{"a"}}, "string Default"},
		{"min above max", Preference{Key: "ok_key", Label: "L", Kind: PreferenceInt, Default: 5, Min: min(10), Max: min(3)}, "greater than Max"},
		{"default below min", Preference{Key: "ok_key", Label: "L", Kind: PreferenceInt, Default: 0, Min: min(1)}, "below Min"},
		{"default above max", Preference{Key: "ok_key", Label: "L", Kind: PreferenceInt, Default: 5, Max: min(3)}, "above Max"},
		{"min on bool", Preference{Key: "ok_key", Label: "L", Kind: PreferenceBool, Default: true, Min: min(1)}, "cannot carry Min or Max"},
		{"choices on string", Preference{Key: "ok_key", Label: "L", Kind: PreferenceString, Default: "a", Choices: []string{"a"}}, "cannot carry Choices"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				r := recover()
				if r == nil {
					t.Fatal("New accepted a bad declaration")
				}
				msg, _ := r.(string)
				if !strings.Contains(msg, tc.want) || !strings.Contains(msg, "desktop:") {
					t.Fatalf("panic = %v, want it to name %q with the desktop: prefix", r, tc.want)
				}
			}()
			New(Config{ID: "prefs.unit.test", Preferences: []Preference{tc.decl}})
		})
	}
	// And a duplicate key refuses too.
	defer func() {
		r := recover()
		if r == nil || !strings.Contains(r.(string), "duplicate preference key") {
			t.Fatalf("duplicate key panic = %v", r)
		}
	}()
	decls := testPreferenceDecls()
	New(Config{ID: "prefs.unit.test", Preferences: append(decls, Preference{Key: "sound", Label: "Again", Kind: PreferenceBool, Default: false})})
}

func TestNewTakesACopyOfDeclarations(t *testing.T) {
	decls := testPreferenceDecls()
	b := New(Config{ID: "prefs.unit.test", Preferences: decls})
	// Mutate everything the caller still holds; the battery must not
	// see it.
	decls[0].Default = 999
	decls[0].Min = nil
	decls[3].Choices[0] = "hijacked"
	if got := b.Preferences().Int("work_minutes"); got != 25 {
		t.Fatalf("work_minutes after caller mutation = %d, want 25", got)
	}
	if got := b.Preferences().Declared()[3].Choices[0]; got != "none" {
		t.Fatalf("Choices after caller mutation = %q, want none (the slice was not copied)", got)
	}
	if got := b.Preferences().Declared()[0]; got.Min == nil {
		t.Fatal("the Min pointer was not copied")
	}
}

func TestPreferencesBeforeRunAnswerDeclaredDefaults(t *testing.T) {
	b := newPrefsBattery(t)
	p := b.Preferences()
	if got := p.Int("work_minutes"); got != 25 {
		t.Fatalf("work_minutes = %d, want 25", got)
	}
	if got := p.Bool("notify_on_done"); !got {
		t.Fatal("notify_on_done = false, want true")
	}
	if got := p.String("sound"); got != "chime" {
		t.Fatalf("sound = %q, want chime", got)
	}
	if got := p.String("export_folder"); got != "" {
		t.Fatalf("export_folder = %q, want empty", got)
	}
	all := p.All()
	if len(all) != 5 {
		t.Fatalf("All = %v, want 5 keys", all)
	}
	if all["notify_on_done"] != true || all["work_minutes"] != 25 {
		t.Fatalf("All = %v", all)
	}
	// Set before Run has no store to write: unsupported, never a panic.
	err := p.Set("work_minutes", 30)
	var de *Error
	if !errors.As(err, &de) || de.Code != CodeUnsupported {
		t.Fatalf("Set before Run = %v, want unsupported", err)
	}
}

func TestPreferencesReadersPanicOnWrongUse(t *testing.T) {
	b := newPrefsBattery(t)
	p := b.Preferences()
	for _, tc := range []struct {
		name string
		call func()
		want string
	}{
		{"undeclared key", func() { p.Bool("nope") }, "not declared"},
		{"bool reader on int", func() { p.Bool("work_minutes") }, "not bool"},
		{"int reader on choice", func() { p.Int("sound") }, "not int"},
		{"string reader on bool", func() { p.String("notify_on_done") }, "not string"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				r := recover()
				if r == nil {
					t.Fatal("the read did not panic")
				}
				if !strings.Contains(r.(string), tc.want) {
					t.Fatalf("panic = %v, want %q", r, tc.want)
				}
			}()
			tc.call()
		})
	}
}

func TestPreferencesSetCoercesAndRefuses(t *testing.T) {
	b := newPrefsBattery(t)
	withStore(t, b)
	p := b.Preferences()

	// Typed Go values, JSON numbers, and the form serializer's string
	// spellings all land the same.
	if err := p.Set("work_minutes", 30); err != nil {
		t.Fatalf("set int 30: %v", err)
	}
	if err := p.Set("break_minutes", "7"); err != nil {
		t.Fatalf("set int \"7\": %v", err)
	}
	if err := p.Set("notify_on_done", false); err != nil {
		t.Fatalf("set bool: %v", err)
	}
	if err := p.Set("sound", "bell"); err != nil {
		t.Fatalf("set choice: %v", err)
	}
	if got := p.Int("work_minutes"); got != 30 {
		t.Fatalf("work_minutes = %d, want 30", got)
	}
	if got := p.Int("break_minutes"); got != 7 {
		t.Fatalf("break_minutes = %d, want 7", got)
	}
	if p.Bool("notify_on_done") {
		t.Fatal("notify_on_done = true, want false")
	}
	if got := p.String("sound"); got != "bell" {
		t.Fatalf("sound = %q, want bell", got)
	}

	for _, tc := range []struct {
		key  string
		val  any
		want string
	}{
		{"work_minutes", 0, "at least 1"},
		{"work_minutes", 181, "at most 180"},
		{"work_minutes", "soon", "whole number"},
		{"work_minutes", 25.5, "whole number"},
		{"work_minutes", true, "whole number"},
		{"notify_on_done", "yes", "true or false"},
		{"notify_on_done", 1, "true or false"},
		{"sound", "gong", "one of"},
		{"sound", 5, "string"},
		{"export_folder", 5, "string"},
		{"undeclared", true, "not a declared preference"},
	} {
		err := p.Set(tc.key, tc.val)
		var pe *preferenceError
		if !errors.As(err, &pe) {
			t.Fatalf("Set(%s, %v) = %v, want a preferenceError", tc.key, tc.val, err)
		}
		if !strings.Contains(pe.msg, tc.want) {
			t.Fatalf("Set(%s, %v) msg = %q, want it to contain %q", tc.key, tc.val, pe.msg, tc.want)
		}
		if pe.key != tc.key {
			t.Fatalf("preferenceError key = %q, want %q", pe.key, tc.key)
		}
	}
	// Every refusal left the stored value alone.
	if got := p.Int("work_minutes"); got != 30 {
		t.Fatalf("work_minutes after refusals = %d, want 30", got)
	}
}

func TestSetRawIsAllOrNothing(t *testing.T) {
	b := newPrefsBattery(t)
	withStore(t, b)
	p := b.Preferences()

	_, err := p.setRaw(map[string]json.RawMessage{
		"work_minutes": json.RawMessage(`40`),
		"sound":        json.RawMessage(`"nope"`),
	})
	var pe *preferenceError
	if !errors.As(err, &pe) || pe.key != "sound" {
		t.Fatalf("setRaw = %v, want a preferenceError on sound", err)
	}
	// The valid entry in the same call was NOT applied.
	if got := p.Int("work_minutes"); got != 25 {
		t.Fatalf("work_minutes after a refused batch = %d, want the default 25", got)
	}
}

func TestPreferencesIgnoreUndeclaredStoredKeys(t *testing.T) {
	b := newPrefsBattery(t)
	s := withStore(t, b)
	// A preference a previous version declared, plus a value for one
	// that still exists.
	if err := s.Set(settingsStateKey, map[string]any{"removed_pref": true, "work_minutes": 45}); err != nil {
		t.Fatal(err)
	}
	p := b.Preferences()
	if got := p.Int("work_minutes"); got != 45 {
		t.Fatalf("work_minutes = %d, want 45", got)
	}
	all := p.All()
	if _, ok := all["removed_pref"]; ok {
		t.Fatalf("All answered an undeclared key: %v", all)
	}
	// The undeclared key is still in the file after a Set (left in
	// place, never read).
	if err := p.Set("sound", "none"); err != nil {
		t.Fatal(err)
	}
	var stored map[string]json.RawMessage
	if _, err := s.Get(settingsStateKey, &stored); err != nil {
		t.Fatal(err)
	}
	if _, ok := stored["removed_pref"]; !ok {
		t.Fatal("the undeclared stored key was dropped")
	}
}

func TestStoredValueFailingItsKindFallsBackWithDefault(t *testing.T) {
	b := newPrefsBattery(t)
	s := withStore(t, b)
	if err := s.Set(settingsStateKey, map[string]any{"work_minutes": "tomorrow", "sound": 9}); err != nil {
		t.Fatal(err)
	}
	p := b.Preferences()
	if got := p.Int("work_minutes"); got != 25 {
		t.Fatalf("work_minutes = %d, want the default 25", got)
	}
	if got := p.String("sound"); got != "chime" {
		t.Fatalf("sound = %q, want the default chime", got)
	}
	// An out-of-range stored value is also refused on read.
	if err := s.Set(settingsStateKey, map[string]any{"work_minutes": 9999}); err != nil {
		t.Fatal(err)
	}
	if got := p.Int("work_minutes"); got != 25 {
		t.Fatalf("out-of-range work_minutes = %d, want the default 25", got)
	}
	// A stored entry that is not an object at all reads as defaults.
	if err := s.Set(settingsStateKey, "nonsense"); err != nil {
		t.Fatal(err)
	}
	if got := p.Int("work_minutes"); got != 25 {
		t.Fatalf("non-object entry: work_minutes = %d, want 25", got)
	}
}

func TestPreferencesSurviveASecondBatteryOnTheSameDataDir(t *testing.T) {
	dir := t.TempDir()
	build := func() *Battery {
		b := New(Config{ID: "prefs.unit.test", Preferences: testPreferenceDecls()})
		b.state.Store(appstate.Open(filepath.Join(dir, stateFileName), nil))
		return b
	}
	first := build()
	if err := first.Preferences().Set("work_minutes", 50); err != nil {
		t.Fatal(err)
	}
	if err := first.State().Flush(); err != nil {
		t.Fatal(err)
	}
	second := build()
	if got := second.Preferences().Int("work_minutes"); got != 50 {
		t.Fatalf("work_minutes on relaunch = %d, want 50", got)
	}
}
