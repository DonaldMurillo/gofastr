package desktop

import (
	"context"
	"encoding/json"
	"errors"
)

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

type preferencesGetOutput struct {
	Values   map[string]any   `json:"values"`
	Declared []preferenceInfo `json:"declared"`
}

type preferencesSetOutput struct {
	Values map[string]any `json:"values"`
}

func preferenceErrorFor(err error) error {
	var pe *preferenceError
	if errors.As(err, &pe) {
		return &Error{Code: CodeInvalidInput, Message: pe.Error()}
	}
	return err
}

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
				Handler: func(ctx context.Context, _ json.RawMessage) (any, error) {
					p := b.prefs
					decls := p.Declared()
					infos := make([]preferenceInfo, 0, len(decls))
					for _, d := range decls {
						infos = append(infos, preferenceInfo{
							Key: d.Key, Label: d.Label.Text(ctx), Help: helpText(ctx, d),
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
