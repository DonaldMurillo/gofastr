package desktop

import (
	"context"
	"encoding/json"
)

// notificationsCapability: OS user notifications, gated
// notifications:show.
func (b *Battery) notificationsCapability() Capability {
	return Capability{
		Name:        "notifications",
		Description: "Operating-system user notifications.",
		Version:     1,
		Methods: []Method{
			{
				Name:        "show",
				Description: "Shows a notification with the given title, subtitle, and body.",
				Permission:  "notifications:show",
				Input: json.RawMessage(`{"type":"object","properties":{` +
					`"title":{"type":"string"},"subtitle":{"type":"string"},"body":{"type":"string"}},` +
					`"required":["title"]}`),
				Handler: func(ctx context.Context, in json.RawMessage) (any, error) {
					var req struct {
						Title    string `json:"title"`
						Subtitle string `json:"subtitle"`
						Body     string `json:"body"`
					}
					if err := decodeInput(in, &req); err != nil {
						return nil, err
					}
					if req.Title == "" {
						return nil, &Error{Code: CodeInvalidInput, Message: "title is required"}
					}
					if len(req.Title) > 200 {
						return nil, &Error{Code: CodeInvalidInput, Message: "title must be at most 200 characters"}
					}
					if len(req.Subtitle) > 200 {
						return nil, &Error{Code: CodeInvalidInput, Message: "subtitle must be at most 200 characters"}
					}
					if len(req.Body) > 2000 {
						return nil, &Error{Code: CodeInvalidInput, Message: "body must be at most 2000 characters"}
					}
					if err := b.shell.Notifier().Show(ctx, Notification{Title: req.Title, Subtitle: req.Subtitle, Body: req.Body}); err != nil {
						return nil, err
					}
					return nil, nil
				},
			},
		},
	}
}
