package desktop

import (
	"context"
	"runtime"
)

// The unsupported shell: the answer for every platform without a native
// layer. It is exported (NewUnsupportedShell) because the platform
// packages' New() hand it back off their platform: battery/desktop/macos
// outside darwin/arm64, battery/desktop/windows and battery/desktop/linux
// everywhere today. desktop.New also picks it when Config.Shell is nil;
// the real shell for a host is native.Shell()'s to choose
// (battery/desktop/native).

// unsupportedShell is the no-native-layer Shell. Every interesting
// method returns ErrUnsupported naming the platform, so an app that runs
// on an OS without a native layer fails with a real, named error instead
// of a nil-pointer panic.
type unsupportedShell struct {
	goos   string
	goarch string
}

// NewUnsupportedShell returns the Shell every platform without a native
// layer answers with. Platform packages return it from their New()
// off-platform; desktop.New selects it when Config.Shell is nil.
func NewUnsupportedShell() Shell {
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
