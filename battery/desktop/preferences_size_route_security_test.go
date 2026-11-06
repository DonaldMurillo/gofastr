package desktop_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/battery/desktop"
	"github.com/DonaldMurillo/gofastr/battery/desktop/appstate"
	"github.com/DonaldMurillo/gofastr/battery/desktop/desktoptest"
)

func TestPreferenceSizeRouteErrors(t *testing.T) {
	app, battery := prefsApp(t)
	harness := desktoptest.Run(t, app, battery)
	if err := battery.Preferences().Set("export_folder", "kept"); err != nil {
		t.Fatal(err)
	}
	values := map[string]any{"export_folder": strings.Repeat("<", appstate.MaxValueBytes/6), "work_minutes": 40}
	t.Run("bridge", func(t *testing.T) {
		result := harness.Call("preferences", "set", map[string]any{"values": values})
		result.AssertCode(t, desktop.CodeInvalidInput)
		if !strings.Contains(result.Message, "export_folder") {
			t.Fatalf("bridge refusal does not name the key: %q", result.Message)
		}
	})
	t.Run("form", func(t *testing.T) {
		response := harness.Post("/__gofastr/desktop/preferences", values)
		response.AssertStatus(t, http.StatusBadRequest)
		var envelope struct {
			Fields map[string][]string `json:"fields"`
		}
		if err := json.Unmarshal([]byte(response.Body), &envelope); err != nil || len(envelope.Fields["export_folder"]) == 0 {
			t.Fatalf("form refusal does not identify the control: %s (%v)", response.Body, err)
		}
	})
	if battery.Preferences().String("export_folder") != "kept" || battery.Preferences().Int("work_minutes") != 25 {
		t.Fatal("oversized bridge/form request partially changed preferences")
	}
}
