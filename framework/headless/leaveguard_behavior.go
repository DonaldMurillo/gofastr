package headless

import (
	_ "embed"

	uiregistry "github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/runtime"
	"github.com/DonaldMurillo/gofastr/core/i18n"
)

//go:embed leaveguard.js
var leaveguardJS string

// LeaveGuardBehaviorName is the runtime module that keeps a
// data-hui-leave-guard form's unsaved-changes state and asks before a
// navigation, a layer close or a reload drops it.
const LeaveGuardBehaviorName = "headless-leaveguard"

var _ = uiregistry.RegisterBehavior(LeaveGuardBehaviorName, leaveguardJS,
	uiregistry.Markers("[data-hui-leave-guard]"))

var leaveGuardMessage = i18n.Define("headless.leave_guard", "You have unsaved changes.")

func init() {
	runtime.RegisterStrings(leaveGuardMessage)
}
