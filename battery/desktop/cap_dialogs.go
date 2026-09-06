package desktop

import (
	"context"
	"encoding/json"
)

// dialogsCapability: the OS file/folder pickers. Ungated on purpose:
// every dialog is user-mediated (the user picks the path), and every
// returned path lands on the fs allow-list, which is the actual grant.
func (b *Battery) dialogsCapability() Capability {
	return Capability{
		Name:        "dialogs",
		Description: "Operating-system file and folder dialogs. Paths the user picks become readable (and writable for files) through the fs capability.",
		Version:     1,
		Methods: []Method{
			{
				Name:        "openFile",
				Description: "Shows an open-file dialog; returns the picked path(s).",
				Input: json.RawMessage(`{"type":"object","properties":{` +
					`"filters":{"type":"array","items":{"type":"object","properties":{"name":{"type":"string"},"extensions":{"type":"array","items":{"type":"string"}}}}},` +
					`"multiple":{"type":"boolean"}}}`),
				Output: json.RawMessage(`{"type":"object","properties":{"paths":{"type":"array","items":{"type":"string"}}}}`),
				Handler: func(ctx context.Context, in json.RawMessage) (any, error) {
					var req struct {
						Filters []struct {
							Name       string   `json:"name"`
							Extensions []string `json:"extensions"`
						} `json:"filters"`
						Multiple bool `json:"multiple"`
					}
					if err := decodeInput(in, &req); err != nil {
						return nil, err
					}
					opts := OpenOptions{Multiple: req.Multiple}
					for _, f := range req.Filters {
						opts.Filters = append(opts.Filters, FileFilter{Name: f.Name, Extensions: f.Extensions})
					}
					paths, err := b.shell.Dialogs().OpenFile(ctx, opts)
					if err != nil {
						return nil, err
					}
					for _, p := range paths {
						if err := b.AllowPath(p); err != nil {
							return nil, &Error{Code: CodeInvalidInput, Message: "dialog returned an unusable path"}
						}
					}
					if paths == nil {
						paths = []string{}
					}
					return dialogPathsOutput{Paths: paths}, nil
				},
			},
			{
				Name:        "saveFile",
				Description: "Shows a save-file dialog; returns the chosen destination path.",
				Input:       json.RawMessage(`{"type":"object","properties":{"defaultName":{"type":"string"}}}`),
				Output:      json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}}}`),
				Handler: func(ctx context.Context, in json.RawMessage) (any, error) {
					var req struct {
						DefaultName string `json:"defaultName"`
					}
					if err := decodeInput(in, &req); err != nil {
						return nil, err
					}
					path, err := b.shell.Dialogs().SaveFile(ctx, SaveOptions{DefaultName: req.DefaultName})
					if err != nil {
						return nil, err
					}
					if err := b.AllowPath(path); err != nil {
						return nil, &Error{Code: CodeInvalidInput, Message: "dialog returned an unusable path"}
					}
					return dialogPathOutput{Path: path}, nil
				},
			},
			{
				Name:        "openFolder",
				Description: "Shows a folder-picker dialog; returns the chosen folder.",
				Output:      json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}}}`),
				Handler: func(ctx context.Context, in json.RawMessage) (any, error) {
					path, err := b.shell.Dialogs().OpenFolder(ctx)
					if err != nil {
						return nil, err
					}
					if err := b.AllowPath(path); err != nil {
						return nil, &Error{Code: CodeInvalidInput, Message: "dialog returned an unusable path"}
					}
					return dialogPathOutput{Path: path}, nil
				},
			},
		},
	}
}

type dialogPathsOutput struct {
	Paths []string `json:"paths"`
}

type dialogPathOutput struct {
	Path string `json:"path"`
}
