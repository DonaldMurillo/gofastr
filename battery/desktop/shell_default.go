package desktop

import (
	"context"
	"runtime"
)

type unsupportedShell struct {
	goos   string
	goarch string
}

// NewUnsupportedShell returns the Shell every platform without a native layer answers with.
func NewUnsupportedShell() Shell {
	return &unsupportedShell{goos: runtime.GOOS, goarch: runtime.GOARCH}
}

func (s *unsupportedShell) Run(context.Context, WindowConfig, func(Window)) error {
	return unsupportedErrorf(s.goos, s.goarch)
}

func (s *unsupportedShell) Quit() {}

func (s *unsupportedShell) Main(fn func()) error {
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

func (s *unsupportedShell) Appearance() Appearance { return Appearance{} }

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
