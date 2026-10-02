package headless

import (
	_ "embed"

	uiregistry "github.com/DonaldMurillo/gofastr/core-ui/registry"
)

//go:embed feedback.js
var feedbackJS string

// FeedbackBehaviorName is the runtime module that binds the feedback
// family's data-hui-* hooks and owns the toast stack runtime the
// kernel's response-header path dispatches into. It replaces the
// retired core-ui/runtime copy, toasts and networkretrybanner modules;
// the kernel's loadModule('headless-feedback') retarget is what keeps
// NS.toast, _initToasts and the X-Gofastr-Toast dispatch working.
const FeedbackBehaviorName = "headless-feedback"

// The markers: the copy wrapper, the stack (both names — the kernel's
// data-fui-toast-stack is what a runtime-only stack or a ToastSlot
// carries, data-hui-toast-stack what the component renders), the bell
// and the retry link.
var _ = uiregistry.RegisterBehavior(FeedbackBehaviorName, feedbackJS,
	uiregistry.Markers("[data-hui-copy]", "[data-hui-toast-stack]",
		"[data-fui-toast-stack]", "[data-hui-notification-bell]",
		"[data-hui-network-retry]"))
