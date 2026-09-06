package desktop

import (
	"context"
	"encoding/json"
	"errors"
)

// The preferences capability: the page's typed view of the app's
// declared preferences. Ungated like state and window.setPath (the page
// is the app), confined by construction: only declared keys can be read
// or written, and the values live in the battery's own "settings" state
// entry, which the state capability keeps out of the page's reach.
// Every successful set broadcasts preferences_changed with {keys} to
// all windows, the same cross-window sync state_changed gives.

// preferenceInfo is Preference's wire shape (the capability's `get`
// answer and the manifest-facing description of the declaration).
type preferenceInfo struct {
	Key     string   `json:"key"`
	Label   string   `json:"label"`
	Help    string   `json:"help,omitempty"`
	Kind    string   `json:"kind"`
	Default any      `json:"default"`
	Choices []string `json:"choices,omitempty"`
	Min     *int     `json:"min,omitempty"`
	Max     *int     `json:"max,omitempty"`
}

// preferencesGetOutput is get's result.
type preferencesGetOutput struct {
	Values   map[string]any   `json:"values"`
	Declared []preferenceInfo `json:"declared"`
}

// preferencesSetOutput is set's result: every declared key with its
// value after the call.
type preferencesSetOutput struct {
	Values map[string]any `json:"values"`
}

// preferenceErrorFor converts a setRaw failure into the bridge error:
// a preference validation error is invalid_input naming the key,
// everything else passes through (unsupported before Run).
func preferenceErrorFor(err error) error {
	var pe *preferenceError
	if errors.As(err, &pe) {
		return &Error{Code: CodeInvalidInput, Message: pe.Error()}
	}
	return err
}

// preferencesCapability builds the core "preferences" capability over
// the battery's declared preference list.
func (b *Battery) preferencesCapability() Capability {
	return Capability{
		Name:        "preferences",
		Description: "The app's declared preferences: typed values stored in the app state. get answers every value and declaration; set applies a whole object at once (all or nothing) and broadcasts preferences_changed with {keys} to all windows.",
		Version:     1,
		Methods: []Method{
			{
				Name:        "get",
				Description: "Returns every declared preference's current value and its declaration.",
				Input:       json.RawMessage(`{"type":"object"}`),
				Output:      json.RawMessage(`{"type":"object","properties":{"values":{"type":"object"},"declared":{"type":"array"}}}`),
				Handler: func(_ context.Context, _ json.RawMessage) (any, error) {
					p := b.prefs
					decls := p.Declared()
					infos := make([]preferenceInfo, 0, len(decls))
					for _, d := range decls {
						infos = append(infos, preferenceInfo{
							Key: d.Key, Label: d.Label, Help: d.Help,
							Kind: string(d.Kind), Default: d.Default,
							Choices: d.Choices, Min: d.Min, Max: d.Max,
						})
					}
					return preferencesGetOutput{Values: p.All(), Declared: infos}, nil
				},
			},
			{
				Name:        "set",
				Description: "Sets preferences by key. Every entry is validated first; the first invalid one refuses the whole call with invalid_input naming the key.",
				Input:       json.RawMessage(`{"type":"object","properties":{"values":{"type":"object"}},"required":["values"]}`),
				Output:      json.RawMessage(`{"type":"object","properties":{"values":{"type":"object"}}}`),
				Handler: func(_ context.Context, in json.RawMessage) (any, error) {
					var req struct {
						Values map[string]json.RawMessage `json:"values"`
					}
					if err := decodeInput(in, &req); err != nil {
						return nil, err
					}
					if req.Values == nil {
						return nil, &Error{Code: CodeInvalidInput, Message: "values must be an object of preference keys to values"}
					}
					if _, err := b.prefs.setRaw(req.Values); err != nil {
						return nil, preferenceErrorFor(err)
					}
					return preferencesSetOutput{Values: b.prefs.All()}, nil
				},
			},
		},
	}
}
