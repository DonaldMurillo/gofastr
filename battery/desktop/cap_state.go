package desktop

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/DonaldMurillo/gofastr/battery/desktop/appstate"
)

const pageKeyPrefix = "page."

type stateGetOutput struct {
	Value json.RawMessage `json:"value"`
}

type stateKeysOutput struct {
	Keys []string `json:"keys"`
}

const stateKeyMsg = "key must match ^[a-z][a-z0-9_.-]{0,63}$ (with the page. prefix)"

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
					s := b.stateStore()
					if s == nil {
						return nil, b.stateNotOpen()
					}
					if err := s.Set(key, req.Value); err != nil {
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

func (b *Battery) stateStore() *appstate.Store { return b.state.Load() }

func (b *Battery) stateNotOpen() *Error {
	return &Error{Code: CodeUnsupported, Message: "the app state store is not open yet"}
}

func pageStateKey(key string) (string, *Error) {
	if !strings.HasPrefix(key, pageKeyPrefix) {
		return "", &Error{Code: CodeInvalidInput,
			Message: "key must start with \"page.\"; the app's own state keys are not reachable from the page"}
	}
	return key, nil
}

func (b *Battery) emitStateChanged(key string) {
	if err := b.Emit("state_changed", map[string]any{"key": key}); err != nil {
		b.logger.Warn("desktop: announcing a state change failed", "key", key, "error", err.Error())
	}
}
