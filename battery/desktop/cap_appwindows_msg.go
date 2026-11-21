package desktop

import (
	"bytes"
	"context"
	"encoding/json"
	"net/url"

	"github.com/DonaldMurillo/gofastr/core/textsafe"
)

const maxEventPayload = 64 << 10

func dispatchJS(name, data string) string {
	return "window.__gofastr && window.__gofastr.desktop && " +
		"window.__gofastr.desktop._dispatch(" + jsQuote(name) + ", JSON.parse(" + jsQuote(data) + "))"
}

func (b *Battery) inAppJS(js string) string {
	b.windowMu.Lock()
	origin := "http://" + b.addr
	b.windowMu.Unlock()
	return "if (location.origin === " + jsQuote(origin) + ") { " + js + " }"
}

func (b *Battery) allowNavigation(raw string) bool {
	if b.cfg.DangerouslyAllowForeignNavigation || raw == "about:blank" {
		return true
	}
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	b.windowMu.Lock()
	addr := b.addr
	b.windowMu.Unlock()
	return addr != "" && u.Scheme == "http" && u.Host == addr
}

// NavigationHost returns raw's host with control bytes scrubbed, for logging a refused navigation.
func NavigationHost(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return textsafe.ScrubControlBytes(u.Host)
}

func (b *Battery) emitToWindowID(windowID, name, data string) error {
	w, ok := b.windowByID(windowID)
	if !ok {
		return &Error{Code: CodeNotFound, Message: "no window with that id"}
	}
	if err := w.Eval(b.inAppJS(dispatchJS(name, data))); err != nil {
		if _, ok := b.windowByID(windowID); !ok {
			return &Error{Code: CodeNotFound, Message: "no window with that id"}
		}
		return err
	}
	return nil
}

func compactEventPayload(p json.RawMessage) (string, *Error) {
	if len(p) == 0 {
		return "null", nil
	}
	var buf bytes.Buffer
	if err := json.Compact(&buf, p); err != nil {
		return "", &Error{Code: CodeInvalidInput, Message: "payload must be valid JSON"}
	}
	if buf.Len() > maxEventPayload {
		return "", &Error{Code: CodeInvalidInput, Message: "payload must be at most 64 KiB of compact JSON"}
	}
	return buf.String(), nil
}

func callerWindowID(ctx context.Context) string {
	if id, ok := ctx.Value(windowIDCtxKey{}).(string); ok && reWindowID.MatchString(id) {
		return id
	}
	return MainWindowID
}

type windowsBroadcastOutput struct {
	Delivered int `json:"delivered"`
}

type windowsSelfOutput struct {
	ID string `json:"id"`
}

func (b *Battery) windowsMessagingMethods() []Method {
	return []Method{
		{
			Name:        "post",
			Description: "Delivers an event to one open window's page listeners, the same _dispatch shape native events use. to must be an open window id; name follows the event grammar; the payload's compact JSON is at most 64 KiB.",
			Input: json.RawMessage(`{"type":"object","properties":{` +
				`"to":{"type":"string","description":"the target window id"},` +
				`"name":{"type":"string"},"payload":{}},"required":["to","name"]}`),
			Handler: func(ctx context.Context, in json.RawMessage) (any, error) {
				var req struct {
					To      string          `json:"to"`
					Name    string          `json:"name"`
					Payload json.RawMessage `json:"payload"`
				}
				if err := decodeInput(in, &req); err != nil {
					return nil, err
				}
				if _, ok := b.windowByID(req.To); !ok {
					return nil, &Error{Code: CodeNotFound, Message: "no window with that id"}
				}
				if !reCapabilityName.MatchString(req.Name) {
					return nil, &Error{Code: CodeInvalidInput, Message: "name must match ^[a-z][a-z0-9_]{0,63}$"}
				}
				data, err := compactEventPayload(req.Payload)
				if err != nil {
					return nil, err
				}
				if err := b.emitToWindowID(req.To, req.Name, data); err != nil {
					return nil, err
				}
				return nil, nil
			},
		},
		{
			Name:        "broadcast",
			Description: "Delivers an event to every open window's page listeners EXCEPT the caller's own (the caller knows what it sent). Same name and payload rules as post.",
			Input:       json.RawMessage(`{"type":"object","properties":{"name":{"type":"string"},"payload":{}},"required":["name"]}`),
			Output:      json.RawMessage(`{"type":"object","properties":{"delivered":{"type":"integer"}}}`),
			Handler: func(ctx context.Context, in json.RawMessage) (any, error) {
				var req struct {
					Name    string          `json:"name"`
					Payload json.RawMessage `json:"payload"`
				}
				if err := decodeInput(in, &req); err != nil {
					return nil, err
				}
				if !reCapabilityName.MatchString(req.Name) {
					return nil, &Error{Code: CodeInvalidInput, Message: "name must match ^[a-z][a-z0-9_]{0,63}$"}
				}
				data, err := compactEventPayload(req.Payload)
				if err != nil {
					return nil, err
				}
				caller := callerWindowID(ctx)
				delivered := 0
				for _, w := range b.Windows() {
					if w.ID() == caller {
						continue
					}
					if err := w.Eval(b.inAppJS(dispatchJS(req.Name, data))); err != nil {
						b.logger.Warn("desktop: broadcasting an event to a window failed",
							"window", w.ID(), "error", err.Error())
						continue
					}
					delivered++
				}
				return windowsBroadcastOutput{Delivered: delivered}, nil
			},
		},
		{
			Name:        "self",
			Description: "Returns the calling window's id as the page reported it (the host marker's window field; \"main\" when the page reported nothing).",
			Output:      json.RawMessage(`{"type":"object","properties":{"id":{"type":"string"}}}`),
			Handler: func(ctx context.Context, in json.RawMessage) (any, error) {
				return windowsSelfOutput{ID: callerWindowID(ctx)}, nil
			},
		},
	}
}
