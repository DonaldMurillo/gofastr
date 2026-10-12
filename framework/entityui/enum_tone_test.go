package entityui

import (
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/ui"
)

// Status words take one tone per meaning: settled, in flight, needing
// attention, failed, closed.
func TestEnumToneByMeaning(t *testing.T) {
	for v, want := range map[string]ui.StatusVariant{
		"paid":     ui.StatusSuccess,
		"Active":   ui.StatusSuccess,
		"open":     ui.StatusInfo,
		"trialing": ui.StatusInfo,
		"past_due": ui.StatusWarning,
		"pending":  ui.StatusWarning,
		"failed":   ui.StatusDanger,
		"draft":    ui.StatusNeutral,
		"void":     ui.StatusNeutral,
		"shipped":  ui.StatusInfo,
	} {
		if got := enumVariant(v); got != want {
			t.Errorf("enumVariant(%q) = %q, want %q", v, got, want)
		}
	}
}
