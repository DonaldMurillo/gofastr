package uihost

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
)

func TestPrecacheRefusesWidgetState(t *testing.T) {
	application := app.NewApp("widgets")
	application.Register("/", &plainComp{}, nil)
	host := New(application, WithPWA(PWAConfig{Precache: []string{
		"/core-ui/widget/counter/state",
		"/core-ui/widget/profile/chrome",
		"/core-ui/widget",
		"/core-ui/%77idget/cart/state",
		"/static/../core-ui/widget/alerts/chrome",
		"/core-ui/widget-icons.svg",
	}}))
	worker := pwaGet(t, host, "/service-worker.js").Body.String()
	for _, entry := range pwaSWPrecache(t, worker) {
		if entry != "/core-ui/widget-icons.svg" && strings.Contains(entry, "/core-ui/") {
			t.Errorf("credentialed widget endpoint entered precache: %s", entry)
		}
	}
	if !pwaAllowedPrecache("/core-ui/widget-icons.svg", host.pwaDenyPaths(PWAConfig{})) {
		t.Fatal("widget sibling asset was denied")
	}
}
