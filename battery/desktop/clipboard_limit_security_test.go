package desktop_test

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/battery/desktop"
	"github.com/DonaldMurillo/gofastr/battery/desktop/desktoptest"
)

func TestClipboardReadByteLimit(t *testing.T) {
	app, battery := prefsApp(t)
	harness := desktoptest.Run(t, app, battery)
	harness.Answer(desktop.DecisionAllow)
	for name, text := range map[string]string{
		"empty": "", "ascii boundary": strings.Repeat("a", 1<<20),
		"unicode boundary": strings.Repeat("é", 1<<19),
		"ascii over":       strings.Repeat("b", (1<<20)+1),
		"unicode over":     strings.Repeat("界", (1<<20)/3+1),
	} {
		t.Run(name, func(t *testing.T) {
			harness.Shell.SetClipboardText(text)
			result := harness.Call("clipboard", "readText", nil)
			if len(text) > 1<<20 {
				if result.OK {
					t.Fatalf("clipboard read accepted %d bytes", len(text))
				}
				result.AssertCode(t, desktop.CodeInvalidInput)
				if len(result.Result) != 0 && string(result.Result) != "null" {
					t.Fatalf("oversized clipboard leaked a %d-byte result", len(result.Result))
				}
				return
			}
			var output struct {
				Text string `json:"text"`
			}
			result.MustResult(t, &output)
			if output.Text != text {
				t.Fatal("clipboard text was truncated or changed")
			}
		})
	}
}
