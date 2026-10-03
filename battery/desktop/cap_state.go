package desktop

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/DonaldMurillo/gofastr/battery/desktop/appstate"
)

// The state capability: the app's durable key/value store, served to
// the page. Ungated like window.setPath (the page is the app; the
// window id is a claim), but confined: page keys live under "page."
// only, so the battery's own entries ("windows", "settings") are never
// reachable from the page. Every successful set and delete broadcasts
// state_changed to every open window, which is the cross-window sync a
// settings widget wants: one window changes the theme, every window
// hears it.

// pageKeyPrefix is the only key namespace the page may touch.
const pageKeyPrefix = "page."

// stateGetOutput is get's result: the stored value, or JSON null when
// the key is absent.
type stateGetOutput struct {
	Value json.RawMessage `json:"value"`
}

// stateKeysOutput is keys' result.
type stateKeysOutput struct {
	Keys []string `json:"keys"`
}

// stateKeyMsg is the closed refusal message for a key the store's
// grammar refuses (the prefix check in pageStateKey runs first, so
// this is the "page.Bad" family).
const stateKeyMsg = "key must match ^[a-z][a-z0-9_.-]{0,63}$ (with the page. prefix)"

// stateCapability builds the core "state" capability over the
// battery's app state store.
func (b *Battery) stateCapability() Capability {
	return Capability{
		Name:        "state",
		Description: "The app's durable key/value store. Keys are confined to the \"page.\" namespace; every set and delete broadcasts state_changed with {key} to all windows.",
		Version:     1,
		Methods: []Method{
			{
				Name:        "get",
				Description: "Returns one page key's stored value (null when absent).",
				Input:       json.RawMessage(`{"type":"object","properties":{"key":{"type":"string"}},"required":["key"]}`),
				Output:      json.RawMessage(`{"type":"object","properties":{"value":{}}}`),
				Handler: func(ctx context.Context, in json.RawMessage) (any, error) {
					var req struct {
						Key string `json:"key"`
					}
					if err := decodeInput(in, &req); err != nil {
						return nil, err
					}
					key, derr := pageStateKey(req.Key)
					if derr != nil {
						return nil, derr
					}
					s := b.stateStore()
					if s == nil {
						return nil, b.stateNotOpen()
					}
					var out stateGetOutput
					found, err := s.Get(key, &out.Value)
					if err != nil {
						return nil, &Error{Code: CodeInvalidInput, Message: stateKeyMsg}
					}
					if !found || out.Value == nil {
						out.Value = json.RawMessage("null")
					}
					return out, nil
				},
			},
			{
				Name:        "set",
				Description: "Stores a JSON value under a page key (at most 64 KiB of compact JSON) and broadcasts state_changed.",
				Input:       json.RawMessage(`{"type":"object","properties":{"key":{"type":"string"},"value":{}},"required":["key"]}`),
				Handler: func(ctx context.Context, in json.RawMessage) (any, error) {
					var req struct {
						Key   string          `json:"key"`
						Value json.RawMessage `json:"value"`
					}
					if err := decodeInput(in, &req); err != nil {
						return nil, err
					}
					key, derr := pageStateKey(req.Key)
					if derr != nil {
						return nil, derr
					}
					data, derr := compactStateValue(req.Value)
					if derr != nil {
						return nil, derr
					}
					s := b.stateStore()
					if s == nil {
						return nil, b.stateNotOpen()
					}
					if err := s.Set(key, json.RawMessage(data)); err != nil {
						if errors.Is(err, appstate.ErrTooLarge) {
							return nil, &Error{Code: CodeInvalidInput, Message: "value must be at most 64 KiB of compact JSON"}
						}
						return nil, &Error{Code: CodeInvalidInput, Message: stateKeyMsg}
					}
					b.emitStateChanged(key)
					return nil, nil
				},
			},
			{
				Name:        "delete",
				Description: "Removes a page key and broadcasts state_changed.",
				Input:       json.RawMessage(`{"type":"object","properties":{"key":{"type":"string"}},"required":["key"]}`),
				Handler: func(ctx context.Context, in json.RawMessage) (any, error) {
					var req struct {
						Key string `json:"key"`
					}
					if err := decodeInput(in, &req); err != nil {
						return nil, err
					}
					key, derr := pageStateKey(req.Key)
					if derr != nil {
						return nil, derr
					}
					s := b.stateStore()
					if s == nil {
						return nil, b.stateNotOpen()
					}
					if err := s.Delete(key); err != nil {
						return nil, &Error{Code: CodeInvalidInput, Message: stateKeyMsg}
					}
					b.emitStateChanged(key)
					return nil, nil
				},
			},
			{
				Name:        "keys",
				Description: "Returns the page keys that start with prefix, sorted.",
				Input:       json.RawMessage(`{"type":"object","properties":{"prefix":{"type":"string"}}}`),
				Output:      json.RawMessage(`{"type":"object","properties":{"keys":{"type":"array","items":{"type":"string"}}}}`),
				Handler: func(ctx context.Context, in json.RawMessage) (any, error) {
					var req struct {
						Prefix string `json:"prefix"`
					}
					if err := decodeInput(in, &req); err != nil {
						return nil, err
					}
					s := b.stateStore()
					if s == nil {
						return nil, b.stateNotOpen()
					}
					return stateKeysOutput{Keys: s.Keys(pageKeyPrefix + req.Prefix)}, nil
				},
			},
		},
	}
}

// stateStore is the battery's app state store (nil before Run).
func (b *Battery) stateStore() *appstate.Store { return b.state.Load() }

// stateNotOpen is the before-Run refusal. The chokepoint only answers
// after Run froze the registry and the store opens in the same flow,
// so a page cannot normally see this; a nil guard is still the honest
// answer for the in-process callers.
func (b *Battery) stateNotOpen() *Error {
	return &Error{Code: CodeUnsupported, Message: "the app state store is not open yet"}
}

// pageStateKey validates a page-supplied state key: it must live under
// the page. prefix, which keeps the battery's own keys ("windows",
// "settings") out of the page's reach. The store's grammar (and its
// length cap, prefix included) is enforced by Set/Get/Delete.
func pageStateKey(key string) (string, *Error) {
	if !strings.HasPrefix(key, pageKeyPrefix) {
		return "", &Error{Code: CodeInvalidInput,
			Message: "key must start with \"page.\"; the app's own state keys are not reachable from the page"}
	}
	return key, nil
}

// compactStateValue normalizes a page-supplied value to compact JSON
// and enforces the size cap, measured on the compact form (the
// windows.post rule: whitespace cannot smuggle the difference).
func compactStateValue(v json.RawMessage) (string, *Error) {
	if len(v) == 0 {
		return "null", nil
	}
	var buf bytes.Buffer
	if err := json.Compact(&buf, v); err != nil {
		return "", &Error{Code: CodeInvalidInput, Message: "value must be valid JSON"}
	}
	if buf.Len() > appstate.MaxValueBytes {
		return "", &Error{Code: CodeInvalidInput, Message: "value must be at most 64 KiB of compact JSON"}
	}
	return buf.String(), nil
}

// emitStateChanged announces one page-key change to every open window.
// Best effort on top of a committed write: Emit already logs a Warn
// per failed window.
func (b *Battery) emitStateChanged(key string) {
	if err := b.Emit("state_changed", map[string]any{"key": key}); err != nil {
		b.logger.Warn("desktop: announcing a state change failed", "key", key, "error", err.Error())
	}
}
