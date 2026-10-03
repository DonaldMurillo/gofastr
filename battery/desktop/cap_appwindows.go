package desktop

import (
	"context"
	"encoding/json"
)

// windowsCapability: open, focus, close, and list the app's windows.
// Ungated, the page already lives in one of these windows and can
// only reach the app's own screens (path validated like a menu
// Navigate path).
func (b *Battery) windowsCapability() Capability {
	return Capability{
		Name:        "windows",
		Description: "The application's windows (the main window and the secondary windows the app opens).",
		Version:     1,
		Methods: append([]Method{
			{
				Name:        "open",
				Description: "Opens (or focuses) a secondary window on one of the app's own screens. style shapes the window's chrome and is applied when the window is CREATED; an already-open path is only focused, its style unchanged.",
				Input: json.RawMessage(`{"type":"object","properties":{` +
					`"path":{"type":"string"},"title":{"type":"string"},` +
					`"width":{"type":"integer"},"height":{"type":"integer"},` +
					`"style":{"type":"object","properties":{` +
					`"chrome":{"type":"string","enum":["default","hiddenTitle","none"]},` +
					`"float":{"type":"boolean"},"panel":{"type":"boolean"},` +
					`"transparent":{"type":"boolean"},"resizable":{"type":"boolean"},` +
					`"allSpaces":{"type":"boolean"},` +
					`"x":{"type":"integer"},"y":{"type":"integer"}}}},` +
					`"required":["path"]}`),
				Output: json.RawMessage(`{"type":"object","properties":{"id":{"type":"string"}}}`),
				Handler: func(ctx context.Context, in json.RawMessage) (any, error) {
					var req struct {
						Path   string             `json:"path"`
						Title  string             `json:"title"`
						Width  int                `json:"width"`
						Height int                `json:"height"`
						Style  *windowsStyleInput `json:"style"`
					}
					if err := decodeInput(in, &req); err != nil {
						return nil, err
					}
					if len(req.Title) > 512 {
						return nil, &Error{Code: CodeInvalidInput, Message: "title must be at most 512 characters"}
					}
					spec := WindowSpec{Path: req.Path, Title: req.Title, Width: req.Width, Height: req.Height}
					if req.Style != nil {
						style, err := req.Style.toWindowStyle()
						if err != nil {
							return nil, err
						}
						spec.Style = style
					}
					if req.Width < 0 || req.Height < 0 || req.Width > 100000 || req.Height > 100000 {
						return nil, &Error{Code: CodeInvalidInput, Message: "width and height must be between 0 and 100000"}
					}
					w, err := b.OpenWindow(spec)
					if err != nil {
						return nil, err
					}
					return windowsOpenOutput{ID: w.ID()}, nil
				},
			},
			{
				Name:        "openSettings",
				Description: "Opens (or focuses) the app's settings window; unsupported when the app configured none.",
				Output:      json.RawMessage(`{"type":"object","properties":{"id":{"type":"string"}}}`),
				Handler: func(ctx context.Context, in json.RawMessage) (any, error) {
					w, err := b.OpenSettings()
					if err != nil {
						return nil, err
					}
					return windowsOpenOutput{ID: w.ID()}, nil
				},
			},
			{
				Name:        "focus",
				Description: "Brings the window with the given id to front.",
				Input:       json.RawMessage(`{"type":"object","properties":{"id":{"type":"string"}},"required":["id"]}`),
				Handler: func(ctx context.Context, in json.RawMessage) (any, error) {
					id, err := windowsIDInput(in)
					if err != nil {
						return nil, err
					}
					w, ok := b.windowByID(id)
					if !ok {
						return nil, &Error{Code: CodeNotFound, Message: "no window with that id"}
					}
					if err := w.Focus(); err != nil {
						return nil, err
					}
					return nil, nil
				},
			},
			{
				Name:        "close",
				Description: "Closes a secondary window. The main window closes through the OS, never through the page.",
				Input:       json.RawMessage(`{"type":"object","properties":{"id":{"type":"string"}},"required":["id"]}`),
				Handler: func(ctx context.Context, in json.RawMessage) (any, error) {
					id, err := windowsIDInput(in)
					if err != nil {
						return nil, err
					}
					if id == "main" {
						return nil, &Error{Code: CodeInvalidInput, Message: "the main window closes through the OS, not through the page"}
					}
					w, ok := b.windowByID(id)
					if !ok {
						return nil, &Error{Code: CodeNotFound, Message: "no window with that id"}
					}
					if err := w.Close(); err != nil {
						return nil, err
					}
					b.handleWindowClosed(id)
					return nil, nil
				},
			},
			{
				Name:        "list",
				Description: "Returns every open window, the main window first.",
				Output:      json.RawMessage(`{"type":"object","properties":{"windows":{"type":"array","items":{"type":"object","properties":{"id":{"type":"string"},"title":{"type":"string"}}}}}}`),
				Handler: func(ctx context.Context, in json.RawMessage) (any, error) {
					var out windowsListOutput
					for _, w := range b.Windows() {
						out.Windows = append(out.Windows, windowsEntry{ID: w.ID(), Title: w.Title()})
					}
					return out, nil
				},
			},
		}, b.windowsMessagingMethods()...),
	}
}

// windowsStyleInput is the page-facing shape of WindowStyle. Decoding
// into a typed struct (never a map[string]any walk) makes a wrong-typed
// field a decode error, not a silent miss.
type windowsStyleInput struct {
	Chrome      string `json:"chrome"`
	Float       bool   `json:"float"`
	Panel       bool   `json:"panel"`
	Transparent bool   `json:"transparent"`
	Resizable   *bool  `json:"resizable"`
	AllSpaces   bool   `json:"allSpaces"`
	X           *int   `json:"x"`
	Y           *int   `json:"y"`
}

// toWindowStyle validates and maps the page's style object onto
// WindowStyle. The coordinate bound keeps a page from parking a window
// far off-screen; the pair rule matches the Go contract (both set, or
// centered).
func (s *windowsStyleInput) toWindowStyle() (WindowStyle, *Error) {
	var chrome WindowChrome
	switch s.Chrome {
	case "", "default":
		chrome = ChromeDefault
	case "hiddenTitle":
		chrome = ChromeHiddenTitle
	case "none":
		chrome = ChromeNone
	default:
		return WindowStyle{}, &Error{Code: CodeInvalidInput, Message: "style.chrome must be one of default, hiddenTitle, none"}
	}
	if (s.X == nil) != (s.Y == nil) {
		return WindowStyle{}, &Error{Code: CodeInvalidInput, Message: "style.x and style.y must be set together"}
	}
	for _, v := range []*int{s.X, s.Y} {
		if v != nil && (*v > 100000 || *v < -100000) {
			return WindowStyle{}, &Error{Code: CodeInvalidInput, Message: "style.x and style.y must be between -100000 and 100000"}
		}
	}
	return WindowStyle{
		Chrome:      chrome,
		Float:       s.Float,
		Panel:       s.Panel,
		Transparent: s.Transparent,
		Resizable:   s.Resizable,
		AllSpaces:   s.AllSpaces,
		X:           s.X,
		Y:           s.Y,
	}, nil
}

// windowByID resolves a live window by id ("main" or a secondary id).
func (b *Battery) windowByID(id string) (Window, bool) {
	b.windowMu.Lock()
	defer b.windowMu.Unlock()
	if id == "main" {
		return b.window, b.window != nil
	}
	w, ok := b.windows[id]
	return w, ok
}

// windowsIDInput decodes the shared {id} request shape.
func windowsIDInput(in json.RawMessage) (string, *Error) {
	var req struct {
		ID string `json:"id"`
	}
	if err := decodeInput(in, &req); err != nil {
		return "", err
	}
	if req.ID == "" {
		return "", &Error{Code: CodeInvalidInput, Message: "id is required"}
	}
	if len(req.ID) > 64 {
		return "", &Error{Code: CodeInvalidInput, Message: "id must be at most 64 characters"}
	}
	return req.ID, nil
}

type windowsOpenOutput struct {
	ID string `json:"id"`
}

type windowsEntry struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

type windowsListOutput struct {
	Windows []windowsEntry `json:"windows"`
}
