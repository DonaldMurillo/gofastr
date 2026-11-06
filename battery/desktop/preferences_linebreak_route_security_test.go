package desktop_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/battery/desktop"
	"github.com/DonaldMurillo/gofastr/battery/desktop/appstate"
	"github.com/DonaldMurillo/gofastr/battery/desktop/desktoptest"
	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"
)

func TestStringRoutesRefuseLineBreaks(t *testing.T) {
	app, battery := prefsApp(t)
	harness := desktoptest.Run(t, app, battery)
	for _, text := range []string{"one\ntwo", "three\rfour", "five\r\nsix"} {
		values := map[string]any{"export_folder": text, "work_minutes": 40}
		result := harness.Call("preferences", "set", map[string]any{"values": values})
		result.AssertCode(t, desktop.CodeInvalidInput)
		if !strings.Contains(result.Message, "export_folder") {
			t.Errorf("bridge refusal lost the field key: %s", result.Message)
		}
		response := harness.Post("/__gofastr/desktop/preferences", values)
		response.AssertStatus(t, http.StatusBadRequest)
		var envelope struct{ Fields map[string][]string }
		if err := json.Unmarshal([]byte(response.Body), &envelope); err != nil || len(envelope.Fields["export_folder"]) == 0 {
			t.Errorf("form lost the field error: %s (%v)", response.Body, err)
		}
		if battery.Preferences().String("export_folder") != "" || battery.Preferences().Int("work_minutes") != 25 {
			t.Error("a refused request changed preferences")
		}
	}
}

func TestLegacyStringFormSave(t *testing.T) {
	for name, legacy := range map[string]string{"lf": "north\nsouth", "cr": "east\rwest", "crlf": "up\r\ndown"} {
		t.Run(name, func(t *testing.T) {
			base := t.TempDir()
			t.Setenv("GOFASTR_DESKTOP_DATA_DIR", base)
			statePath := filepath.Join(base, "prefs.harness.test", "state.json")
			if err := os.MkdirAll(filepath.Dir(statePath), 0o700); err != nil {
				t.Fatal(err)
			}
			store := appstate.Open(statePath, nil)
			if err := store.Set("settings", map[string]any{"export_folder": legacy, "work_minutes": 25}); err != nil {
				t.Fatal(err)
			}
			if err := store.Flush(); err != nil {
				t.Fatal(err)
			}
			app, battery := prefsApp(t)
			harness := desktoptest.Run(t, app, battery)
			if battery.Preferences().String("export_folder") != legacy {
				t.Fatal("legacy fixture did not load unchanged")
			}
			ctx := chromedptest.Context(t)
			cookie := harness.SessionCookie()
			var displayed string
			if err := chromedp.Run(ctx,
				network.SetCookie(cookie.Name, cookie.Value).WithURL(harness.URL("/")).WithHTTPOnly(true),
				chromedp.Navigate(harness.URL("/settings")),
				chromedp.WaitVisible(`#f-export_folder`),
				chromedp.Value(`#f-export_folder`, &displayed),
				chromedp.SetValue(`#f-work_minutes`, "40"),
				chromedp.Click(`form button[type="submit"]`),
			); err != nil {
				t.Fatal(err)
			}
			if displayed != strings.NewReplacer("\r", "", "\n", "").Replace(legacy) {
				t.Fatalf("browser sanitization = %q", displayed)
			}
			harness.Wait("the unrelated preference to save", func() bool { return battery.Preferences().Int("work_minutes") == 40 })
			if battery.Preferences().String("export_folder") != legacy {
				t.Fatal("saving another field silently stripped legacy line breaks")
			}
			var message string
			var screenshot []byte
			if err := chromedp.Run(ctx,
				chromedp.Poll(`document.getElementById('f-work_minutes').getAttribute('value') === '40'`, nil),
				chromedp.SetValue(`#f-export_folder`, "replacement"),
				chromedp.SetValue(`#f-work_minutes`, "55"),
				chromedp.Click(`form button[type="submit"]`),
				chromedp.Poll(`document.getElementById('f-export_folder').getAttribute('aria-invalid') === 'true'`, nil, chromedp.WithPollingTimeout(5*time.Second)),
				chromedp.Text(`#f-export_folder-error`, &message),
				chromedp.FullScreenshot(&screenshot, 90),
			); err != nil {
				t.Fatal(err)
			}
			if message == "" || battery.Preferences().String("export_folder") != legacy || battery.Preferences().Int("work_minutes") != 40 {
				t.Fatalf("legacy edit did not fail atomically with a field error: %q", message)
			}
			artifactDir := t.TempDir()
			if configured := os.Getenv("GOFASTR_TEST_ARTIFACT_DIR"); configured != "" {
				artifactDir = configured
			}
			if err := os.WriteFile(filepath.Join(artifactDir, fmt.Sprintf("preferences-%s.png", name)), screenshot, 0o600); err != nil {
				t.Fatal(err)
			}
			if err := harness.Quit(); err != nil {
				t.Fatal(err)
			}
			var saved map[string]any
			if _, err := appstate.Open(statePath, nil).Get("settings", &saved); err != nil || saved["export_folder"] != legacy || saved["work_minutes"] != float64(40) {
				t.Fatalf("legacy value changed on disk: %+v (%v)", saved, err)
			}
		})
	}
}
