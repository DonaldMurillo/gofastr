package framework

import (
	"testing"

	"github.com/DonaldMurillo/gofastr/core/router"
)

type recordingMountable struct{ mounted bool }

func (r *recordingMountable) Mount(_ *router.Router) { r.mounted = true }

func TestNewUIHostAppMountsHost(t *testing.T) {
	h := &recordingMountable{}
	app := NewUIHostApp(h, WithConfig(AppConfig{Name: "t"}))
	if app == nil {
		t.Fatal("NewUIHostApp returned nil")
	}
	if !h.mounted {
		t.Fatal("NewUIHostApp should Mount the host")
	}
	if app.Config.Name != "t" {
		t.Errorf("AppOptions should apply: Name=%q", app.Config.Name)
	}
}
