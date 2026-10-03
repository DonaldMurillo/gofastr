// Package native picks the desktop host's platform package by GOOS: the
// one line a host needs to run on the OS's own WebView without naming a
// platform itself.
//
//	native.New(desktop.Config{ID: "dev.gofastr.notes", Title: "Notes"})
//
// is desktop.New with Shell: native.Shell() as the default; a host may
// keep desktop.New and pass Shell: native.Shell() itself, or hand its
// own Shell (the test double) to either. Nothing here registers through
// init: importing the package changes nothing until New or Shell runs.
package native

import (
	"github.com/DonaldMurillo/gofastr/battery/desktop"
)

// New is desktop.New with the platform shell as the nil-Shell default:
// cfg.Shell = Shell() when the caller left it nil, then desktop.New. It
// panics on a bad Config exactly like desktop.New.
func New(cfg desktop.Config) *desktop.Battery {
	if cfg.Shell == nil {
		cfg.Shell = Shell()
	}
	return desktop.New(cfg)
}
