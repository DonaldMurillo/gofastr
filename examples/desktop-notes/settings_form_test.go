package main

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework"
)

// TestSettingsFormShapedSaveRoundTrips drives the settings row the way
// the rendered form does: the runtime serializes the hidden/checkbox
// pair as the STRING "true" or "false" (never a JSON bool, never "on").
// Both must persist, and the re-rendered form must reflect the value.
func TestSettingsFormShapedSaveRoundTrips(t *testing.T) {
	app, _, _ := newTestApp(t)
	ta := framework.TestHarness(t, app).AsUser(harnessUser{id: "u1"})
	ta.Get("/settings").AssertStatus(t, http.StatusOK)
	list := ta.Get("/api/settings").AssertStatus(t, http.StatusOK)
	var rows struct {
		Data []map[string]any `json:"data"`
	}
	if err := json.Unmarshal([]byte(list.Body()), &rows); err != nil || len(rows.Data) == 0 {
		t.Fatalf("settings list: %v %s", err, list.Body())
	}
	id, _ := rows.Data[0]["id"].(string)

	off := ta.Put("/api/settings/"+id, map[string]any{"notify_on_save": "false", "export_folder": "/tmp/exports"}).AssertStatus(t, http.StatusOK)
	var row map[string]any
	if err := decodeData(off.Body(), &row); err != nil {
		t.Fatal(err)
	}
	if v, _ := row["notifyOnSave"].(bool); v {
		t.Fatalf("notify_on_save=\"false\" did not persist: %s", off.Body())
	}
	form := ta.Get("/settings/"+id).AssertStatus(t, http.StatusOK).Body()
	box := form[strings.Index(form, `type="checkbox"`):]
	if strings.Contains(box[:strings.Index(box, ">")], "checked") {
		t.Fatal("form still renders the box checked after saving false")
	}

	on := ta.Put("/api/settings/"+id, map[string]any{"notify_on_save": "true", "export_folder": "/tmp/exports"}).AssertStatus(t, http.StatusOK)
	if err := decodeData(on.Body(), &row); err != nil {
		t.Fatal(err)
	}
	if v, _ := row["notifyOnSave"].(bool); !v {
		t.Fatalf("notify_on_save=\"true\" did not persist: %s", on.Body())
	}

	// The shape a bare checkbox used to send is still refused, so this
	// test cannot pass by accident of a laxer validator.
	ta.Put("/api/settings/"+id, map[string]any{"notify_on_save": "on"}).AssertStatus(t, http.StatusBadRequest)
}
