package desktop

import (
	"context"
	"encoding/json"
)

// updatesCapability: the page's view of the updater. check is ungated
// (it reads the public feed); install is gated updates:install so the
// OS prompt asks the user once before an app bundle is replaced.
//
// New registers this capability for every battery, configured or not:
// an unconfigured app gets a real unsupported error instead of an
// undefined namespace, the same posture as an OS-absent capability.
func (b *Battery) updatesCapability() Capability {
	return Capability{
		Name:        "updates",
		Description: "App auto-update: check the signed release feed and install the next version.",
		Version:     1,
		Methods: []Method{
			{
				Name:        "check",
				Description: "Checks the update feed now and reports the newest release.",
				Output: json.RawMessage(`{"type":"object","properties":{` +
					`"available":{"type":"boolean"},"version":{"type":"string"},` +
					`"notes":{"type":"string"}},` +
					`"required":["available","version","notes"]}`),
				Handler: func(ctx context.Context, in json.RawMessage) (any, error) {
					res, err := b.CheckForUpdates(ctx)
					if err != nil {
						return nil, err
					}
					return updateCheckOutput{Available: res.Available, Version: res.Version, Notes: res.Notes}, nil
				},
			},
			{
				Name:        "install",
				Description: "Downloads, verifies, and installs the update, then relaunches the app.",
				Permission:  "updates:install",
				Handler: func(ctx context.Context, in json.RawMessage) (any, error) {
					if err := b.InstallUpdate(ctx); err != nil {
						return nil, err
					}
					return nil, nil
				},
			},
		},
	}
}

// updateCheckOutput is updates.check's result.
type updateCheckOutput struct {
	Available bool   `json:"available"`
	Version   string `json:"version"`
	Notes     string `json:"notes"`
}
