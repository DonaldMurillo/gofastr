package desktop

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"
)

// fakeShell is the no-native-code Shell the whole test suite runs
// against. Run records the WindowConfig, calls ready on a goroutine,
// and blocks until Quit (or the test's cleanup closes it).
//
// battery/desktop/desktoptest ships the same double as an exported
// package for app-level tests; it cannot be imported here (an internal
// test package importing it would be an import cycle), so this copy
// stays and TestDesktoptestDoubleMatchesContract in desktoptest_test.go
// pins the two against the same contract.
type fakeShell struct {
	mu sync.Mutex

	runCalled bool
	runCfg    WindowConfig
	runErr    error

	quitCh   chan struct{}
	quitOnce sync.Once

	// Secondary windows (OpenWindow): recorded calls and per-id state.
	openWindowCalls []openWindowCall
	openWindows     map[string]*fakeWindow

	// Tray state: SetTrayTitle's latest value.
	trayTitle string

	promptRequests  []PermissionRequest
	promptDecisions []Decision
	promptErr       error

	clipText   string
	clipReads  int
	clipWrites []string

	openFileResult []string
	openFileErr    error
	saveFileResult string
	saveFileErr    error
	folderResult   string
	folderErr      error
	dialogCalls    int

	notifications []Notification
	notifyErr     error
}

func newFakeShell() *fakeShell {
	return &fakeShell{quitCh: make(chan struct{})}
}

func (f *fakeShell) Run(ctx context.Context, w WindowConfig, ready func(Window)) error {
	f.mu.Lock()
	if f.runCalled {
		f.mu.Unlock()
		return fmt.Errorf("fakeShell: Run called twice")
	}
	f.runCalled = true
	f.runCfg = w
	if f.openWindows == nil {
		f.openWindows = make(map[string]*fakeWindow)
	}
	win := &fakeWindow{shell: f, id: MainWindowID, title: w.Title}
	f.openWindows[MainWindowID] = win
	f.mu.Unlock()

	go ready(win)

	select {
	case <-f.quitCh:
		return f.runErr
	case <-ctx.Done():
		return ctx.Err()
	}
}

// openWindowCall records one OpenWindow.
type openWindowCall struct {
	id   string
	spec WindowSpec
	url  string
}

func (f *fakeShell) OpenWindow(id string, spec WindowSpec, url string) (Window, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.openWindows == nil {
		f.openWindows = make(map[string]*fakeWindow)
	}
	f.openWindowCalls = append(f.openWindowCalls, openWindowCall{id: id, spec: spec, url: url})
	if w, ok := f.openWindows[id]; ok {
		return w, nil
	}
	w := &fakeWindow{shell: f, id: id, title: spec.Title}
	f.openWindows[id] = w
	return w, nil
}

func (f *fakeShell) SetTrayTitle(title string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.trayTitle = title
	return nil
}

func (f *fakeShell) Quit() { f.quitOnce.Do(func() { close(f.quitCh) }) }

func (f *fakeShell) Main(fn func()) error {
	fn()
	return nil
}

func (f *fakeShell) Prompt(ctx context.Context, req PermissionRequest) (Decision, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.promptRequests = append(f.promptRequests, req)
	if f.promptErr != nil {
		return DecisionDeny, f.promptErr
	}
	if len(f.promptDecisions) == 0 {
		return DecisionDeny, nil
	}
	d := f.promptDecisions[0]
	f.promptDecisions = f.promptDecisions[1:]
	return d, nil
}

func (f *fakeShell) Clipboard() Clipboard { return f }
func (f *fakeShell) Dialogs() Dialogs     { return f }
func (f *fakeShell) Notifier() Notifier   { return f }

func (f *fakeShell) ReadText(context.Context) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.clipReads++
	return f.clipText, nil
}

func (f *fakeShell) WriteText(_ context.Context, text string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.clipWrites = append(f.clipWrites, text)
	return nil
}

func (f *fakeShell) OpenFile(context.Context, OpenOptions) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.dialogCalls++
	return f.openFileResult, f.openFileErr
}

func (f *fakeShell) SaveFile(context.Context, SaveOptions) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.dialogCalls++
	return f.saveFileResult, f.saveFileErr
}

func (f *fakeShell) OpenFolder(context.Context) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.dialogCalls++
	return f.folderResult, f.folderErr
}

func (f *fakeShell) Show(_ context.Context, n Notification) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.notifyErr != nil {
		return f.notifyErr
	}
	f.notifications = append(f.notifications, n)
	return nil
}

// Accessors for assertions.
func (f *fakeShell) prompts() []PermissionRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]PermissionRequest{}, f.promptRequests...)
}

func (f *fakeShell) config() (WindowConfig, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.runCfg, f.runCalled
}

func (f *fakeShell) notified() []Notification {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]Notification{}, f.notifications...)
}

func (f *fakeShell) clipWritesList() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string{}, f.clipWrites...)
}

// openWindowCallsList snapshots the recorded OpenWindow calls.
func (f *fakeShell) openWindowCallsList() []openWindowCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]openWindowCall{}, f.openWindowCalls...)
}

// trayTitleNow returns the last title SetTrayTitle received.
func (f *fakeShell) trayTitleNow() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.trayTitle
}

// fakeWindow records Navigate/Eval/Focus and answers a fixed 1x1 PNG.
type fakeWindow struct {
	mu        sync.Mutex
	shell     *fakeShell
	id        string
	title     string
	navigated []string
	evaluated []string
	titles    []string
	focuses   int
	closed    bool
	frame     Frame
	setFrames []Frame
}

func (w *fakeWindow) Frame() (Frame, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.frame, nil
}

func (w *fakeWindow) SetFrame(f Frame) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.frame = f
	w.setFrames = append(w.setFrames, f)
	return nil
}

func (w *fakeWindow) ID() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.id
}
func (w *fakeWindow) Focus() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.focuses++
	return nil
}

func (w *fakeWindow) Navigate(url string) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.navigated = append(w.navigated, url)
	return nil
}

func (w *fakeWindow) Eval(js string) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.evaluated = append(w.evaluated, js)
	return nil
}

// png1x1 is a fixed valid 1x1 transparent PNG.
var png1x1 = func() []byte {
	const s = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNkYPhfDwAChwGA60e6kgAAAABJRU5ErkJggg=="
	b, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		panic("bad test PNG fixture: " + err.Error())
	}
	return b
}()

func (w *fakeWindow) Snapshot(context.Context) ([]byte, error) { return png1x1, nil }

func (w *fakeWindow) Title() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.title
}

func (w *fakeWindow) SetTitle(t string) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.title = t
	w.titles = append(w.titles, t)
	return nil
}

func (w *fakeWindow) Native() uintptr { return 0 }

func (w *fakeWindow) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.closed = true
	return nil
}

func (w *fakeWindow) navURLs() []string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]string{}, w.navigated...)
}

func (w *fakeWindow) evals() []string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]string{}, w.evaluated...)
}

// newTestBattery builds a battery around a fake shell with an

// focusCount reports Focus calls.
func (w *fakeWindow) focusCount() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.focuses
}

// in-memory grant store, for handler-level tests that do not need a
// framework.App.
func newTestBattery(t *testing.T) (*Battery, *fakeShell) {
	t.Helper()
	shell := newFakeShell()
	b := New(Config{ID: "test.example.app", Title: "Test", Shell: shell, Logger: testLogger(t)})
	b.grants = newMemGrantStore()
	return b, shell
}

// freezeForTest freezes the registry the way Run would.
func (b *Battery) freezeForTest() { b.reg.freeze() }

// testLogger discards output unless GOFASTR_DESKTOP_TEST_LOG is set.
func testLogger(t *testing.T) *slog.Logger {
	t.Helper()
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// waitTimeout bounds condition polls.
const waitTimeout = 5 * time.Second
