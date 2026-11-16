package uihost

import (
	"github.com/DonaldMurillo/gofastr/core-ui/widget"
	"github.com/DonaldMurillo/gofastr/core-ui/widget/preset"
)

// DefaultToastStack is the name of the toast stack the host mounts at
// boot when the app mounted none.
const DefaultToastStack = "gofastr-toasts"

func (ds *UIHost) ensureToastStack() {
	if ds.coreRouter == nil {
		return
	}
	registered := false
	for _, d := range widget.AllForSSR() {
		if preset.IsToastStack(d) {
			registered = true
			widget.MountShared(ds.coreRouter, d)
		}
	}
	if !registered {
		widget.MountBuilder(ds.coreRouter, preset.ToastStack(DefaultToastStack))
	}
}
