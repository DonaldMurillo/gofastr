package desktop

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/DonaldMurillo/gofastr/battery/desktop/appstate"
	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/framework/uihost"
)

// validateAppID enforces the reverse-DNS app id grammar.
var reAppID = regexp.MustCompile(`^[A-Za-z0-9.-]+$`)

func validateAppID(id string) error {
	if !reAppID.MatchString(id) || !strings.Contains(id, ".") {
		return fmt.Errorf("desktop: app id %q must be reverse-DNS: [A-Za-z0-9.-]+ with at least one dot", id)
	}
	// The id becomes a PATH SEGMENT under the OS user config dir
	// (DataDir joins it onto the base), and ".", "..", "..." and
	// "..-.." all satisfy the grammar AND the "contains a dot" rule.
	// DataDir("..") then resolves to the PARENT of the base and creates
	// it, so app.db, the 0600 session secret and the identity file that
	// IS the local owner id all land outside the app's own directory -
	// and `gofastr desktop build --id ..` bakes that into
	// CFBundleIdentifier, so a built bundle does it on every launch.
	// The grammar excludes "/", so this is the whole escape surface:
	// an id made of nothing but dots and dashes.
	if strings.Trim(id, ".-") == "" {
		return fmt.Errorf("desktop: app id %q must not be a bare dot/dash sequence: "+
			"it names a directory under the OS user config dir", id)
	}
	if len(id) > 253 {
		return fmt.Errorf("desktop: app id %q is longer than 253 characters", id)
	}
	return nil
}

// Config constructs a desktop Battery. The zero value is invalid: ID
// is required. New panics on a bad Config (relay's posture: a wiring
// error you want at construction, not at first window).
type Config struct {
	// ID is the reverse-DNS app id ("dev.gofastr.notes"). It names the
	// data directory under the OS user config dir. Required.
	ID string

	// Title is the window title; defaults to ID.
	Title string

	// Width and Height of the first window; defaults 1024 x 768.
	Width, Height int

	// Style is the main window's chrome (frameless, floating,
	// transparent, ...). The zero value is the host's standard window.
	Style WindowStyle

	// Widgets are secondary windows opened at launch, in order, after
	// the main window's boot navigation (desktop.Widget builds the
	// typical floating-widget spec). Ids follow OpenWindow's normal
	// rule.
	Widgets []WindowSpec

	// DataDir overrides DataDir(ID). Must be absolute when set.
	DataDir string

	// Shell is the native layer. nil selects NewUnsupportedShell() on
	// every platform: the darwin default moved to battery/desktop/native
	// with the platform split, so a host that wants the real shell
	// passes native.New(cfg) or Shell: native.Shell(). A test double
	// goes here.
	Shell Shell

	// Menu is the optional native menu tree. Validated (and ID-assigned
	// on a copy) at New; an invalid item panics.
	Menu *Menu

	// Settings, when set, enables the app-menu "Settings…" item
	// (cmd+,), the RoleSettings menu role, and windows.openSettings on
	// the bridge. Path is validated like a menu Navigate path; New
	// panics on a bad one.
	Settings *WindowSpec

	// Tray, when set, puts the app in the OS menu bar. macOS only for
	// now: on a host without a tray, Run logs a Warn and the config is
	// ignored.
	Tray *Tray

	// DeepLink, when set, makes the app answer URLs on its own scheme
	// (see DeepLinkConfig).
	DeepLink *DeepLinkConfig

	// RememberWindows persists every window's frame (position and
	// size, screen points) and the main window's last path under the
	// "windows" key of the app state store (state.json in the data dir,
	// 0600), and restores them on the next launch: the main window and
	// each secondary window reopen where the user left them, and the
	// boot handshake redirects to the main window's last path instead
	// of "/". A remembered frame that no longer intersects any screen
	// (the monitor was unplugged) is dropped and the window centers.
	RememberWindows bool

	// Preferences declares the app's settings screen: typed values
	// (bool, int, string, choice) stored in the app state under
	// "settings", read through (*Battery).Preferences, and rendered by
	// desktop.PreferencesScreen (the screen builder the host mounts,
	// typically at the path Config.Settings names). New panics on a
	// declaration whose Default does not match its Kind, whose key is
	// off the grammar or duplicated, whose Choice carries no Choices,
	// or whose Int carries Min > Max.
	Preferences []Preference

	// Update, when set, enables the auto-updater (see UpdateConfig).
	Update *UpdateConfig

	// KeepIsolation stops Run from forcing GOFASTR_ISOLATION=off.
	//
	// Run sets GOFASTR_ISOLATION=off in the PROCESS environment before
	// starting the app (a desktop app never runs from a linked git
	// worktree, and the isolation remap would silently move the
	// loopback port the window is about to load). That env change is
	// process-wide: any other App started afterwards in the same
	// process loses worktree isolation too. Set KeepIsolation to keep
	// the ambient environment untouched.
	KeepIsolation bool

	// Logger defaults to slog.Default() until Init swaps in the app's
	// logger.
	Logger *slog.Logger
}

// Battery is the desktop host. Construct with New, register with
// app.RegisterBattery, then Run (which replaces app.Start).
type Battery struct {
	cfg    Config
	shell  Shell
	logger *slog.Logger
	// Handshake state, minted at New.
	bootToken    string
	sessionValue string
	tokenUsed    atomic.Bool
	hostPin      atomicString
	gateArmed    atomic.Bool
	// Capability registry and grants.
	reg    *registry
	grants GrantStore
	// promptMu serializes permission prompts so two concurrent first
	// calls do not show two dialogs.
	promptMu sync.Mutex

	// Menu (validated copy) and the local identity.
	menu *Menu
	user *localUser
	// authOwnsIdentity is set by installOwnerExtractor (from Init) when
	// another battery had already installed the framework/owner
	// extractor, battery/auth does it from its package init. When it is
	// set, localUserMiddleware installs no identity at all: auth's
	// "this request is anonymous" is a DECISION, and a fallback that
	// cannot see it turns a signed-out desktop window into the local
	// admin. Read-only after Init.
	authOwnsIdentity bool

	dataDir string

	// bridgeScriptURL query nonce + lazily built artifacts.
	scriptNonce string
	script      bridgeScript
	manifestMu  sync.Mutex
	manifest    *Manifest

	// App wiring, filled by Init.
	host        *uihost.UIHost
	app         *framework.App
	readyCh     chan string
	initMu      sync.Mutex
	initialized bool

	// Window bookkeeping. window is the MAIN window (set by the shell's
	// ready callback); windows/winOrder/winPaths cover the secondaries
	// OpenWindow opened. openMu serializes OpenWindow so two callers
	// racing on the same path get one window and one Focus. addr is the
	// loopback address Run resolved, the base of every window URL.
	windowMu   sync.Mutex
	window     Window
	windows    map[string]Window
	winOrder   []string
	winPaths   map[string]string
	nextWinNum int
	addr       string
	openMu     sync.Mutex

	// The validated tray copy (its Menu carries the tray rows only).
	tray *Tray
	// runMenu is the validated MAIN menu handed to the shell (b.menu
	// keeps both trees so dispatchMenu finds tray items too).
	runMenu *Menu

	// fs allow-list (cap_fs.go).
	allowMu    sync.Mutex
	allowPaths map[string]struct{}

	// deepLinks queues links that arrive before the window is up
	// (deeplink.go).
	deepLinks deepLinkQueue

	// updater is the auto-update state (update.go), built on first use.
	updater     *updater
	updaterOnce sync.Once

	// state is the app state store (battery/desktop/appstate), opened
	// by Run after the app reports ready: Init, which resolves the data
	// dir, runs inside app.Start. Run assigns it while the app's
	// listener is already answering, and handler goroutines read it
	// through stateStore(), so the field is an atomic pointer: the
	// handoff is synchronized by construction. Nil before Run; State()
	// documents that.
	state atomic.Pointer[appstate.Store]

	// prefs is the validated preference list (Config.Preferences),
	// built at New; Preferences() hands it out. Its reads consult
	// state, so before Run they answer the declared defaults.
	prefs *Preferences

	// winStore is the remembered window state (windowstate.go), a thin
	// client over the app state store, set by Run when
	// Config.RememberWindows is on, after the app's listener is
	// answering (same handoff shape as state, so same atomic). nil
	// when the app does not remember windows.
	winStore atomic.Pointer[windowStore]
}

// New validates cfg and constructs the Battery, registering the eight
// core capabilities. A nil cfg.Shell selects the unsupported shell on
// every platform; the platform-aware default is native.New
// (battery/desktop/native). New panics on invalid configuration with a
// message prefixed "desktop:", the same construction-time posture as
// framework.NewApp's registration panics.
func New(cfg Config) *Battery {
	if err := validateAppID(cfg.ID); err != nil {
		panic(err.Error())
	}
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}

	// One validation pass over the main menu AND the tray menu, so
	// item ids are unique across both (the shell dispatches both
	// through the same action-id table). menu keeps every tree for
	// dispatch lookup; runMenu holds only the main rows for the shell.
	var merged Menu
	if cfg.Menu != nil {
		merged.Items = append(merged.Items, cfg.Menu.Items...)
	}
	mainCount := len(merged.Items)
	if cfg.Tray != nil && cfg.Tray.Menu != nil {
		merged.Items = append(merged.Items, cfg.Tray.Menu.Items...)
	}
	var menu *Menu
	var runMenu *Menu
	var trayMenu *Menu
	if len(merged.Items) > 0 {
		validated, err := validateMenu(&merged)
		if err != nil {
			panic(err.Error())
		}
		menu = validated
		runMenu = &Menu{Items: validated.Items[:mainCount]}
		if len(validated.Items) > mainCount {
			trayMenu = &Menu{Items: validated.Items[mainCount:]}
		}
	}
	if err := validateDeepLink(cfg.DeepLink); err != nil {
		panic(err.Error())
	}
	if cfg.Update != nil {
		if err := validateUpdateConfig(cfg.Update); err != nil {
			panic(err.Error())
		}
	}
	prefs, err := newPreferences(nil, cfg.Preferences)
	if err != nil {
		panic(err.Error())
	}
	if cfg.Settings != nil && !validNavigatePath(cfg.Settings.Path) {
		panic(fmt.Sprintf("desktop: Config.Settings.Path %q must be a same-origin absolute path (leading /, no scheme, no //, no control characters, no .. segments)", cfg.Settings.Path))
	}
	var tray *Tray
	if cfg.Tray != nil {
		t := *cfg.Tray
		t.Menu = trayMenu
		tray = &t
	}
	shell := cfg.Shell
	if shell == nil {
		shell = NewUnsupportedShell()
	}
	token, err := randomToken()
	if err != nil {
		panic(fmt.Sprintf("desktop: mint boot token: %v", err))
	}
	session, err := randomToken()
	if err != nil {
		panic(fmt.Sprintf("desktop: mint session value: %v", err))
	}
	nonce, err := randomToken()
	if err != nil {
		panic(fmt.Sprintf("desktop: mint script nonce: %v", err))
	}
	b := &Battery{
		cfg:          cfg,
		shell:        shell,
		logger:       logger,
		bootToken:    token,
		sessionValue: session,
		reg:          newRegistry(),
		menu:         menu,
		runMenu:      runMenu,
		tray:         tray,
		dataDir:      cfg.DataDir,
		scriptNonce:  nonce,
		readyCh:      make(chan string, 1),
		windows:      make(map[string]Window),
		winPaths:     make(map[string]string),
		allowPaths:   make(map[string]struct{}),
	}
	prefs.b = b
	b.prefs = prefs
	// Register the core capabilities; a plugin adding a colliding name
	// gets an error naming both sites.
	for _, cap := range []Capability{
		b.windowCapability(),
		b.windowsCapability(),
		b.dialogsCapability(),
		b.clipboardCapability(),
		b.notificationsCapability(),
		b.fsCapability(),
		b.trayCapability(),
		b.preferencesCapability(),
		b.updatesCapability(),
		b.stateCapability(),
	} {
		if cap.Name == "" {
			continue
		}
		if err := b.reg.register(cap, "battery/desktop core capability "+cap.Name); err != nil {
			panic(err.Error())
		}
	}
	return b
}

// Name implements framework.Battery.
func (b *Battery) Name() string { return "desktop" }

// FromApp fetches the registered desktop battery off an App, the seam
// plugins use to register capabilities from their own Init.
func FromApp(app *framework.App) (*Battery, error) {
	return framework.GetAs[*Battery](app.Batteries, "desktop")
}

// State returns the app state store Run opened in the data dir
// (state.json, one store for the battery and the page). nil BEFORE
// Run: the data dir is resolved by Init inside app.Start, so no store
// exists to hand out yet. Call it from menu handlers, capability
// handlers, or goroutines that outlive Run's start.
func (b *Battery) State() *appstate.Store { return b.state.Load() }

// Preferences returns the declared preferences (Config.Preferences):
// typed reads of the stored-or-default values plus Set. Usable from
// New on; before Run opens the app state store the reads answer the
// declared defaults and Set answers unsupported (the --serve shape:
// no desktop host, no store). Bool/Int/String panic on an undeclared
// key or a kind mismatch, a programming error.
func (b *Battery) Preferences() *Preferences { return b.prefs }

// Init wires the battery into the App: routes, gate + local-identity
// middleware, the bridge script rail entry, the owner extractor
// fallback, and the OnReady hook that hands Run the bound address.
func (b *Battery) Init(app *framework.App) error {
	b.initMu.Lock()
	defer b.initMu.Unlock()
	if b.initialized {
		return errors.New("desktop: Init called twice")
	}
	b.app = app
	b.logger = app.Logger()

	for _, m := range app.Mountables() {
		if h, ok := m.(*uihost.UIHost); ok {
			b.host = h
			break
		}
	}
	if b.host == nil {
		return errors.New("desktop: no *uihost.UIHost among the App's mountables; mount one (app.Mount(uihost.New(site))) BEFORE app.RegisterBattery(desktop.New(...)); the desktop host serves the app's pages through the UI host")
	}

	// Data dir + local identity (best effort to surface config errors
	// now rather than mid-boot).
	dir := b.dataDir
	if dir == "" {
		d, err := DataDir(b.cfg.ID)
		if err != nil {
			return err
		}
		dir = d
	}
	b.dataDir = dir
	user, err := loadOrMintLocalUser(dir)
	if err != nil {
		return err
	}
	b.user = user

	// Grants: persisted when the app has a database, in-memory (with a
	// Warn) otherwise, the framework-screams convention for accepted
	// configuration it cannot act on.
	if app.DB != nil {
		b.grants = newSQLGrantStore(app.DB)
	} else {
		b.grants = newMemGrantStore()
		b.logger.Warn("desktop: no database on the App; permission grants will NOT persist across restarts",
			"fix", "pass framework.WithDB through desktop.AppOptions or your own wiring")
	}

	// Gate first (outermost), then the local identity inside it.
	app.Use(b.gateMiddleware())
	app.Use(b.localUserMiddleware())
	b.installOwnerExtractor()

	b.registerBridgeRoutes(app.Router())
	if err := b.host.RegisterExternalScript(b.bridgeScriptURL()); err != nil {
		return fmt.Errorf("desktop: register bridge script: %w", err)
	}

	app.OnReady(func(addr string) {
		select {
		case b.readyCh <- addr:
		default:
		}
	})
	b.initialized = true
	return nil
}

// OnStart creates the grants table once the app has a database.
// Implements framework.BatteryLifecycle.
func (b *Battery) OnStart(ctx context.Context) error {
	if s, ok := b.grants.(*sqlGrantStore); ok {
		return s.ensureSchema(ctx)
	}
	return nil
}

// OnStop implements framework.BatteryLifecycle. The window and app are
// torn down by Run; nothing to drain here beyond dropping the window
// references so a late Emit fails instead of touching a dead view.
func (b *Battery) OnStop(_ context.Context) error {
	b.windowMu.Lock()
	b.window = nil
	b.windows = make(map[string]Window)
	b.winOrder = nil
	b.winPaths = make(map[string]string)
	b.addr = ""
	b.windowMu.Unlock()
	return nil
}

// Shell returns the configured native shell (the test double, in
// tests).
func (b *Battery) Shell() Shell { return b.shell }

// Window returns the live MAIN window once the shell's ready callback
// has run.
func (b *Battery) Window() (Window, bool) {
	b.windowMu.Lock()
	defer b.windowMu.Unlock()
	return b.window, b.window != nil
}

// Windows returns every live window: the main window first, then the
// secondaries in opening order.
func (b *Battery) Windows() []Window {
	b.windowMu.Lock()
	defer b.windowMu.Unlock()
	var out []Window
	if b.window != nil {
		out = append(out, b.window)
	}
	for _, id := range b.winOrder {
		if w, ok := b.windows[id]; ok {
			out = append(out, w)
		}
	}
	return out
}

// OpenWindow opens a secondary window on one of the app's own screens
// (spec.Path is validated like a menu Navigate path). A window with
// the same path that is already open is focused and returned, so the
// call is idempotent per path. The returned Window's ID is "settings"
// for the configured settings path, else "w2", "w3", ... in opening
// order.
func (b *Battery) OpenWindow(spec WindowSpec) (Window, error) {
	if !validNavigatePath(spec.Path) {
		return nil, &Error{Code: CodeInvalidInput, Message: "path must be a same-origin absolute path (leading /, no scheme, no //, no .. segments)"}
	}
	// Serialize opens so two callers racing on the same path get one
	// window and one Focus, not two windows.
	b.openMu.Lock()
	defer b.openMu.Unlock()

	b.windowMu.Lock()
	addr := b.addr
	if addr == "" {
		b.windowMu.Unlock()
		return nil, &Error{Code: CodeUnsupported, Message: "window is not open yet"}
	}
	if id, ok := b.winPaths[spec.Path]; ok {
		w := b.windows[id]
		b.windowMu.Unlock()
		if w == nil {
			return nil, &Error{Code: CodeInternal, Message: InternalErrorMsg}
		}
		if err := w.Focus(); err != nil {
			return nil, err
		}
		return w, nil
	}
	// Every count created from request input is capped. windows.open is
	// UNGATED, OpenWindow de-dupes per path STRING, and
	// validNavigatePath accepts a query, so "/n?i=0", "/n?i=1", ... are
	// all distinct paths: without this bound one page script allocates
	// native windows without limit (measured: 1000, no refusal), and
	// b.windows / b.winOrder / b.winPaths grow with them. The cap lives
	// here rather than in the capability handler because OpenWindow is
	// also the in-process API (menu handlers, OpenSettings), and a bound
	// applied at one of two callers is the drift this package keeps
	// producing.
	if len(b.winOrder) >= maxSecondaryWindows {
		b.windowMu.Unlock()
		return nil, &Error{Code: CodeDenied, Message: "too many windows are open"}
	}
	id := b.nextWindowIDLocked(spec.Path)
	b.windowMu.Unlock()

	// A remembered frame for this window id replaces a nil Frame, so a
	// secondary window ("settings", a widget) reopens where the user
	if s := b.winStore.Load(); s != nil && spec.Frame == nil {
		spec.Frame = s.frameFor(id)
	}
	if spec.Title == "" {
		spec.Title = b.windowTitle()
	}
	w, err := b.shell.OpenWindow(id, spec, "http://"+addr+spec.Path)
	if err != nil {
		return nil, err
	}
	b.windowMu.Lock()
	b.windows[id] = w
	b.winOrder = append(b.winOrder, id)
	b.winPaths[spec.Path] = id
	b.windowMu.Unlock()
	return w, nil
}

// maxSecondaryWindows bounds the secondary windows one process may have
// open at once (the main window is not one of them). See OpenWindow.
const maxSecondaryWindows = 16

// nextWindowIDLocked assigns the id for a new secondary window:
// "settings" for the configured settings path, else the next "w<N>"
// (starting at w2; "main" and "settings" are reserved).
func (b *Battery) nextWindowIDLocked(path string) string {
	if s := b.cfg.Settings; s != nil && s.Path == path {
		return "settings"
	}
	b.nextWinNum++
	if b.nextWinNum < 2 {
		b.nextWinNum = 2
	}
	return fmt.Sprintf("w%d", b.nextWinNum)
}

// handleWindowClosed drops a secondary window's registration so the
// next OpenWindow on the same path opens a fresh one. WindowConfig's
// OnWindowClosed, never called for the main window.
func (b *Battery) handleWindowClosed(id string) {
	if id == "main" {
		return
	}
	b.windowMu.Lock()
	defer b.windowMu.Unlock()
	delete(b.windows, id)
	for p, pid := range b.winPaths {
		if pid == id {
			delete(b.winPaths, p)
		}
	}
	for i, x := range b.winOrder {
		if x == id {
			b.winOrder = append(b.winOrder[:i], b.winOrder[i+1:]...)
			break
		}
	}
}

// handleWindowFrame is WindowConfig.OnWindowFrame: the shell reported a
// user move or resize. It runs on a goroutine (the delegate hands it
// off), and the store debounces the write.
func (b *Battery) handleWindowFrame(id string, f Frame) {
	if s := b.winStore.Load(); s != nil {
		s.setFrame(id, f)
	}
}

func (b *Battery) onWindowFrame() func(id string, f Frame) {
	if b.winStore.Load() == nil {
		return nil
	}
	return b.handleWindowFrame
}

func (b *Battery) rememberedFrame(id string) *Frame {
	s := b.winStore.Load()
	if s == nil {
		return nil
	}
	return s.frameFor(id)
}

// OpenSettings opens (or focuses) the window Config.Settings
// describes. ErrUnsupported when Config.Settings is nil.
func (b *Battery) OpenSettings() (Window, error) {
	if b.cfg.Settings == nil {
		return nil, &Error{Code: CodeUnsupported, Message: "no Settings window is configured (desktop.Config.Settings)"}
	}
	spec := *b.cfg.Settings
	if spec.Title == "" {
		spec.Title = "Settings"
	}
	return b.OpenWindow(spec)
}

// SetTrayTitle changes the tray item's title. ErrUnsupported when
// Config.Tray is nil.
func (b *Battery) SetTrayTitle(title string) error {
	if b.cfg.Tray == nil {
		return &Error{Code: CodeUnsupported, Message: "no tray is configured (desktop.Config.Tray)"}
	}
	return b.shell.SetTrayTitle(title)
}

// Notify shows an OS notification through the shell's notifier: the
// Go-side entry the bridge's notifications.show capability uses.
func (b *Battery) Notify(ctx context.Context, n Notification) error {
	return b.shell.Notifier().Show(ctx, n)
}

// Register adds a capability. Valid until Run freezes the registry;
// duplicate names are refused naming the first registrant's site.
func (b *Battery) Register(cap Capability) error {
	origin, _ := callerSite(2)
	return b.reg.register(cap, origin)
}

// MustRegister is Register for static wiring: it panics on error.
func (b *Battery) MustRegister(cap Capability) {
	origin, _ := callerSite(2)
	if err := b.reg.register(cap, origin); err != nil {
		panic(err.Error())
	}
}

// Manifest returns the frozen manifest; before Run froze the registry
// it is an error.
func (b *Battery) Manifest() (Manifest, error) {
	if !b.reg.isFrozen() {
		return Manifest{}, errors.New("desktop: Manifest before Run froze the registry")
	}
	return b.frozenManifest()
}

// Emit dispatches a native event to the page-side listener API
// (window.__gofastr.desktop._dispatch) in EVERY open window, the main
// window first: a native event (a menu action, an update tick) is
// app-wide, and every page that asked to hear about it does. The
// payload is marshaled to JSON once and handed to each page as a
// QUOTED STRING parsed by JSON.parse, never as an object literal:
// __proto__ is a setter in a literal, and duplicate __proto__ keys
// are a SyntaxError json.Valid accepts.
//
// An eval failure in one window is logged and does not stop the
// others; Emit returns the first error after trying every window.
// EmitTo delivers to one window only.
//
// Emit is for the page-side listener API only; island refreshes after a
// native event stay server-driven through island Manager.PushUpdate.
func (b *Battery) Emit(name string, payload any) error {
	if !reCapabilityName.MatchString(name) {
		return fmt.Errorf("desktop: event name %q must match %s", name, reCapabilityName.String())
	}
	data, err := marshalJSONForEval(payload)
	if err != nil {
		return fmt.Errorf("desktop: emit %q: %w", name, err)
	}
	ws := b.Windows()
	if len(ws) == 0 {
		return errors.New("desktop: emit before the window opened")
	}
	js := dispatchJS(name, data)
	var firstErr error
	for _, w := range ws {
		if err := w.Eval(js); err != nil {
			b.logger.Warn("desktop: emitting an event to a window failed",
				"window", w.ID(), "error", err.Error())
			if firstErr == nil {
				firstErr = err
			}
		}
	}
	return firstErr
}

// EmitTo dispatches a native event to ONE window's page listeners
// (window.__gofastr.desktop._dispatch). A window id that is not open
// is a *Error with Code not_found.
func (b *Battery) EmitTo(windowID, name string, payload any) error {
	if !reCapabilityName.MatchString(name) {
		return fmt.Errorf("desktop: event name %q must match %s", name, reCapabilityName.String())
	}
	data, err := marshalJSONForEval(payload)
	if err != nil {
		return fmt.Errorf("desktop: emit %q: %w", name, err)
	}
	return b.emitToWindowID(windowID, name, data)
}

// marshalJSONForEval marshals v to compact JSON for embedding in a JS
// string literal; callers still quote the result before splicing it
// into evaluated code.
func marshalJSONForEval(v any) (string, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// runTimeout bounds the wait for the app's listener.
const runTimeout = 30 * time.Second

// Run is the host entry point: it owns the process's main flow for the
// lifetime of the window. It starts the App on a loopback port, pins
// the gate, freezes the registry, opens the native window on the boot
// URL, and blocks until the window closes. Call it instead of
// app.Start; do not call both.
func (b *Battery) Run(app *framework.App) error {
	b.initMu.Lock()
	initialized := b.initialized
	hostMissing := b.host == nil
	b.initMu.Unlock()

	if got, err := framework.GetAs[*Battery](app.Batteries, "desktop"); err != nil || got != b {
		return errors.New("desktop: Run: this battery is not registered on the App; call app.RegisterBattery(desktop.New(...)) before Run, or the App never runs the battery's Init")
	}
	if initialized && hostMissing {
		return errors.New("desktop: Run: Init found no *uihost.UIHost; mount one (app.Mount(uihost.New(site))) before app.RegisterBattery(desktop.New(...))")
	}

	// `gofastr desktop types` runs the app with ManifestEnv set: freeze
	// the registry, print the manifest JSON to stdout, and stop. No
	// listener, no window, no shell. Everything a generator needs and
	// nothing a headless CI run cannot do.
	if os.Getenv(ManifestEnv) == "1" {
		return b.runManifestMode(app, os.Stdout)
	}

	// Arm the boot gate BEFORE the listener opens (see gateMiddleware):
	// from the moment the port answers, every request needs the session
	// cookie. An app that never calls Run keeps the gate unarmed and its
	// routes open, which is the --serve host-independence mode.
	b.armGate()

	// Deviation 7: worktree isolation off for this Start. Process-wide
	// effect; see Config.KeepIsolation.
	if !b.cfg.KeepIsolation {
		os.Setenv("GOFASTR_ISOLATION", "off")
	}
	startErr := make(chan error, 1)
	go func() { startErr <- app.Start("127.0.0.1:0") }()

	var addr string
	select {
	case a := <-b.readyCh:
		addr = a
	case err := <-startErr:
		b.shell.Quit()
		return fmt.Errorf("desktop: app failed to start: %w", err)
	case <-time.After(runTimeout):
		b.shell.Quit()
		shutdownErr := b.shutdownApp()
		return fmt.Errorf("desktop: app did not report ready within %s (start error: %v)", runTimeout, shutdownErr)
	}

	host, _, err := net.SplitHostPort(addr)
	if err != nil || host != "127.0.0.1" {
		b.shell.Quit()
		b.shutdownApp()
		return fmt.Errorf("desktop: app bound a non-loopback address %q; the desktop host requires 127.0.0.1", addr)
	}
	b.setHostPin(addr)
	b.reg.freeze()
	b.windowMu.Lock()
	b.addr = addr
	b.windowMu.Unlock()

	enterURL := "http://" + addr + enterPath + "?t=" + url.QueryEscape(b.bootToken)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	b.startUpdater(ctx)
	// The app state store opens after the app reports ready (Init,
	// which resolves the data dir, runs inside app.Start) and before
	// the shell opens: the remembered windows, the state capability,
	b.state.Store(appstate.Open(filepath.Join(b.dataDir, stateFileName), b.logger))
	if b.cfg.RememberWindows {
		b.winStore.Store(newWindowStore(b.state.Load(), b.logger))
	}

	shellErr := b.shell.Run(ctx, WindowConfig{
		Title:    b.windowTitle(),
		Width:    b.windowWidth(),
		Height:   b.windowHeight(),
		Style:    b.cfg.Style,
		Menu:     b.runMenu,
		Tray:     b.tray,
		Settings: b.cfg.Settings,
		OnMenu:   b.dispatchMenu,
		OnSettings: func() {
			if _, err := b.OpenSettings(); err != nil {
				b.logger.Error("desktop: opening the settings window failed", "error", err)
			}
		},
		OnWindowClosed: b.handleWindowClosed,
		OnDeepLink:     b.handleDeepLink,
		Frame:          b.rememberedFrame(MainWindowID),
		OnWindowFrame:  b.onWindowFrame(),
	}, func(w Window) {
		b.windowMu.Lock()
		b.window = w
		b.windows[w.ID()] = w
		b.windowMu.Unlock()
		if err := w.Navigate(enterURL); err != nil {
			b.logger.Error("desktop: initial navigation failed", "error", err)
		}
		b.flushDeepLinks()
		// Config.Widgets open after the boot navigation, in order,
		// through the same OpenWindow everything else uses (path
		// validation, id assignment, per-path dedupe included).
		for _, spec := range b.cfg.Widgets {
			if _, err := b.OpenWindow(spec); err != nil {
				b.logger.Error("desktop: opening a configured widget window failed",
					"path", spec.Path, "error", err)
			}
		}
		// A configured tray on a host with none (a Shell without a
		// status-item surface) is a named Warn, not a silent miss.
		if b.tray != nil {
			if err := b.shell.SetTrayTitle(b.tray.Title); err != nil {
				b.logger.Warn("desktop: Config.Tray is set but this host has no tray; ignoring it",
					"host_error", err.Error())
			}
		}
	})

	// The last state write happens here, after the shell returned and
	if s := b.state.Load(); s != nil {
		_ = s.Flush() // a failed write was already Warned inside
	}

	// Drain the app: Start returns once Shutdown completes.
	shutdownErr := b.shutdownApp()
	select {
	case err := <-startErr:
		if shellErr != nil {
			return shellErr
		}
		if err != nil {
			return fmt.Errorf("desktop: app start returned: %w", err)
		}
	case <-time.After(runTimeout):
		// Shutdown force-closed the server; Start should have returned.
		// Do not block the exit path on it.
	}
	if shellErr != nil {
		return shellErr
	}
	if shutdownErr != nil {
		return fmt.Errorf("desktop: shutdown: %w", shutdownErr)
	}
	return nil
}

// ManifestEnv, when set to "1" in the process environment, makes Run
// print the frozen manifest JSON to stdout and return without starting
// the App or touching the Shell. `gofastr desktop types` sets it so one
// headless run of the real app yields the exact capability surface the
// built app will serve.
const ManifestEnv = "GOFASTR_DESKTOP_MANIFEST"

// runManifestMode is the ManifestEnv path of Run. Plugins register
// their capabilities from Init, which normally runs inside app.Start;
// with no Start on this path, InitPlugins runs here (idempotent, so a
// later Start in the same process is unaffected).
func (b *Battery) runManifestMode(app *framework.App, w io.Writer) error {
	if err := app.InitPlugins(); err != nil {
		return fmt.Errorf("desktop: manifest mode: init plugins: %w", err)
	}
	b.reg.freeze()
	m, err := b.frozenManifest()
	if err != nil {
		return err
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(m)
}

// LocalUserID returns the per-installation local identity the battery
// installs into request contexts ("" before Init). Menu handlers and
// other code outside a request needs it to scope direct queries to the
// same rows the screens show.
func (b *Battery) LocalUserID() string {
	if b.user == nil {
		return ""
	}
	return b.user.GetID()
}

// shutdownApp drains the app with a bounded context.
func (b *Battery) shutdownApp() error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := b.app.Shutdown(ctx); err != nil {
		return err
	}
	return nil
}

// windowTitle/Width/Height resolve the effective Config values.
func (b *Battery) windowTitle() string {
	if b.cfg.Title != "" {
		return b.cfg.Title
	}
	return b.cfg.ID
}

func (b *Battery) windowWidth() int {
	if b.cfg.Width > 0 {
		return b.cfg.Width
	}
	return 1024
}

func (b *Battery) windowHeight() int {
	if b.cfg.Height > 0 {
		return b.cfg.Height
	}
	return 768
}

// AppOptions resolves the zero-config desktop database + secret: it
// creates the app's data dir, opens app.db through sqlite/stdlib (the
// caller's blank import of that package registers the driver), and
// reads or mints the secret file so sessions survive restarts.
//
//	opts, err := desktop.AppOptions("dev.gofastr.notes")
//	if err != nil { log.Fatal(err) }
//	app := framework.NewApp(append(opts,
//	    framework.WithConfig(framework.AppConfig{Name: "notes"}))...)
//
// New cannot do this itself (WithDB is a constructor option and the DB
// must exist before NewApp runs), so the main.go assembles it.
func AppOptions(id string) ([]framework.AppOption, error) {
	if err := validateAppID(id); err != nil {
		return nil, err
	}
	dir, err := DataDir(id)
	if err != nil {
		return nil, err
	}
	db, err := openAppDB(dir)
	if err != nil {
		return nil, err
	}
	secret, err := loadOrMintSecret(dir)
	if err != nil {
		db.Close()
		return nil, err
	}
	return []framework.AppOption{framework.WithDB(db), framework.WithSecret(secret)}, nil
}

// buildVersion returns the module version from the build info, or ""
// outside a versioned build.
func buildVersion() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return ""
	}
	v := info.Main.Version
	if v == "" || v == "(devel)" {
		return ""
	}
	return v
}

// callerSite formats the file:line skip frames above its caller, for
// duplicate-registration error messages.
func callerSite(skip int) (string, bool) {
	if _, file, line, ok := runtime.Caller(skip); ok {
		return file + ":" + strconv.Itoa(line), true
	}
	return "unknown", false
}
