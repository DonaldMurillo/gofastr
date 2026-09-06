//go:build !(darwin && arm64)

package desktop

import (
	"context"
	"runtime"
)

// The unsupported shell: every platform without a native layer. The
// darwin/arm64 build replaces this file with shell_darwin.go (and its
// companions) behind //go:build darwin && arm64.

// unsupportedShell is the default Shell. Every interesting method
// returns ErrUnsupported naming the platform, so an app that runs on an
// OS without a native layer fails with a real, named error instead of a
// nil-pointer panic.
type unsupportedShell struct {
	goos   string
	goarch string
}

// newDefaultShell returns the Shell a Config without Shell gets: the
// unsupported one everywhere except darwin/arm64.
func newDefaultShell() Shell {
	return &unsupportedShell{goos: runtime.GOOS, goarch: runtime.GOARCH}
}

func (s *unsupportedShell) Run(context.Context, WindowConfig, func(Window)) error {
	return unsupportedErrorf(s.goos, s.goarch)
}

func (s *unsupportedShell) Quit() {}

func (s *unsupportedShell) Main(fn func()) error {
	// There is no UI thread to hop to; run inline so callers that only
	// need sequencing keep working, and report the platform honestly
	// everywhere else.
	fn()
	return nil
}

func (s *unsupportedShell) OpenWindow(string, WindowSpec, string) (Window, error) {
	return nil, unsupportedErrorf(s.goos, s.goarch)
}

func (s *unsupportedShell) SetTrayTitle(string) error {
	return unsupportedErrorf(s.goos, s.goarch)
}

func (s *unsupportedShell) Prompt(context.Context, PermissionRequest) (Decision, error) {
	return DecisionDeny, unsupportedErrorf(s.goos, s.goarch)
}

func (s *unsupportedShell) Clipboard() Clipboard { return s }
func (s *unsupportedShell) Dialogs() Dialogs     { return s }
func (s *unsupportedShell) Notifier() Notifier   { return s }

func (s *unsupportedShell) ReadText(context.Context) (string, error) {
	return "", unsupportedErrorf(s.goos, s.goarch)
}

func (s *unsupportedShell) WriteText(context.Context, string) error {
	return unsupportedErrorf(s.goos, s.goarch)
}

func (s *unsupportedShell) OpenFile(context.Context, OpenOptions) ([]string, error) {
	return nil, unsupportedErrorf(s.goos, s.goarch)
}

func (s *unsupportedShell) SaveFile(context.Context, SaveOptions) (string, error) {
	return "", unsupportedErrorf(s.goos, s.goarch)
}

func (s *unsupportedShell) OpenFolder(context.Context) (string, error) {
	return "", unsupportedErrorf(s.goos, s.goarch)
}

func (s *unsupportedShell) Show(context.Context, Notification) error {
	return unsupportedErrorf(s.goos, s.goarch)
}
