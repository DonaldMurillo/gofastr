package main

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// TestSettingsFormShapedSaveRoundTrips drives the preferences route the
// way the rendered form does: the runtime serializes the
// hidden/checkbox pair and the number inputs as STRINGS ("true",
// "false", ""), and the route accepts exactly those spellings. Both
// bool directions persist, the re-rendered form reflects the value,
// and a blank string field is a real empty value.
func TestSettingsFormShapedSaveRoundTrips(t *testing.T) {
	h := newHarness(t)

	// Off: the string "false" persists and the form reflects it.
	res := h.Post("/__gofastr/desktop/preferences", map[string]any{
		"notify_on_save": "false",
		"export_folder":  "/tmp/exports",
	})
	if res.Status != http.StatusOK {
		t.Fatalf("form save off = %d %s", res.Status, res.Body)
	}
	if h.Battery.Preferences().Bool("notify_on_save") {
		t.Fatal(`notify_on_save="false" did not persist`)
	}
	if got := h.Battery.Preferences().String("export_folder"); got != "/tmp/exports" {
		t.Fatalf("export_folder = %q, want /tmp/exports", got)
	}
	form := h.Get("/settings").AssertStatus(t, http.StatusOK).Body
	box := form[strings.Index(form, `id="f-notify_on_save"`):]
	if strings.Contains(box[:strings.Index(box, ">")], "checked") {
		t.Fatal("form still renders the box checked after saving false")
	}

	// On again: the string "true".
	res = h.Post("/__gofastr/desktop/preferences", map[string]any{
		"notify_on_save": "true",
		"export_folder":  "",
	})
	if res.Status != http.StatusOK {
		t.Fatalf("form save on = %d %s", res.Status, res.Body)
	}
	if !h.Battery.Preferences().Bool("notify_on_save") {
		t.Fatal(`notify_on_save="true" did not persist`)
	}
	if got := h.Battery.Preferences().String("export_folder"); got != "" {
		t.Fatalf("export_folder = %q, want empty", got)
	}
	form = h.Get("/settings").AssertStatus(t, http.StatusOK).Body
	if !strings.Contains(form, `<input checked="checked" id="f-notify_on_save"`) {
		t.Fatal("form does not render the box checked after saving true")
	}

	// A value the form could not have produced is a field error, so
	// this test cannot pass by accident of a laxer validator.
	res = h.Post("/__gofastr/desktop/preferences", map[string]any{"notify_on_save": "on"})
	if res.Status != http.StatusBadRequest {
		t.Fatalf(`notify_on_save="on" = %d, want 400`, res.Status)
	}
	var env struct {
		Fields map[string][]string `json:"fields"`
	}
	if err := json.Unmarshal([]byte(res.Body), &env); err != nil {
		t.Fatal(err)
	}
	if _, ok := env.Fields["notify_on_save"]; !ok {
		t.Fatalf("envelope fields = %v, want notify_on_save", env.Fields)
	}
}
