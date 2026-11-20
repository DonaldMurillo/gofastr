// Package native picks the platform shell by GOOS; the gofastr_nodesktop tag leaves every one out.
package native

import (
	"github.com/DonaldMurillo/gofastr/battery/desktop"
)

// New is desktop.New with Shell() as the default when cfg.Shell is nil.
func New(cfg desktop.Config) *desktop.Battery {
	if cfg.Shell == nil {
		cfg.Shell = Shell()
	}
	return desktop.New(cfg)
}
