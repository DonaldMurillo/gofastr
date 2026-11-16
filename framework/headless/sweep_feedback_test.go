package headless

import (
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
)

func TestSweepFeedbackActionPartsCannotStealTheLifecycle(t *testing.T) {
	root := html.Attrs{"aria-busy": "true", "ARIA-PRESSED": "true", "aria-live": "assertive",
		"data-state": "committed", "data-testid": "keep"}
	got := OptimisticAction(OptimisticActionProps{Endpoint: "/save", IdleLabel: "Save", SuccessLabel: "Saved",
		Parts: Parts{Attrs: PartAttrs{PartRoot: root}}}, nil)
	hasNot(t, got, "aria-busy", "a root part attr claimed a request is in flight")
	hasNot(t, got, "aria-pressed", "a root part attr bound a one-shot action as a toggle")
	hasNot(t, got, "aria-live", "a root part attr made the button a live region")
	has(t, got, `data-state="idle"`, "a root part attr shipped a state the server did not know")
	has(t, got, `data-testid="keep"`, "an ordinary root part attr was dropped")
	if root["aria-busy"] != "true" || root["ARIA-PRESSED"] != "true" || len(root) != 5 {
		t.Errorf("the caller's root part attrs were written to: %v", root)
	}
}

func TestSweepFeedbackUntoggleEndpointImpliesUntoggle(t *testing.T) {
	base := ToggleActionProps{Endpoint: "/w", IdleLabel: "Watch", CommittedLabel: "Watching", Committed: true}
	withEndpoint := base
	withEndpoint.UntoggleEndpoint = "/unwatch"
	has(t, ToggleAction(withEndpoint, nil), `data-hui-action-untoggle="/unwatch"`,
		"an untoggle endpoint without AllowUntoggle left the button sticky")
	local := base
	local.AllowUntoggle = true
	has(t, ToggleAction(local, nil), `data-hui-action-untoggle=""`, "AllowUntoggle alone did not allow a local revert")
	hasNot(t, ToggleAction(base, nil), "data-hui-action-untoggle", "a sticky toggle carries the untoggle hook")
}

func TestSweepFeedbackActionMethodOmitsPOST(t *testing.T) {
	for _, m := range []string{"", "POST"} {
		hasNot(t, OptimisticAction(OptimisticActionProps{Endpoint: "/f", Method: m, IdleLabel: "a", SuccessLabel: "b"}, nil),
			"data-hui-action-method", "the runtime's default method was rendered as an attribute")
	}
	for _, m := range []string{"PUT", "PATCH", "DELETE"} {
		has(t, ToggleAction(ToggleActionProps{Endpoint: "/f", Method: m, IdleLabel: "a", CommittedLabel: "b"}, nil),
			`data-hui-action-method="`+m+`"`, "a non-default method did not reach the button")
	}
	refuse(t, "Method", func() {
		ToggleAction(ToggleActionProps{Endpoint: "/f", Method: "post", IdleLabel: "a", CommittedLabel: "b"}, nil)
	})
}

func TestSweepFeedbackButtonNeedsAName(t *testing.T) {
	refuse(t, "AriaLabel", func() { Button(ButtonProps{Icon: SpecimenGlyph}, nil) })
	has(t, Button(ButtonProps{AriaLabel: "Close", Icon: SpecimenGlyph}, nil), `aria-label="Close"`,
		"an icon-only button lost its name")
}

func TestSweepFeedbackExternalOwnsTargetAndRel(t *testing.T) {
	got := Button(ButtonProps{Label: "Docs", Href: "https://example.com", External: true,
		ExtraAttrs: html.Attrs{"TARGET": "_self", "Rel": "opener"}}, nil)
	has(t, got, `rel="noopener noreferrer"`, "an external link lost its noopener rel")
	has(t, got, `target="_blank"`, "an external link does not open a new tab")
	hasNot(t, got, "_self", "a caller's target beat the external contract")
	hasNot(t, got, `"opener"`, "a caller's rel beat the external contract")
}

func TestSweepFeedbackPopoverTriggerStartsCollapsed(t *testing.T) {
	got := Button(ButtonProps{Label: "Menu", PopoverTarget: "m", HasPopup: "menu"}, nil)
	has(t, got, `aria-expanded="false"`, "a popup trigger ships with no expanded state")
	has(t, got, `popovertarget="m"`, "the trigger lost its popover")
	hasNot(t, Button(ButtonProps{Label: "Menu", PopoverTarget: "m"}, nil), "aria-expanded",
		"a trigger that declares no popup claims an expanded state")
}

func TestSweepFeedbackToastTemplateRefusesBadNames(t *testing.T) {
	for _, v := range []string{"Neutral", "a b", "-x", "info", "danger"} {
		refuse(t, "Variants", func() { ToastTemplate(ToastTemplateProps{Variants: []string{v}}, nil) })
	}
	refuse(t, "StyleName", func() { ToastTemplate(ToastTemplateProps{StyleName: "ui notification"}, nil) })
	got := ToastTemplate(ToastTemplateProps{Variants: []string{"neutral"}, StyleName: "ui-notification",
		Glyphs: map[string]string{"neutral": "*"}}, Classes{"root--neutral": "t--neutral"})
	has(t, got, `data-cui-comp="ui-notification"`, "the row does not load its registered sheet")
	has(t, got, "data-hui-toast-variants=", "the open variant set did not travel")
	has(t, got, "t--neutral", "a variant's class did not travel")
}

func TestSweepFeedbackAlertRequiresContent(t *testing.T) {
	refuse(t, "Title, Text or Body", func() { Alert(AlertProps{Tone: "danger", ToneWord: "Error"}, nil) })
	has(t, Alert(AlertProps{Body: render.HTML("<ul><li>x</li></ul>")}, nil), "<li>x</li>",
		"an alert with only a body was refused or lost it")
}
