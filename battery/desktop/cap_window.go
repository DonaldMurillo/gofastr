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
					w, werr := b.callerWindow(ctx)
					if werr != nil {
						return nil, werr
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
					w, werr := b.callerWindow(ctx)
					if werr != nil {
						return nil, werr
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
					if s := b.winStore.Load(); s != nil && callerWindowID(ctx) == MainWindowID {
						s.setMainPath(req.Path)
					}
					return nil, nil
				},
			},
			{
				Name:        "setChrome",
				Description: "Reports the sidebar zone's width in points as the page measured it (a ResizeObserver on the sidebar element). Ungated like setPath: a claim, applied to the calling window only.",
				Input:       json.RawMessage(`{"type":"object","properties":{"sidebarWidth":{"type":"integer","minimum":0,"maximum":4096}},"required":["sidebarWidth"]}`),
				Handler: func(ctx context.Context, in json.RawMessage) (any, error) {
					var req struct {
						SidebarWidth int `json:"sidebarWidth"`
					}
					if err := decodeInput(in, &req); err != nil {
						return nil, err
					}
					if req.SidebarWidth < 0 || req.SidebarWidth > MaxSidebarWidth {
						return nil, &Error{Code: CodeInvalidInput, Message: "sidebarWidth must be between 0 and 4096"}
					}
					w, ok := b.windowByID(callerWindowID(ctx))
					if !ok {
						return nil, &Error{Code: CodeNotFound, Message: "the calling window is not open"}
					}
					if err := w.SetSidebarWidth(req.SidebarWidth); err != nil {
						return nil, err
					}
					return nil, nil
				},
			},
			{
				Name:        "snapshot",
				Description: "Captures the rendered page as a base64 PNG.",
				Output:      json.RawMessage(`{"type":"object","properties":{"png":{"type":"string"}}}`),
				Handler: func(ctx context.Context, in json.RawMessage) (any, error) {
					w, werr := b.callerWindow(ctx)
					if werr != nil {
						return nil, werr
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

// callerWindow resolves the window the request came from
// (callerWindowID). The main window not open yet is unsupported, the
// way it always answered; a secondary window that has closed is
// not_found and never falls back to the main window.
func (b *Battery) callerWindow(ctx context.Context) (Window, *Error) {
	id := callerWindowID(ctx)
	if w, ok := b.windowByID(id); ok {
		return w, nil
	}
	if id == MainWindowID {
		return nil, &Error{Code: CodeUnsupported, Message: "window is not open yet"}
	}
	return nil, &Error{Code: CodeNotFound, Message: "the calling window is not open"}
}

type windowTitleOutput struct {
	Title string `json:"title"`
}

type windowSnapshotOutput struct {
	PNG string `json:"png"`
}
