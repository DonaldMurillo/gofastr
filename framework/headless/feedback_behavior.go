package headless

import (
	_ "embed"

	uiregistry "github.com/DonaldMurillo/gofastr/core-ui/registry"
)

//go:embed feedback.js
var feedbackJS string

// FeedbackBehaviorName is the runtime module that binds the feedback hooks and
// owns the toast runtime the kernel's response-header path dispatches into.
const FeedbackBehaviorName = "headless-feedback"

var _ = uiregistry.RegisterBehavior(FeedbackBehaviorName, feedbackJS,
	uiregistry.Markers("[data-hui-copy]", "[data-hui-toast-stack]",
		"[data-cui-toast-stack]", "[data-hui-network-retry]"))
