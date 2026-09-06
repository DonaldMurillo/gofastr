package desktop

import (
	"context"
	"encoding/json"
)

// trayCapability: drive the configured tray item. Ungated, the title
// is the app's own menu-bar label, not user data.
func (b *Battery) trayCapability() Capability {
	return Capability{
		Name:        "tray",
		Description: "The menu-bar tray item, when the app configured one.",
		Version:     1,
		Methods: []Method{
			{
				Name:        "setTitle",
				Description: "Changes the tray item's title.",
				Input:       json.RawMessage(`{"type":"object","properties":{"title":{"type":"string"}},"required":["title"]}`),
				Handler: func(ctx context.Context, in json.RawMessage) (any, error) {
					var req struct {
						Title string `json:"title"`
					}
					if err := decodeInput(in, &req); err != nil {
						return nil, err
					}
					if len(req.Title) > 64 {
						return nil, &Error{Code: CodeInvalidInput, Message: "title must be at most 64 characters"}
					}
					if err := b.SetTrayTitle(req.Title); err != nil {
						return nil, err
					}
					return nil, nil
				},
			},
		},
	}
}
