package headless

import (
	_ "embed"

	uiregistry "github.com/DonaldMurillo/gofastr/core-ui/registry"
)

//go:embed leaveguard.js
var leaveguardJS string

// LeaveGuardBehaviorName is the runtime module that owns the leave
// guard's shared "changed" state: a form marked data-hui-leave-guard
// becomes dirty on input/change and clean again after a successful
// submit or a reset; while dirty, link navigation, an intercept
// layer's close paths and a reload all ask first. It exposes
// __gofastr._leaveGuard.ok(scope) for the kernel's intercept module
// and listens to the rpc module's gofastr:formresult for refused
// saves. Without the runtime the hook is inert markup.
const LeaveGuardBehaviorName = "headless-leaveguard"

var _ = uiregistry.RegisterBehavior(LeaveGuardBehaviorName, leaveguardJS,
	uiregistry.Markers("[data-hui-leave-guard]"))
