package windows

import "github.com/DonaldMurillo/gofastr/battery/desktop"

func shouldPlanTrayMenu(tray *desktop.Tray) bool {
	return tray != nil && (tray.Menu != nil || tray.CloseHidesWindow)
}
