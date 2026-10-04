package uihost

import (
	"github.com/DonaldMurillo/gofastr/core-ui/widget"
	"github.com/DonaldMurillo/gofastr/core-ui/widget/preset"
)

// DefaultToastStack is the name of the toast stack the host mounts at
// boot when the app mounted none. A toast raised by a response header
// or window.__gofastr.toast needs a region to land in; the
// headless-feedback module mounts none of its own (a module that
// invents a region invents its classes too), so the host guarantees
// one. An app that mounts its own stack, under any name, keeps it and
// gets no second.
const DefaultToastStack = "gofastr-toasts"

// ensureToastStack mounts the default stack unless a preset-built one
// is already registered. Called from ValidateBoot, after the last
// route registration, so an app's own mount anywhere in its boot wins.
func (ds *UIHost) ensureToastStack() {
	if ds.coreRouter == nil {
		return
	}
	for _, d := range widget.AllForSSR() {
		if preset.IsToastStack(d) {
			return
		}
	}
	widget.MountBuilder(ds.coreRouter, preset.ToastStack(DefaultToastStack))
}
