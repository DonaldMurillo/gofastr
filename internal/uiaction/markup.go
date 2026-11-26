// Package uiaction shares the action and toast wiring used by the browser adapters.
package uiaction

import (
	"fmt"
	"strings"

	"github.com/DonaldMurillo/gofastr/core/render"
)

const DefaultFailedText = "Could not save. Try again."

type ToastAction struct {
	Label string            `json:"label"`
	Attrs map[string]string `json:"attrs"`
}

func (a ToastAction) Validate() error {
	if a.Label == "" {
		return fmt.Errorf("a toast action needs a label")
	}
	if a.Attrs["data-cui-rpc"] == "" {
		return fmt.Errorf("a toast action needs an RPC action to run")
	}
	for k := range a.Attrs {
		if k != "data-cui-rpc" && !strings.HasPrefix(k, "data-cui-rpc-") {
			return fmt.Errorf("a toast action carries %s; a toast's button carries RPC wiring only", k)
		}
	}
	return nil
}

func MissingSuccessToast(attrs map[string]string) bool {
	_, action := attrs["data-cui-rpc-success-action"]
	_, toast := attrs["data-cui-rpc-success-toast"]
	return action && !toast
}

type Button struct {
	Endpoint, Method, State, FailedText string
	Idle, Done                          render.HTML
}

func (b Button) Render(attrs map[string]string, el func(tag, part string, attrs map[string]string, children ...render.HTML) render.HTML) render.HTML {
	if attrs == nil {
		attrs = map[string]string{}
	}
	attrs["type"] = "button"
	attrs["data-state"] = b.State
	attrs["data-hui-action"] = ""
	attrs["data-hui-action-endpoint"] = b.Endpoint
	attrs["data-hui-action-failed"] = b.FailedText
	if b.Method != "" && b.Method != "POST" {
		attrs["data-hui-action-method"] = b.Method
	}
	idle := map[string]string{"data-hui-action-idle": ""}
	done := map[string]string{"data-hui-action-done": ""}
	if b.State == "committed" {
		idle["hidden"] = ""
	} else {
		done["hidden"] = ""
	}
	status := map[string]string{"data-hui-action-status": "", "role": "status"}
	return el("button", "root", attrs,
		el("span", "action-idle", idle, b.Idle),
		el("span", "action-done", done, b.Done),
		el("span", "visually-hidden", status))
}
