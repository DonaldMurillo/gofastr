package desktop

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/DonaldMurillo/gofastr/core/i18n"
)

// Error codes for the bridge's closed set.
const (
	CodeDenied       = "denied"
	CodeUnsupported  = "unsupported"
	CodeInvalidInput = "invalid_input"
	CodeCancelled    = "cancelled"
	CodeNotFound     = "not_found"
	CodeInternal     = "internal"
	InternalErrorMsg = "internal error"
)

// Error is the typed error the bridge round-trips to the page.
type Error struct {
	Code    string
	Message string
}

func (e *Error) Error() string {
	return "desktop: " + e.Code + ": " + e.Message
}

// ErrUnsupported is returned when the host OS has no native layer for the operation.
var ErrUnsupported = &Error{Code: CodeUnsupported, Message: "operation is not supported by this desktop host"}

// ErrCancelled is returned when the user cancelled a native dialog or the request ended first.
var ErrCancelled = &Error{Code: CodeCancelled, Message: "operation was cancelled"}

// ErrWindowClosed is returned when a native operation targets a closed window.
var ErrWindowClosed = &Error{Code: CodeNotFound, Message: "window is closed"}

// Decision is the user's answer to a PermissionRequest.
type Decision int

const (
	// DecisionDeny refuses the call and persists the refusal, so a page cannot re-prompt in a loop.
	DecisionDeny Decision = iota
	// DecisionAllowOnce permits this single call without persisting a grant.
	DecisionAllowOnce
	// DecisionAllow permits the call and persists the grant.
	DecisionAllow
)

// PermissionRequest describes one permission the chokepoint is about to enforce.
type PermissionRequest struct {
	Capability  string
	Method      string
	Permission  string
	Description string
}

// Notification is one user-facing notification.
type Notification struct {
	Title    string
	Subtitle string
	Body     string
}

// FileFilter is one entry in an open-file dialog's type filter.
type FileFilter struct {
	Name       string
	Extensions []string // without the leading dot, e.g. "png"
}

// OpenOptions configures an open-file dialog.
type OpenOptions struct {
	Filters  []FileFilter
	Multiple bool
}

// SaveOptions configures a save-file dialog.
type SaveOptions struct {
	DefaultName string
}

// Clipboard is the OS clipboard.
type Clipboard interface {
	ReadText(ctx context.Context) (string, error)
	WriteText(ctx context.Context, text string) error
}

// Dialogs is the OS file/folder picker family.
type Dialogs interface {
	OpenFile(ctx context.Context, opts OpenOptions) ([]string, error)
	SaveFile(ctx context.Context, opts SaveOptions) (string, error)
	OpenFolder(ctx context.Context) (string, error)
}

// Notifier shows OS notifications.
type Notifier interface {
	Show(ctx context.Context, n Notification) error
}

// WindowMaterial selects the native effect under a window's page.
type WindowMaterial string

const (
	// MaterialNone is the opaque window (default).
	MaterialNone WindowMaterial = ""
	// MaterialSidebar puts the effect under the sidebar zone where the
	// platform supports it; Windows uses whole-window Mica Alt.
	MaterialSidebar WindowMaterial = "sidebar"
	// MaterialWindow puts the effect under the whole window.
	MaterialWindow WindowMaterial = "window"
	// MaterialGlass asks for macOS 26 glass or Windows Desktop Acrylic.
	MaterialGlass WindowMaterial = "glass"
)

// Inset is an offset in screen points from a default position.
type Inset struct{ X, Y int }

// WindowChrome selects a window's title-bar treatment.
type WindowChrome int

const (
	// ChromeDefault keeps the host's standard titled window and controls.
	ChromeDefault WindowChrome = iota
	// ChromeHiddenTitle keeps the host's caption controls and lets the
	// page paint under the title bar where the platform supports it.
	ChromeHiddenTitle
	// ChromeNone is borderless: no title bar, no traffic lights, no resize box.
	ChromeNone
	// ChromeUnified is the Notes and Finder shape: a transparent title
	// bar with a hidden title and unified page content.
	ChromeUnified
)

// MaxSidebarWidth bounds a sidebar zone in screen points.
const MaxSidebarWidth = 4096

// ValidateSidebarWidth checks the sidebar zone width in screen points.
func ValidateSidebarWidth(points int) *Error {
	if points < 0 || points > MaxSidebarWidth {
		return &Error{Code: CodeInvalidInput, Message: fmt.Sprintf("sidebarWidth must be between 0 and %d", MaxSidebarWidth)}
	}
	return nil
}

// WindowStyle describes the native chrome of a window beyond its content.
type WindowStyle struct {
	// Chrome selects the title-bar treatment (ChromeDefault keeps the host's standard window).
	Chrome WindowChrome
	// Float keeps the window above normal windows (NSFloatingWindowLevel on darwin).
	Float bool
	// Panel makes the window a non-activating panel: clicking it does not steal focus from the frontmost app.
	Panel bool
	// Transparent clears the window background: the page paints its own
	// surface (a ui.Card, say) and the corners are see-through.
	Transparent bool
	// Resizable toggles the resize affordance; nil means true.
	Resizable *bool
	// AllSpaces shows the window on every Space / desktop.
	AllSpaces bool
	// X and Y are the initial origin in screen points, measured from the primary screen's top-left.
	X, Y *int
	// Material selects the native effect under the page (the WindowMaterial constants).
	Material WindowMaterial
	// TrafficLightInset moves the darwin window buttons from the system position; nil keeps it.
	TrafficLightInset *Inset
}

// Frame is a window's position and size in screen points.
type Frame struct {
	X, Y, Width, Height int
}

// WindowSpec describes a secondary window showing one of the app's own screens.
type WindowSpec struct {
	Path   string
	Title  string
	Width  int
	Height int
	Style  WindowStyle
	// Frame is the window's initial frame; nil uses the host default (centered).
	Frame *Frame
	// SidebarWidth is this window's initial sidebar zone in points; 0 means none.
	SidebarWidth int
}

// Tray puts the app in the menu bar (a status item on macOS).
type Tray struct {
	// Title is the text in the menu bar (may be empty when Icon is set).
	Title string
	// Tooltip is the hover help on the tray item, in the OS language the shell reads.
	Tooltip i18n.Phrase
	// Icon is a PNG.
	Icon []byte
	// Menu is the tray menu; same model as Config.Menu.
	Menu *Menu
	// CloseHidesWindow makes the main window's close button hide it instead of quitting.
	CloseHidesWindow bool
	// HideDock removes the macOS Dock icon (activation policy Accessory).
	HideDock bool
}

// WindowConfig describes the main window and the callbacks the shell reports through.
type WindowConfig struct {
	// AppID is the stable application identity used for per-app embedded browser data.
	AppID  string
	Title  string
	Width  int
	Height int
	// Style is the main window's chrome; the zero value is the host's standard window.
	Style WindowStyle
	// Menu is the validated menu tree (IDs assigned); may be nil.
	Menu *Menu
	// Tray is the validated tray configuration; nil means no tray.
	Tray *Tray
	// Settings, when set, makes the shell add the app-menu "Settings…"
	// item (cmd+, on darwin) and route it to OnSettings.
	Settings *WindowSpec
	// OnSettings is called by the shell (on a goroutine) when the
	// app-menu Settings item or a RoleSettings menu item fires.
	OnSettings func(ctx context.Context)
	// OnMenu is called by the shell when the user activates a menu item carrying a Navigate path or a Handler.
	OnMenu func(id string)
	// OnTrayMenu is optional synchronous tray dispatch.
	OnTrayMenu func(id string)
	// AllowNavigation is asked, on the UI thread, before any window's
	// main frame navigates to url; false cancels the navigation.
	AllowNavigation func(url string) bool
	// OnWindowClosed is called by the shell (on a goroutine) when a
	// secondary window the user closed goes away, with the id OpenWindow
	// assigned.
	OnWindowClosed func(id string)
	// Frame is the main window's initial frame; nil uses the host default (centered).
	Frame *Frame
	// OnWindowFrame is called on a goroutine, unthrottled, when the user moves or resizes the main or a secondary window.
	OnWindowFrame func(id string, f Frame)
	// OnWindowFrameFrom identifies the exact window through its comparable handle.
	// Shells should use WindowFrameReporter to support both callback forms.
	OnWindowFrameFrom func(w Window, f Frame)
	SidebarWidth      int
	// OnWindowFocus is called by the shell (on a goroutine) when a window becomes key (windowDidBecomeKey: on darwin).
	OnWindowFocus func(id string)
	// OnWindowBlur is called by the shell (on a goroutine) when a window resigns key (windowDidResignKey: on darwin).
	OnWindowBlur func(id string)
	// OnAppearance is called by the shell (on a goroutine, any thread)
	// when an appearance setting the page cannot see through CSS
	// changes.
	OnAppearance func(a Appearance)
	// OnDeepLink is called by the shell (on a goroutine) with the raw
	// URL the OS asked the app to open (a custom URL scheme).
	OnDeepLink func(rawURL string)
}

// WindowFrameReporter returns the configured frame callback, or nil if neither
// callback is set. Shells call it off the UI thread with the reporting window.
func (cfg WindowConfig) WindowFrameReporter() func(Window, Frame) {
	if cfg.OnWindowFrameFrom != nil {
		return cfg.OnWindowFrameFrom
	}
	cb := cfg.OnWindowFrame
	if cb == nil {
		return nil
	}
	return func(w Window, f Frame) { cb(w.ID(), f) }
}

// Appearance is the appearance state the page cannot read through CSS.
type Appearance struct {
	// ReduceTransparency is the user's Reduce Transparency setting.
	ReduceTransparency bool
}

// Window is the native window + web view.
type Window interface {
	// ID is "main" for the first window, then the id the battery assigned the secondary window ("settings", "w2", ...).
	ID() string
	// Navigate loads a URL in the web view.
	Navigate(url string) error
	// Eval evaluates a JavaScript expression in the page.
	Eval(js string) error
	// Snapshot captures the rendered page as PNG bytes.
	Snapshot(ctx context.Context) ([]byte, error)
	// Title returns the current window title.
	Title() string
	// SetTitle changes the window title.
	SetTitle(title string) error
	// Focus brings the window to front and activates the app.
	Focus() error
	// Native returns the raw native web view handle (WKWebView*, ICoreWebView2*, WebKitWebView*).
	Native() uintptr
	// Frame reads the window's current frame (top-left screen points).
	Frame() (Frame, error)
	// SetFrame moves and sizes the window.
	SetFrame(f Frame) error
	// SetSidebarWidth resizes the window's sidebar zone (the effect
	// area under MaterialSidebar) to points; 0 removes the zone.
	SetSidebarWidth(points int) error
	// Close closes the window.
	Close() error
}

// Shell is the OS layer: Run owns the UI thread, every other method is safe from any goroutine.
type Shell interface {
	// Run brings up the window described by w and blocks in the OS
	// event loop until the window closes or Quit is called.
	Run(ctx context.Context, w WindowConfig, ready func(Window)) error
	// OpenWindow opens a secondary window on url that shares the main window's cookie store.
	OpenWindow(id string, spec WindowSpec, url string) (Window, error)
	// Quit ends the event loop Run is blocked in.
	Quit()
	// SetTrayTitle changes the tray item's title.
	SetTrayTitle(title string) error
	// Prompt shows the OS permission alert and returns the user's decision.
	Prompt(ctx context.Context, req PermissionRequest) (Decision, error)
	// Clipboard returns the clipboard surface.
	Clipboard() Clipboard
	// Dialogs returns the file/folder dialog surface.
	Dialogs() Dialogs
	// Notifier returns the notification surface.
	Notifier() Notifier
	// Appearance returns the current appearance state (the settings the page cannot read through CSS).
	Appearance() Appearance
}

// PageEvaluator is implemented by a Window that can run a script in its page and hand back the result.
type PageEvaluator interface {
	EvalAsync(ctx context.Context, body string) (json.RawMessage, error)
}

// NativeDriver is the optional test surface a real shell exposes to drive and read its native UI.
type NativeDriver interface {
	// ActivateMenu fires a main-menu item by its title path; a role item matches its role name when untitled.
	ActivateMenu(titles ...string) error
	// ActivateTray fires a tray menu row by title (role name when untitled).
	ActivateTray(title string) error
	// OpenSettingsItem fires the app menu's own Settings item, the one the shell synthesizes for Config.Settings.
	OpenSettingsItem() error
	// ScriptPrompts answers the next permission alerts in order without showing them.
	ScriptPrompts(ds ...Decision)
	// ClickPrompt clicks a button on the modal alert that is up (the real alert path), waiting for it to appear.
	ClickPrompt(button string) error
	// PostDeepLink delivers a URL the way the OS does (the GetURL Apple Event on darwin).
	PostDeepLink(rawURL string) error
	// CloseWindowNative closes the window the way its close button does.
	CloseWindowNative(id string) error
	// WindowState reads what the OS knows about a window.
	WindowState(id string) (WindowState, error)
	// MakeKey makes the window the key window, firing the focus callbacks, without activating the app.
	MakeKey(id string) error
	// NotificationLog is every Notification Show received, bundled or not, in order.
	NotificationLog() []Notification
}

// WindowState is what the OS knows about a window.
type WindowState struct {
	Title     string
	Visible   bool
	Key       bool
	Level     int
	StyleMask uint64
	Class     string // "NSWindow", "NSPanel"
	// ContentClass is the window's content view class ("WKWebView"
	// when the page is actually in the window; "" when the window has
	// no content view).
	ContentClass string
	// X and Y are the window's top-left corner in screen points (top-left based), the Frame contract's coordinates.
	X, Y   int
	Width  int
	Height int
	// Material is the effect the shell applied under the page.
	Material string
	// TitlebarTransparent reports the titlebarAppearsTransparent fact.
	TitlebarTransparent bool
	// ToolbarStyle names the window's toolbar style when a toolbar is
	// attached ("unified", "expanded", ...); "" when there is none.
	ToolbarStyle string
	// TitleVisibility reports the effective title visibility
	// ("visible" or "hidden"; "" when the platform does not expose
	// it).
	TitleVisibility string
	// SidebarWidth is the sidebar zone's width in screen points as the
	// live zone view is sized; 0 when the window has no zone.
	SidebarWidth int
	// CGWindowID is the window's CoreGraphics window number; 0 where the platform has none.
	CGWindowID int64
}

// MainWindowID is the id of the main window, which windows.close refuses.
const MainWindowID = "main"

func unsupportedErrorf(goos, goarch string) *Error {
	return &Error{
		Code:    CodeUnsupported,
		Message: fmt.Sprintf("no desktop shell is implemented for GOOS=%s GOARCH=%s", goos, goarch),
	}
}

// Widget builds the WindowSpec of a borderless, non-activating, transparent floating panel of w x h points.
func Widget(path string, w, h int) WindowSpec {
	return WindowSpec{
		Path:   path,
		Width:  w,
		Height: h,
		Style: WindowStyle{
			Chrome:      ChromeNone,
			Panel:       true,
			Transparent: true,
		},
	}
}
