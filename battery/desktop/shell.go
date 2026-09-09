package desktop

import (
	"context"
	"encoding/json"
	"fmt"
)

// Error codes for the bridge's closed set. A handler returning *Error
// maps to its code verbatim; the chokepoint maps every other error to
// internal with a fixed message, logging the real error server-side.
const (
	CodeDenied       = "denied"
	CodeUnsupported  = "unsupported"
	CodeInvalidInput = "invalid_input"
	CodeCancelled    = "cancelled"
	CodeNotFound     = "not_found"
	CodeInternal     = "internal"
	InternalErrorMsg = "internal error"
)

// Error is the typed error the bridge round-trips to the page. Code is
// one of the Code* constants above; Message is safe to show in the
// page (it never carries an internal error chain).
type Error struct {
	Code    string
	Message string
}

func (e *Error) Error() string {
	return "desktop: " + e.Code + ": " + e.Message
}

// ErrUnsupported is returned by Shell implementations (and capabilities
// built on them) when the host OS has no native layer for the operation.
var ErrUnsupported = &Error{Code: CodeUnsupported, Message: "operation is not supported by this desktop host"}

// ErrCancelled is returned when the user cancelled a native dialog (or
// the request context ended before the operation did).
var ErrCancelled = &Error{Code: CodeCancelled, Message: "operation was cancelled"}

// Decision is the user's answer to a PermissionRequest.
type Decision int

const (
	// DecisionDeny refuses the call and persists the refusal, so a page
	// cannot re-prompt in a loop.
	DecisionDeny Decision = iota
	// DecisionAllowOnce permits this single call without persisting a
	// grant.
	DecisionAllowOnce
	// DecisionAllow permits the call and persists the grant.
	DecisionAllow
)

// PermissionRequest describes one permission the chokepoint is about to
// enforce. The Shell renders it as an OS-native alert.
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

// Clipboard is the OS clipboard. Implementations must be safe for use
// from any goroutine; the bridge hops to the UI thread internally where
// the platform demands it.
type Clipboard interface {
	ReadText(ctx context.Context) (string, error)
	WriteText(ctx context.Context, text string) error
}

// Dialogs is the OS file/folder picker family. A cancelled dialog
// returns ("", nil-with-ErrCancelled) or (nil, ErrCancelled): the
// caller maps that to the bridge's cancelled code.
type Dialogs interface {
	OpenFile(ctx context.Context, opts OpenOptions) ([]string, error)
	SaveFile(ctx context.Context, opts SaveOptions) (string, error)
	OpenFolder(ctx context.Context) (string, error)
}

// Notifier shows OS notifications.
type Notifier interface {
	Show(ctx context.Context, n Notification) error
}

// WindowMaterial selects the native effect under a window's page. The
// zero value is the opaque default; the shell picks the mechanism per
// platform (glass on macOS 26, an NSVisualEffectView below it, Mica on
// Windows 11, none on Linux). A material only shows where the page
// paints transparent (html and body); the doc section spells the page
// contract.
type WindowMaterial string

const (
	// MaterialNone is the opaque window (default).
	MaterialNone WindowMaterial = ""
	// MaterialSidebar puts the effect under the sidebar zone only.
	MaterialSidebar WindowMaterial = "sidebar"
	// MaterialWindow puts the effect under the whole window.
	MaterialWindow WindowMaterial = "window"
	// MaterialGlass asks for macOS 26 glass; below 26 it degrades to
	// MaterialWindow (the shell logs the degradation).
	MaterialGlass WindowMaterial = "glass"
)

// Inset is an offset in screen points from a default position.
type Inset struct{ X, Y int }

// WindowChrome selects a window's title-bar treatment.
type WindowChrome int

const (
	// ChromeDefault is the titled window with traffic lights.
	ChromeDefault WindowChrome = iota
	// ChromeHiddenTitle keeps the traffic lights and lets the page
	// paint under the title bar (fullSizeContentView plus a
	// transparent, title-hidden title bar on darwin).
	ChromeHiddenTitle
	// ChromeNone is borderless: no title bar, no traffic lights, no
	// resize box. The page drags the window through
	// data-fui-window-drag.
	ChromeNone
	// ChromeUnified is the Notes and Finder shape: a transparent title
	// bar with a hidden title and a unified toolbar style (an empty
	// NSToolbar attached on darwin so toolbarStyle takes effect).
	ChromeUnified
)

// MaxSidebarWidth bounds a sidebar zone in screen points on both sides
// of the contract (Config, WindowSpec, window.setChrome).
const MaxSidebarWidth = 4096

// WindowStyle describes the native chrome of a window beyond its
// content. The zero value is the plain titled window. Hosts without a
// native layer ignore it.
type WindowStyle struct {
	// Chrome selects the title-bar treatment (ChromeDefault keeps the
	// host's standard window).
	Chrome WindowChrome
	// Float keeps the window above normal windows (NSFloatingWindowLevel
	// on darwin).
	Float bool
	// Panel makes the window a non-activating panel: clicking it does
	// not steal focus from the frontmost app. Implies Float.
	Panel bool
	// Transparent clears the window background: the page paints its own
	// surface (a ui.Card, say) and the corners are see-through.
	Transparent bool
	// Resizable toggles the resize affordance; nil means true.
	Resizable *bool
	// AllSpaces shows the window on every Space / desktop.
	AllSpaces bool
	// X and Y are the initial origin in screen points, measured from
	// the primary screen's top-left. Both must be set together; nil
	// means the host centers the window.
	X, Y *int
	// Material selects the native effect under the page (the
	// WindowMaterial constants). MaterialNone (the zero value) is the
	// opaque default.
	Material WindowMaterial
	// TrafficLightInset moves the three window buttons (close,
	// minimize, zoom) from the system position on darwin; nil keeps
	// it. Both values must be >= 0. The page reserves space for the
	// buttons (the theme's spacing token), and the shell re-applies
	// the geometry whenever AppKit re-lays the title bar out.
	TrafficLightInset *Inset
}

// Frame is a window's position and size in screen points. X and Y
// locate the top-left corner in the global top-left coordinate space
// (the WindowStyle.X/Y convention); Width and Height size the window.
type Frame struct {
	X, Y, Width, Height int
}

// WindowSpec describes a secondary window showing one of the app's own
// screens. Path is a same-origin absolute path (validated like a menu
// Navigate path); Title, Width, and Height default per host when zero.
// Style is the optional window chrome (WindowStyle's zero value is the
// host's standard window).
type WindowSpec struct {
	Path   string
	Title  string
	Width  int
	Height int
	Style  WindowStyle
	// Frame is the window's initial frame; nil uses the host default
	// (centered). A Frame from the window store replaces a nil one, so
	// a remembered window reopens where the user left it.
	Frame *Frame
	// SidebarWidth is this window's initial sidebar zone in screen
	// points (0 means no zone); the page updates it through
	// window.setChrome as its layout changes.
	SidebarWidth int
}

// WindowConfig describes the main window the desktop host opens and

// Tray puts the app in the menu bar (a status item on macOS). The
// menu uses the same MenuItem model as Config.Menu: Navigate |
// Handler | Role, validated together with the main menu so item ids
// stay unique across both.
type Tray struct {
	// Title is the text in the menu bar (may be empty when Icon is
	// set).
	Title string
	// Tooltip is the hover help on the tray item.
	Tooltip string
	// Icon is a PNG rendered as a template image at 18x18 points.
	Icon []byte
	// Menu is the tray menu; same model as Config.Menu.
	Menu *Menu
	// CloseHidesWindow makes the main window's close button hide the
	// window instead of quitting; the app stays alive in the menu bar
	// and RoleShow (or the tray icon's own click) brings it back.
	CloseHidesWindow bool
	// HideDock removes the Dock icon (activation policy Accessory).
	HideDock bool
}

// the shell-level extras that hang off it.
type WindowConfig struct {
	Title  string
	Width  int
	Height int
	// Style is the main window's chrome; the zero value is the host's
	// standard window.
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
	OnSettings func()
	// OnMenu is called by the shell when the user activates a menu item
	// carrying a Navigate path or a Handler. Role items ("quit",
	// "about", "settings", "show", "separator") are handled by the
	// shell itself and are never dispatched through OnMenu.
	OnMenu func(id string)
	// OnWindowClosed is called by the shell (on a goroutine) when a
	// secondary window the user closed goes away, with the id OpenWindow
	// assigned. The main window never reports through it.
	OnWindowClosed func(id string)
	// Frame is the main window's initial frame; nil uses the host
	// default (centered).
	Frame *Frame
	// OnWindowFrame is called by the shell (on a goroutine) whenever
	// the user moves or resizes a window, unthrottled: the host
	// debounces. It never fires for windows created after Run.
	OnWindowFrame func(id string, f Frame)
	// SidebarWidth is the main window's initial sidebar zone in screen
	// points (0 means no zone); the battery fills it from
	// Config.SidebarWidth and the page updates it through
	// window.setChrome.
	SidebarWidth int
	// OnWindowFocus is called by the shell (on a goroutine) when a
	// window becomes key (windowDidBecomeKey: on darwin).
	OnWindowFocus func(id string)
	// OnWindowBlur is called by the shell (on a goroutine) when a
	// window resigns key (windowDidResignKey: on darwin).
	OnWindowBlur func(id string)
	// OnAppearance is called by the shell (on a goroutine, any thread)
	// when an appearance setting the page cannot see through CSS
	// changes. It fires on change only; the boot marker carries the
	// initial value.
	OnAppearance func(a Appearance)
	// OnDeepLink is called by the shell (on a goroutine) with the raw
	// URL the OS asked the app to open (a custom URL scheme). It may be
	// called before ready fired; the battery queues until the window is
	// up.
	OnDeepLink func(rawURL string)
}

// Appearance is the accessibility and appearance state the page cannot
// read through CSS: WebKit implements no prefers-reduced-transparency
// query, so the shell reads NSWorkspace on darwin and pushes changes
// through the OnAppearance callback and the boot marker.
type Appearance struct {
	// ReduceTransparency is the user's Reduce Transparency setting.
	ReduceTransparency bool
}

// Window is the native window + web view. Every method may be called
// from any goroutine; a platform implementation hops to the UI thread
// internally and must not block it.
type Window interface {
	// ID is "main" for the first window, then the id the battery
	// assigned the secondary window ("settings", "w2", ...).
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
	// Native returns the raw native web view handle (WKWebView*,
	// ICoreWebView2*, WebKitWebView*). It is the documented escape
	// hatch; treat it as opaque unless you know your platform.
	Native() uintptr
	// Frame reads the window's current frame (top-left screen points).
	Frame() (Frame, error)
	// SetFrame moves and sizes the window.
	SetFrame(f Frame) error
	// SetSidebarWidth resizes the window's sidebar zone (the effect
	// area under MaterialSidebar) to points; 0 removes the zone. The
	// page's setChrome capability is the usual caller.
	SetSidebarWidth(points int) error
	// Close closes the window.
	Close() error
}

// Shell is the OS abstraction the whole battery is written against.
// One implementation per platform arrives behind build tags; tests run
// against a fake with no native code at all.
//
// Threading contract for implementers:
//
//   - Run owns the process's UI thread: it blocks until the window
//     closes or Quit is called, and calls ready exactly once, on the
//     UI thread, once the window (and its web view) exist.
//   - Main runs fn on the UI thread and waits for it to finish. It must
//     enforce a deadline internally so a hung call cannot freeze the
//     window forever; a timed-out Main returns an error.
//   - All other methods (Prompt, Clipboard, Dialogs, Notifier, and the
//     Window methods) are called from HTTP goroutines and must be safe
//     for concurrent use; they hop to the UI thread via Main
//     internally.
//   - Methods receiving a context should honour its cancellation where
//     the platform allows: a cancelled dialog resolves to
//     ErrCancelled, not to a hang.
type Shell interface {
	// Run brings up the window described by w and blocks in the OS
	// event loop until the window closes or Quit is called. ready is
	// invoked exactly once, on the UI thread, with the live Window; the
	// battery navigates it to the boot URL from there.
	Run(ctx context.Context, w WindowConfig, ready func(Window)) error
	// OpenWindow opens a secondary window with the given id and spec,
	// navigated to url, sharing the main window's cookie store (the
	// same web view configuration), and returns it. Called after Run's
	// ready callback fired.
	OpenWindow(id string, spec WindowSpec, url string) (Window, error)
	// Quit ends the event loop Run is blocked in. It must be safe to
	// call before Run starts and after it returns.
	Quit()
	// SetTrayTitle changes the tray item's title. ErrUnsupported when
	// this host has no tray.
	SetTrayTitle(title string) error
	// Prompt shows the OS permission alert and returns the user's
	// decision. ctx cancellation should dismiss the dialog and return
	// an error.
	Prompt(ctx context.Context, req PermissionRequest) (Decision, error)
	// Clipboard returns the clipboard surface.
	Clipboard() Clipboard
	// Dialogs returns the file/folder dialog surface.
	Dialogs() Dialogs
	// Notifier returns the notification surface.
	Notifier() Notifier
	// Appearance returns the current appearance state (the settings
	// the page cannot read through CSS). Callable at any time,
	// including before Run: a shell without a native layer answers the
	// zero value.
	Appearance() Appearance
}

// PageEvaluator is implemented by a Window that can run a script in its
// page and hand back the result. body is the body of an async JavaScript
// function (it may use await and must `return` its result); the result
// is the JSON encoding of the returned value ("null" for undefined). A
// thrown error or a rejected promise is returned as an *Error with
// CodeInternal whose Message is the error's message. The darwin window
// implements it through WKWebView's callAsyncJavaScript, which awaits
// promises natively.
type PageEvaluator interface {
	EvalAsync(ctx context.Context, body string) (json.RawMessage, error)
}

// NativeDriver is the optional test surface a real shell exposes: the
// user's hands on the native UI and eyes on the native state. The fake
// shell exposes the same operations through desktoptest; a shell that
// cannot perform one returns ErrUnsupported.
type NativeDriver interface {
	// ActivateMenu fires a main-menu item by its title path; a role
	// item matches its role name when untitled.
	ActivateMenu(titles ...string) error
	// ActivateTray fires a tray menu row by title (role name when
	// untitled).
	ActivateTray(title string) error
	// OpenSettingsItem fires the app menu's own Settings item, the
	// one the shell synthesizes for Config.Settings.
	OpenSettingsItem() error
	// ScriptPrompts answers the next permission alerts without
	// showing them, consuming the decisions in order; an empty queue
	// means the real alert shows.
	ScriptPrompts(ds ...Decision)
	// ClickPrompt clicks a button on the modal alert that is up (the
	// real alert path), waiting for it to appear.
	ClickPrompt(button string) error
	// PostDeepLink delivers a URL the way the OS does (the GetURL
	// Apple Event on darwin).
	PostDeepLink(rawURL string) error
	// CloseWindowNative is the red button on that window:
	// performClose, or the windowShouldClose path when the window has
	// no close box.
	CloseWindowNative(id string) error
	// WindowState reads what the OS knows about a window.
	WindowState(id string) (WindowState, error)
	// DeactivateReactivate deactivates and reactivates the app the way
	// a user's app switch does ([NSApp deactivate] then
	// activateIgnoringOtherApps: on darwin): the key window must
	// resign and become key again, firing the focus callbacks.
	DeactivateReactivate() error
	// NotificationLog is every Notification Show received, bundled or
	// not, in order.
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
	// no content view). Window.Snapshot renders the web view on its
	// own, so this is the fact that says the page is on screen.
	ContentClass string
	// X and Y are the window's top-left corner in screen points
	// (top-left based), the Frame contract's coordinates.
	X, Y   int
	Width  int
	Height int
	// Material is what the shell actually applied under the page, read
	// from the live view tree: "none", "vibrancy-sidebar",
	// "vibrancy-window", or "glass" (macOS 26 only). The configured
	// Material may degrade (glass below macOS 26 answers
	// "vibrancy-window").
	Material string
	// TitlebarTransparent reports the titlebarAppearsTransparent fact.
	TitlebarTransparent bool
	// ToolbarStyle names the window's toolbar style when a toolbar is
	// attached ("unified", "expanded", ...); "" when there is none.
	ToolbarStyle string
	// SidebarWidth is the sidebar zone's width in screen points as the
	// live zone view is sized; 0 when the window has no zone.
	SidebarWidth int
	// CGWindowID is the window's CoreGraphics id (NSWindow's
	// windowNumber on darwin), the id screencapture -l and the window
	// services speak. 0 when the platform has none.
	CGWindowID int64
}

// MainWindowID is the id the main window carries; the battery assigns
// "settings" and "w2", "w3", ... to the secondaries it opens, and
// windows.close refuses this one. A platform shell labels the window
// its Run opens with it (the Window.ID contract); it is exported so
// the platform packages (battery/desktop/macos, ...) share one source
// of truth with the battery and the test double instead of pinning the
// literal again per package.
const MainWindowID = "main"

// unsupportedErrorf builds an *Error naming the platform.
func unsupportedErrorf(goos, goarch string) *Error {
	return &Error{
		Code:    CodeUnsupported,
		Message: fmt.Sprintf("no desktop shell is implemented for GOOS=%s GOARCH=%s", goos, goarch),
	}
}

// Widget builds the WindowSpec of a floating widget: a borderless,
// non-activating, transparent panel of w x h points showing one of the
// app's own screens, dragged by whatever the page marks
// data-fui-window-drag. Panel implies Float, so the widget stays above
// the app's normal windows.
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
