package desktop

import (
	"bytes"
	"context"
	"encoding/json"
)

// The windows capability's cross-window messaging methods: post to one
// window, broadcast to the others, the caller's own id. Pages message
// each other through the server, never window to window: the bridge is
// the only transport, so the same chokepoint, body limits, and logging
// apply as to every other capability call. All three are ungated;
// only the app's own same-origin windows exist.
//
// This file also holds the delivery helpers Emit and EmitTo share with
// post and broadcast, so every _dispatch eval the process issues is
// built exactly one way.

// maxEventPayload caps the compact JSON of one cross-window payload
// (64 KiB). The chokepoint already caps the whole request body at
// 1 MiB; this is the policy bound for page-to-page messages. It is
// measured on the COMPACT form, so whitespace cannot smuggle the
// difference.
const maxEventPayload = 64 << 10

// dispatchJS builds the script every event delivery evaluates in a
// window: _dispatch(name, JSON.parse(payload)). Name and payload travel
// as quoted strings (jsQuote), and the payload is parsed by JSON.parse,
// never spliced in as an object literal: __proto__ is a setter in a
// literal, and duplicate __proto__ keys are a SyntaxError json.Valid
// accepts.
func dispatchJS(name, data string) string {
	return "window.__gofastr && window.__gofastr.desktop && " +
		"window.__gofastr.desktop._dispatch(" + jsQuote(name) + ", JSON.parse(" + jsQuote(data) + "))"
}

// emitToWindowID delivers one prepared event to a live window. A window
// that closed between the lookup and the eval reads as not_found, not
// internal: the user clicking a close button mid-delivery is normal
// window life, not a host error.
func (b *Battery) emitToWindowID(windowID, name, data string) error {
	w, ok := b.windowByID(windowID)
	if !ok {
		return &Error{Code: CodeNotFound, Message: "no window with that id"}
	}
	if err := w.Eval(dispatchJS(name, data)); err != nil {
		if _, ok := b.windowByID(windowID); !ok {
			return &Error{Code: CodeNotFound, Message: "no window with that id"}
		}
		return err
	}
	return nil
}

// compactEventPayload normalizes a page-supplied payload to compact
// JSON and enforces the size cap. An absent payload becomes JSON's
// null: an empty string would hand JSON.parse("") to the page, a
// SyntaxError, not "no payload".
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

// callerWindowID returns the window the calling page claims to live in
// (the chokepoint put windowIDFromRequest's answer in the context;
// "main" when the context carries nothing). A claim, not an identity.
func callerWindowID(ctx context.Context) string {
	if id, ok := ctx.Value(windowIDCtxKey{}).(string); ok && reWindowID.MatchString(id) {
		return id
	}
	return MainWindowID
}

// windowsBroadcastOutput is broadcast's result.
type windowsBroadcastOutput struct {
	Delivered int `json:"delivered"`
}

// windowsSelfOutput is self's result.
type windowsSelfOutput struct {
	ID string `json:"id"`
}

// windowsMessagingMethods are the cross-window methods of the windows
// capability: post to one window, broadcast to all but the caller, the
// caller's own id.
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
					if err := w.Eval(dispatchJS(req.Name, data)); err != nil {
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
