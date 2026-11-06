package desktop

import (
	"context"
	"encoding/json"
)

const clipboardTextLimit = 1 << 20

// clipboardCapability: the OS clipboard, gated clipboard:read /
// clipboard:write.
func (b *Battery) clipboardCapability() Capability {
	return Capability{
		Name:        "clipboard",
		Description: "The operating-system clipboard.",
		Version:     1,
		Methods: []Method{
			{
				Name:        "readText",
				Description: "Returns the clipboard's current text.",
				Permission:  "clipboard:read",
				Output:      json.RawMessage(`{"type":"object","properties":{"text":{"type":"string"}}}`),
				Handler: func(ctx context.Context, in json.RawMessage) (any, error) {
					text, err := b.shell.Clipboard().ReadText(ctx)
					if err != nil {
						return nil, err
					}
					if len(text) > clipboardTextLimit {
						return nil, &Error{Code: CodeInvalidInput, Message: reasonClipboardTextLimit.Text(ctx)}
					}
					return clipboardTextOutput{Text: text}, nil
				},
			},
			{
				Name:        "writeText",
				Description: "Replaces the clipboard's text.",
				Permission:  "clipboard:write",
				Input:       json.RawMessage(`{"type":"object","properties":{"text":{"type":"string"}},"required":["text"]}`),
				Handler: func(ctx context.Context, in json.RawMessage) (any, error) {
					var req struct {
						Text string `json:"text"`
					}
					if err := decodeInput(in, &req); err != nil {
						return nil, err
					}
					if len(req.Text) > clipboardTextLimit {
						return nil, &Error{Code: CodeInvalidInput, Message: reasonClipboardTextLimit.Text(ctx)}
					}
					if err := b.shell.Clipboard().WriteText(ctx, req.Text); err != nil {
						return nil, err
					}
					return nil, nil
				},
			},
		},
	}
}

type clipboardTextOutput struct {
	Text string `json:"text"`
}
