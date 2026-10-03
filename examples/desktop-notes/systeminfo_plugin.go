package main

import (
	"context"
	"encoding/json"
	"runtime"

	"github.com/DonaldMurillo/gofastr/battery/desktop"
	"github.com/DonaldMurillo/gofastr/framework"
)

// systemInfoPlugin is the plugin-path proof: a plain framework.Plugin
// whose Init reaches the desktop battery through desktop.FromApp and
// registers a capability. No host change was needed for "systeminfo"
// to appear in the manifest, the generated bridge.js, and the .d.ts
// (`gofastr desktop types`).
type systemInfoPlugin struct{}

func (systemInfoPlugin) Name() string { return "systeminfo" }

func (systemInfoPlugin) Init(app *framework.App) error {
	d, err := desktop.FromApp(app)
	if err != nil {
		// The battery is optional: under `--serve`-only wiring without
		// RegisterBattery the plugin is a no-op, not a startup failure.
		app.Logger().Warn("desktop-notes: systeminfo plugin found no desktop battery", "error", err)
		return nil
	}
	return d.Register(desktop.Capability{
		Name:        "systeminfo",
		Description: "Machine facts the notes app demonstrates plugin capabilities with.",
		Version:     1,
		Methods: []desktop.Method{
			{
				Name:        "cpuCount",
				Description: "Returns the number of logical CPUs.",
				Output:      json.RawMessage(`{"type":"object","properties":{"count":{"type":"integer"}}}`),
				// Ungated: the CPU count is not a permission-worthy fact.
				Handler: func(ctx context.Context, in json.RawMessage) (any, error) {
					return map[string]any{"count": runtime.NumCPU()}, nil
				},
			},
		},
	})
}
