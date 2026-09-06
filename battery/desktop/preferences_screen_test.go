package desktop_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/battery/desktop/desktoptest"
)

// The preferences screen and its form route through the real Run flow:
// the mounted screen renders one field per declaration from
// framework/ui components, the form's POST applies through the same
// all-or-nothing path the capability uses, and a refused submission
// answers the validation envelope the runtime's formerrors module
// renders.

func TestPreferencesScreenRendersEveryDeclaredField(t *testing.T) {
	app, d := prefsApp(t)
	h := desktoptest.Run(t, app, d)

	body := h.Get("/settings").AssertStatus(t, http.StatusOK).Body
	for _, want := range []string{
		`id="f-work_minutes"`,  // int
		`name="work_minutes"`,  // the form field the envelope targets
		`min="1"`, `max="180"`, // the declared bounds
		`value="25"`, // the default
		`id="f-break_minutes"`,
		`id="f-notify_on_done"`,          // bool
		`type="hidden"`, `value="false"`, // the pair's hidden half
		`type="checkbox"`, `checked`, // default true renders checked
		`<option selected="selected" value="chime"`,
		`id="f-export_folder"`, // string
		"Work minutes", "Session sound", "Export folder",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("/settings missing %q", want)
		}
	}
	// The form is an island form to the battery's route that navigates
	// back to the mount path, the resource engine's landing shape.
	if !strings.Contains(body, `data-fui-rpc="/__gofastr/desktop/preferences"`) {
		t.Fatal("the form does not submit through the runtime's intercept")
	}
	if !strings.Contains(body, `data-fui-rpc-navigate="/settings"`) {
		t.Fatal("the form does not navigate back to the mount path on save")
	}
	if !strings.Contains(body, `data-fui-comp`) {
		t.Fatal("the screen carries no design-system component markup")
	}
	if strings.Contains(body, "<style") || strings.Contains(body, `style="`) {
		t.Fatal("the settings screen ships CSS")
	}
}

func TestPreferencesFormSavesThroughTheRoute(t *testing.T) {
	app, d := prefsApp(t)
	h := desktoptest.Run(t, app, d)

	// The body the runtime's serializer produces for the rendered
	// form: bools and numbers as the strings the controls carry.
	res := h.Post("/__gofastr/desktop/preferences", map[string]any{
		"work_minutes":   "40",
		"notify_on_done": "false",
		"sound":          "bell",
		"export_folder":  "/tmp/exports",
		"_csrf":          "anything-the-form-stamped",
	})
	if res.Status != http.StatusOK {
		t.Fatalf("form save = %d %s", res.Status, res.Body)
	}
	p := d.Preferences()
	if got := p.Int("work_minutes"); got != 40 {
		t.Fatalf("work_minutes = %d, want 40", got)
	}
	if p.Bool("notify_on_done") {
		t.Fatal("notify_on_save-style bool did not save false")
	}
	if got := p.String("sound"); got != "bell" {
		t.Fatalf("sound = %q, want bell", got)
	}
	if got := p.String("export_folder"); got != "/tmp/exports" {
		t.Fatalf("export_folder = %q, want /tmp/exports", got)
	}
	// The page capability and the event carry the same change.
	values, _ := prefsGet(t, h)
	if v, ok := values["work_minutes"].(float64); !ok || v != 40 {
		t.Fatalf("preferences.get values[work_minutes] = %v, want 40", values["work_minutes"])
	}
	ev := h.WaitEvent("preferences_changed")
	var payload struct {
		Keys []string `json:"keys"`
	}
	if err := ev.Unmarshal(&payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Keys) != 4 {
		t.Fatalf("preferences_changed keys = %v, want the four saved keys", payload.Keys)
	}

	// The re-rendered screen shows the stored values.
	body := h.Get("/settings").AssertStatus(t, http.StatusOK).Body
	if !strings.Contains(body, `value="40"`) {
		t.Fatal("the re-rendered form does not show work minutes 40")
	}
	box := body[strings.Index(body, `id="f-notify_on_done"`):]
	if strings.Contains(box[:strings.Index(box, ">")], "checked") {
		t.Fatal("the re-rendered checkbox still renders checked after saving false")
	}

	// A stranger on the port gets the gate, not the route.
	req, _ := http.NewRequest(http.MethodPost, h.URL("/__gofastr/desktop/preferences"), strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	resp, err := h.Stranger().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("stranger POST the form route = %d, want 403", resp.StatusCode)
	}
}

func TestPreferencesFormBadIntAnswersTheEnvelope(t *testing.T) {
	app, d := prefsApp(t)
	h := desktoptest.Run(t, app, d)

	res := h.Post("/__gofastr/desktop/preferences", map[string]any{
		"work_minutes": "999",
		"sound":        "bell",
	})
	if res.Status != http.StatusBadRequest {
		t.Fatalf("bad int save = %d %s, want 400", res.Status, res.Body)
	}
	var env struct {
		Error  string              `json:"error"`
		Fields map[string][]string `json:"fields"`
	}
	if err := json.Unmarshal([]byte(res.Body), &env); err != nil {
		t.Fatalf("envelope does not decode: %v (%s)", err, res.Body)
	}
	msgs, ok := env.Fields["work_minutes"]
	if !ok || len(msgs) != 1 || !strings.Contains(msgs[0], "at most 180") {
		t.Fatalf("envelope fields = %v, want work_minutes: at most 180", env.Fields)
	}
	if !strings.Contains(env.Error, "work_minutes") {
		t.Fatalf("envelope error = %q, want it to name work_minutes", env.Error)
	}
	// All or nothing: the valid entry in the same body was not applied.
	if got := d.Preferences().String("sound"); got != "chime" {
		t.Fatalf("sound after a refused form = %q, want the default chime", got)
	}

	// A field the form never rendered is a field error, not a crash.
	res = h.Post("/__gofastr/desktop/preferences", map[string]any{"mystery": "1"})
	if res.Status != http.StatusBadRequest {
		t.Fatalf("undeclared field = %d, want 400", res.Status)
	}
	if err := json.Unmarshal([]byte(res.Body), &env); err != nil {
		t.Fatal(err)
	}
	if _, ok := env.Fields["mystery"]; !ok {
		t.Fatalf("undeclared field envelope = %v", env.Fields)
	}

	// A body that is not the serializer's shape is refused.
	res = h.Post("/__gofastr/desktop/preferences", "not-an-object")
	if res.Status != http.StatusBadRequest {
		t.Fatalf("non-object body = %d, want 400", res.Status)
	}
}

func TestPreferencesFormBlankIntKeepsTheStoredValue(t *testing.T) {
	app, d := prefsApp(t)
	h := desktoptest.Run(t, app, d)

	// Store a non-default value first.
	if err := d.Preferences().Set("work_minutes", 60); err != nil {
		t.Fatal(err)
	}
	// A blank number input means "not provided": the stored value
	// stands, the bool in the same body still applies.
	res := h.Post("/__gofastr/desktop/preferences", map[string]any{
		"work_minutes":   "",
		"notify_on_done": "false",
	})
	if res.Status != http.StatusOK {
		t.Fatalf("blank-int save = %d %s", res.Status, res.Body)
	}
	if got := d.Preferences().Int("work_minutes"); got != 60 {
		t.Fatalf("work_minutes after a blank submit = %d, want 60", got)
	}
	if d.Preferences().Bool("notify_on_done") {
		t.Fatal("notify_on_done did not save alongside the blank int")
	}
}
