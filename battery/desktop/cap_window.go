package desktop

import (
	"context"
	"encoding/base64"
	"encoding/json"

	"github.com/DonaldMurillo/gofastr/core/handler"
)

// decodeInput strictly decodes a bridge method input (refusing
// duplicate and case-folded keys); a decode failure is invalid_input.
func decodeInput(in json.RawMessage, dst any) *Error {
	if len(in) == 0 {
		in = json.RawMessage(`{}`)
	}
	if err := handler.UnmarshalStrict(in, dst); err != nil {
		return &Error{Code: CodeInvalidInput, Message: "invalid input for this method"}
	}
	return nil
}

// windowCapability: read and drive the window the page lives in.
// Ungated, the page already lives in that window.
func (b *Battery) windowCapability() Capability {
	return Capability{
		Name:        "window",
		Description: "The application window the page is running in.",
		Version:     1,
		Methods: []Method{
			{
				Name:        "title",
				Description: "Returns the current window title.",
				Output:      json.RawMessage(`{"type":"object","properties":{"title":{"type":"string"}}}`),
				Handler: func(ctx context.Context, in json.RawMessage) (any, error) {
					w, ok := b.Window()
					if !ok {
						return nil, &Error{Code: CodeUnsupported, Message: "window is not open yet"}
					}
					return windowTitleOutput{Title: w.Title()}, nil
				},
			},
			{
				Name:        "setTitle",
				Description: "Changes the window title.",
				Input:       json.RawMessage(`{"type":"object","properties":{"title":{"type":"string"}},"required":["title"]}`),
				Handler: func(ctx context.Context, in json.RawMessage) (any, error) {
					var req struct {
						Title string `json:"title"`
					}
					if err := decodeInput(in, &req); err != nil {
						return nil, err
					}
					if req.Title == "" {
						return nil, &Error{Code: CodeInvalidInput, Message: "title must not be empty"}
					}
					if len(req.Title) > 512 {
						return nil, &Error{Code: CodeInvalidInput, Message: "title must be at most 512 characters"}
					}
					w, ok := b.Window()
					if !ok {
						return nil, &Error{Code: CodeUnsupported, Message: "window is not open yet"}
					}
					if err := w.SetTitle(req.Title); err != nil {
						return nil, err
					}
					return nil, nil
				},
			},
			{
				Name:        "setPath",
				Description: "Reports the app path the page is showing (the runtime module sends it after every navigation). Ungated like the window header: a claim, and only the main window's report is remembered, as the relaunch redirect.",
				Input:       json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}},"required":["path"]}`),
				Handler: func(ctx context.Context, in json.RawMessage) (any, error) {
					var req struct {
						Path string `json:"path"`
					}
					if err := decodeInput(in, &req); err != nil {
						return nil, err
					}
					if !validNavigatePath(req.Path) {
						return nil, &Error{Code: CodeInvalidInput, Message: "path must be a same-origin absolute path (leading /, no scheme, no //, no .. segments)"}
					}
					// Only the main window's report is stored: the
					// window id is the page's own claim
					// (callerWindowID), and the relaunch redirect
					// concerns the main window only.
					if s := b.winStore.Load(); s != nil && callerWindowID(ctx) == mainWindowID {
						s.setMainPath(req.Path)
					}
					return nil, nil
				},
			},
			{
				Name:        "snapshot",
				Description: "Captures the rendered page as a base64 PNG.",
				Output:      json.RawMessage(`{"type":"object","properties":{"png":{"type":"string"}}}`),
				Handler: func(ctx context.Context, in json.RawMessage) (any, error) {
					w, ok := b.Window()
					if !ok {
						return nil, &Error{Code: CodeUnsupported, Message: "window is not open yet"}
					}
					png, err := w.Snapshot(ctx)
					if err != nil {
						return nil, err
					}
					return windowSnapshotOutput{PNG: base64.StdEncoding.EncodeToString(png)}, nil
				},
			},
		},
	}
}

type windowTitleOutput struct {
	Title string `json:"title"`
}

type windowSnapshotOutput struct {
	PNG string `json:"png"`
}
