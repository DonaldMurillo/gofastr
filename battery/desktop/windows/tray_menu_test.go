package windows

import (
	"testing"

	"github.com/DonaldMurillo/gofastr/battery/desktop"
)

func TestTrayMenuNeedsRecovery(t *testing.T) {
	for _, test := range []struct {
		name string
		tray *desktop.Tray
		want bool
	}{
		{"no tray", nil, false},
		{"no menu", &desktop.Tray{}, false},
		{"close hides", &desktop.Tray{CloseHidesWindow: true}, true},
		{"explicit menu", &desktop.Tray{Menu: &desktop.Menu{}}, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := shouldPlanTrayMenu(test.tray); got != test.want {
				t.Fatalf("plan tray menu = %v, want %v", got, test.want)
			}
			if !test.want {
				return
			}
			var actions []string
			rows := desktop.PlanTrayMenu(t.Context(), test.tray, "Notes", &actions)
			if test.tray.CloseHidesWindow && (len(rows) == 0 || rows[0].Role != desktop.RoleShow) {
				t.Fatalf("close-hiding tray has no Show action: %+v", rows)
			}
		})
	}
}
