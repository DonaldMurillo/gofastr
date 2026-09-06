package desktop_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/battery/desktop"
	"github.com/DonaldMurillo/gofastr/battery/desktop/desktoptest"
	appui "github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/framework/uihost"
)

// The preferences capability through the real Run flow: the chokepoint,
// the session, and (in the screen suite) the form route. The store
// semantics themselves are preferences_test.go's, in-package.

// prefsDecls is one declaration of every kind, the focus example's
// shape.
func prefsDecls() []desktop.Preference {
	min := func(n int) *int { return &n }
	return []desktop.Preference{
		{Key: "work_minutes", Label: "Work minutes", Kind: desktop.PreferenceInt, Default: 25, Min: min(1), Max: min(180), Help: "Length of one work session."},
		{Key: "break_minutes", Label: "Break minutes", Kind: desktop.PreferenceInt, Default: 5, Min: min(1), Max: min(60)},
		{Key: "notify_on_done", Label: "Notify when a session ends", Kind: desktop.PreferenceBool, Default: true},
		{Key: "sound", Label: "Session sound", Kind: desktop.PreferenceChoice, Default: "chime", Choices: []string{"none", "chime", "bell"}},
		{Key: "export_folder", Label: "Export folder", Kind: desktop.PreferenceString, Default: ""},
	}
}

// prefsApp assembles an app declaring every preference kind, with the
// battery's own settings screen mounted at /settings the way a host
// mounts it.
func prefsApp(t *testing.T) (*framework.App, *desktop.Battery) {
	t.Helper()
	t.Setenv("GOFASTR_ISOLATION", "off")
	site := appui.NewApp("PrefsHarness")
	layout := appui.NewLayout("app").WithContainer()
	site.SetDefaultLayout(layout)
	site.Register("/", uiScreen{"UI home"}, layout)

	app := framework.NewApp(framework.WithConfig(framework.AppConfig{Name: "prefsharness"}))
	app.Mount(uihost.New(site))
	d := desktop.New(desktop.Config{
		ID:          "prefs.harness.test",
		Shell:       desktoptest.NewShell(),
		Preferences: prefsDecls(),
	})
	site.Register("/settings", desktop.PreferencesScreen(d, desktop.PreferencesScreenPath("/settings")), layout)
	app.RegisterBattery(d)
	return app, d
}

// prefsGet decodes a preferences.get result.
func prefsGet(t *testing.T, h *desktoptest.Harness) (values map[string]any, declared []map[string]any) {
	t.Helper()
	var out struct {
		Values   map[string]any   `json:"values"`
		Declared []map[string]any `json:"declared"`
	}
	h.Call("preferences", "get", nil).MustResult(t, &out)
	return out.Values, out.Declared
}

func TestPreferencesGetAnswersValuesAndDeclarations(t *testing.T) {
	app, d := prefsApp(t)
	_ = app
	h := desktoptest.Run(t, app, d)

	values, declared := prefsGet(t, h)
	if v, ok := values["work_minutes"].(float64); !ok || v != 25 {
		t.Fatalf("values[work_minutes] = %v, want 25", values["work_minutes"])
	}
	if v, ok := values["sound"].(string); !ok || v != "chime" {
		t.Fatalf("values[sound] = %v, want chime", values["sound"])
	}
	if len(declared) != 5 {
		t.Fatalf("declared = %d entries, want 5", len(declared))
	}
	first := declared[0]
	if first["key"] != "work_minutes" || first["kind"] != "int" || first["label"] != "Work minutes" {
		t.Fatalf("declared[0] = %v", first)
	}
	// The int declaration carries its bounds; the choice its options.
	var withBounds, withChoices bool
	for _, d := range declared {
		if d["key"] == "work_minutes" {
			if d["min"] == float64(1) && d["max"] == float64(180) {
				withBounds = true
			}
		}
		if d["key"] == "sound" {
			choices, ok := d["choices"].([]any)
			if ok && len(choices) == 3 {
				withChoices = true
			}
		}
	}
	if !withBounds || !withChoices {
		t.Fatalf("declared bounds/choices missing: %v", declared)
	}
}

func TestPreferencesSetAppliesAndEmits(t *testing.T) {
	app, d := prefsApp(t)
	h := desktoptest.Run(t, app, d)

	res := h.Call("preferences", "set", map[string]any{"values": map[string]any{
		"work_minutes": 50,
		"sound":        "bell",
	}})
	if !res.OK {
		t.Fatalf("preferences.set: %s %s", res.Code, res.Message)
	}
	var out struct {
		Values map[string]any `json:"values"`
	}
	if err := json.Unmarshal(res.Result, &out); err != nil {
		t.Fatal(err)
	}
	if v, _ := out.Values["work_minutes"].(float64); v != 50 {
		t.Fatalf("set result values[work_minutes] = %v, want 50", out.Values["work_minutes"])
	}
	// The Go side reads the same store.
	if got := d.Preferences().Int("work_minutes"); got != 50 {
		t.Fatalf("d.Preferences().Int = %d, want 50", got)
	}
	if got := d.Preferences().String("sound"); got != "bell" {
		t.Fatalf("d.Preferences().String = %q, want bell", got)
	}
	// Untouched keys keep their values.
	if got := d.Preferences().Bool("notify_on_done"); !got {
		t.Fatal("notify_on_done changed without being set")
	}
	// Every window hears preferences_changed with the sorted keys.
	ev := h.WaitEvent("preferences_changed")
	var payload struct {
		Keys []string `json:"keys"`
	}
	if err := ev.Unmarshal(&payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Keys) != 2 || payload.Keys[0] != "sound" || payload.Keys[1] != "work_minutes" {
		t.Fatalf("preferences_changed keys = %v, want [sound work_minutes]", payload.Keys)
	}
}

func TestPreferencesSetRefusesWholeCall(t *testing.T) {
	app, d := prefsApp(t)
	h := desktoptest.Run(t, app, d)

	// One valid entry plus one out-of-range entry: the call is refused
	// AND the valid entry is not applied.
	res := h.Call("preferences", "set", map[string]any{"values": map[string]any{
		"work_minutes":  40,
		"break_minutes": 999,
	}})
	res.AssertCode(t, desktop.CodeInvalidInput)
	if res.Message == "" || !strings.Contains(res.Message, "break_minutes") {
		t.Fatalf("refusal message = %q, want it to name break_minutes", res.Message)
	}
	if got := d.Preferences().Int("work_minutes"); got != 25 {
		t.Fatalf("work_minutes after a refused batch = %d, want the default 25", got)
	}

	// An undeclared key and a wrong-typed value are refused the same
	// way, naming the key.
	res = h.Call("preferences", "set", map[string]any{"values": map[string]any{"nope": true}})
	res.AssertCode(t, desktop.CodeInvalidInput)
	if !strings.Contains(res.Message, "nope") {
		t.Fatalf("undeclared message = %q", res.Message)
	}
	res = h.Call("preferences", "set", map[string]any{"values": map[string]any{"sound": 5}})
	res.AssertCode(t, desktop.CodeInvalidInput)
	if !strings.Contains(res.Message, "sound") {
		t.Fatalf("wrong-type message = %q", res.Message)
	}

	// values missing or not an object is invalid_input.
	h.Call("preferences", "set", nil).AssertCode(t, desktop.CodeInvalidInput)
	h.Call("preferences", "set", map[string]any{"values": "not-an-object"}).AssertCode(t, desktop.CodeInvalidInput)
}
