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
	"github.com/DonaldMurillo/gofastr/battery/desktop/internal/update"
	"github.com/DonaldMurillo/gofastr/core/textsafe"
	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/framework/localapp"
	"github.com/DonaldMurillo/gofastr/framework/uihost"
	"github.com/DonaldMurillo/gofastr/internal/gatepath"
)

var reAppID = regexp.MustCompile(`^[A-Za-z0-9.-]+$`)

func validateAppID(id string) error {
	if !reAppID.MatchString(id) || !strings.Contains(id, ".") {
		return fmt.Errorf("desktop: app id %q must be reverse-DNS: [A-Za-z0-9.-]+ with at least one dot", id)
	}
	if strings.Trim(id, ".-") == "" {
		return fmt.Errorf("desktop: app id %q must not be a bare dot/dash sequence: "+
			"it names a directory under the OS user config dir", id)
	}
	if len(id) > 253 {
		return fmt.Errorf("desktop: app id %q is longer than 253 characters", id)
	}
	return nil
}

func validateWindowStyle(style WindowStyle) error {
	switch style.Material {
	case MaterialNone, MaterialSidebar, MaterialWindow, MaterialGlass:
	default:
		return fmt.Errorf("desktop: WindowStyle.Material %q must be one of sidebar, window, glass, or empty", style.Material)
	}
	if in := style.TrafficLightInset; in != nil && (in.X < 0 || in.Y < 0) {
		return fmt.Errorf("desktop: WindowStyle.TrafficLightInset (%d, %d) must not be negative; nil means the system position", in.X, in.Y)
	}
	return nil
}

// Config constructs a desktop Battery; ID is required and New panics on a bad Config.
type Config struct {
	// ID is the reverse-DNS app id ("dev.gofastr.notes").
	ID string

	// Title is the window title; defaults to ID.
	Title string

	// Width and Height of the first window; defaults 1024 x 768.
	Width, Height int

	// Style is the main window's chrome.
	Style WindowStyle

	// SidebarWidth is the main window's initial sidebar zone in points, 0..MaxSidebarWidth.
	SidebarWidth int

	// Widgets are secondary windows opened, in order, after the main window's boot navigation.
	Widgets []WindowSpec

	// DataDir overrides DataDir(ID); it must be absolute.
	DataDir string

	// PersistentProfile opts into a persistent AppID-scoped browser profile; Run returns ErrUnsupported on a shell without one.
	PersistentProfile bool

	// Shell is the native layer; nil selects NewUnsupportedShell.
	Shell Shell

	// Menu is the optional native menu tree; New panics on an invalid item.
	Menu *Menu

	// Settings, when set, enables the app-menu Settings item, RoleSettings and windows.openSettings.
	Settings *WindowSpec

	// Tray, when set, requests an OS status item.
	Tray *Tray

	// DeepLink, when set, makes the app answer URLs on its own scheme.
	DeepLink *DeepLinkConfig

	// RememberWindows persists window frames and the main window's last path, and restores them on the next launch.
	RememberWindows bool

	// DangerouslyAllowForeignNavigation lets a window's page navigate in place to another origin.
	DangerouslyAllowForeignNavigation bool

	// Preferences declares the app's typed settings; New panics on an invalid declaration.
	Preferences []Preference

	// Update, when set, enables the auto-updater.
	Update *UpdateConfig

	// OpenPrefixes lists path prefixes the boot gate lets through without the window's session cookie.
	OpenPrefixes []string

	// KeepIsolation stops Run from setting GOFASTR_ISOLATION=off in the process environment.
	KeepIsolation bool

	// Logger defaults to slog.Default() until Init swaps in the app's logger.
	Logger *slog.Logger
}

// Battery is the desktop host: New it, app.RegisterBattery it, then Run it in place of app.Start.
type Battery struct {
	cfg              Config
	shell            Shell
	logger           *slog.Logger
	bootToken        string
	sessionValue     string
	sessionCookieKey string
	tokenUsed        atomic.Bool
	hostPin          atomicString
	gateArmed        atomic.Bool
	reg              *registry
	grants           GrantStore
	promptOnce       sync.Once
	promptSem        chan struct{}

	menu             *Menu
	user             *localapp.User
	authOwnsIdentity bool

	dataDir string

	scriptNonce string
	script      bridgeScript
	manifestMu  sync.Mutex
	manifest    *Manifest

	host        *uihost.UIHost
	app         *framework.App
	readyCh     chan string
	initMu      sync.Mutex
	initialized bool

	windowMu   sync.Mutex
	window     Window
	windows    map[string]Window
	winOrder   []string
	winPaths   map[string]string
	nextWinNum int
	addr       string
	openMu     sync.Mutex

	tray    *Tray
	runMenu *Menu

	allowMu      sync.Mutex
	allowPaths   map[string]struct{}
	allowFolders map[string]string

	deepLinks deepLinkQueue

	updater     *updater
	updaterOnce sync.Once

	state atomic.Pointer[appstate.Store]

	prefs *Preferences

	winStore atomic.Pointer[windowStore]
}

// New validates cfg, registers the core capabilities, and panics with a "desktop:" message on a bad Config.
func New(cfg Config) *Battery {
	if err := validateAppID(cfg.ID); err != nil {
		panic(err.Error())
	}
	cfg.OpenPrefixes = append([]string(nil), cfg.OpenPrefixes...)
	for _, p := range cfg.OpenPrefixes {
		if err := gatepath.Validate("desktop: Config.OpenPrefixes", p); err != nil {
			panic(err.Error())
		}
	}
	if builtDeepLinkScheme != "" {
		if cfg.DeepLink == nil {
			cfg.DeepLink = &DeepLinkConfig{Scheme: builtDeepLinkScheme}
		} else if cfg.DeepLink.Scheme != builtDeepLinkScheme {
			panic(fmt.Sprintf("desktop: Config.DeepLink.Scheme %q does not match the scheme embedded by `gofastr desktop build --scheme` (%q)", cfg.DeepLink.Scheme, builtDeepLinkScheme))
		}
	}
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}

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
	if err := validateWindowStyle(cfg.Style); err != nil {
		panic(err.Error())
	}
	for i := range cfg.Widgets {
		if err := validateWindowStyle(cfg.Widgets[i].Style); err != nil {
			panic(err.Error())
		}
		if cfg.Widgets[i].SidebarWidth < 0 || cfg.Widgets[i].SidebarWidth > MaxSidebarWidth {
			panic(fmt.Sprintf("desktop: Widgets[%d].SidebarWidth %d must be between 0 and %d", i, cfg.Widgets[i].SidebarWidth, MaxSidebarWidth))
		}
	}
	if cfg.Settings != nil {
		if err := validateWindowStyle(cfg.Settings.Style); err != nil {
			panic(err.Error())
		}
		if cfg.Settings.SidebarWidth < 0 || cfg.Settings.SidebarWidth > MaxSidebarWidth {
			panic(fmt.Sprintf("desktop: Config.Settings.SidebarWidth %d must be between 0 and %d", cfg.Settings.SidebarWidth, MaxSidebarWidth))
		}
	}
	if cfg.SidebarWidth < 0 || cfg.SidebarWidth > MaxSidebarWidth {
		panic(fmt.Sprintf("desktop: Config.SidebarWidth %d must be between 0 and %d", cfg.SidebarWidth, MaxSidebarWidth))
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
		allowFolders: make(map[string]string),
	}
	prefs.b = b
	b.prefs = prefs
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

// ReservedEmbedPrefixes reports the bridge prefix, which no embed grant may reach.
func (b *Battery) ReservedEmbedPrefixes() []string { return []string{bridgePrefix} }

// FromApp returns the desktop battery registered on app.
func FromApp(app *framework.App) (*Battery, error) {
	return framework.GetAs[*Battery](app.Batteries, "desktop")
}

// State returns the app state store Run opened, or nil before Run.
func (b *Battery) State() *appstate.Store { return b.state.Load() }

// Preferences returns the declared preferences; before Run reads answer the defaults.
func (b *Battery) Preferences() *Preferences { return b.prefs }

// Init wires the battery into app; it errors when called twice or when no *uihost.UIHost is mounted.
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

	dir := b.dataDir
	if dir == "" {
		d, err := DataDir(b.cfg.ID)
		if err != nil {
			return err
		}
		dir = d
	}
	b.dataDir = dir
	user, err := localapp.LoadOrMint(dir)
	if err != nil {
		return fmt.Errorf("desktop: %w", err)
	}
	b.user = user

	if app.DB != nil {
		b.grants = newSQLGrantStore(app.DB)
	} else {
		b.grants = newMemGrantStore()
		b.logger.Warn("desktop: no database on the App; permission grants will NOT persist across restarts",
			"fix", "pass framework.WithDB through desktop.AppOptions or your own wiring")
	}

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
func (b *Battery) OnStart(ctx context.Context) error {
	if s, ok := b.grants.(*sqlGrantStore); ok {
		return s.ensureSchema(ctx)
	}
	return nil
}

// OnStop drops the window references, so a later Emit fails.
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

// Shell returns the configured native shell.
func (b *Battery) Shell() Shell { return b.shell }

// Window returns the main window once the shell's ready callback has run.
func (b *Battery) Window() (Window, bool) {
	b.windowMu.Lock()
	defer b.windowMu.Unlock()
	return b.window, b.window != nil
}

// OpenWindow opens, or focuses the already open window on, spec.Path; ids are "settings" or "w2", "w3", ...
func (b *Battery) OpenWindow(spec WindowSpec) (Window, error) {
	if !validNavigatePath(spec.Path) {
		return nil, &Error{Code: CodeInvalidInput, Message: "path must be a same-origin absolute path (leading /, no scheme, no //, no .. segments)"}
	}
	if err := validateWindowStyle(spec.Style); err != nil {
		return nil, &Error{Code: CodeInvalidInput, Message: strings.TrimPrefix(err.Error(), "desktop: ")}
	}
	if spec.SidebarWidth < 0 || spec.SidebarWidth > MaxSidebarWidth {
		return nil, &Error{Code: CodeInvalidInput, Message: fmt.Sprintf("sidebarWidth must be between 0 and %d", MaxSidebarWidth)}
	}
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
		if closed, known := windowClosed(w); w != nil && known && closed {
			b.removeWindowLocked(id)
		} else {
			b.windowMu.Unlock()
			if w == nil {
				return nil, &Error{Code: CodeInternal, Message: InternalErrorMsg}
			}
			if err := w.Focus(); err != nil {
				return nil, err
			}
			return w, nil
		}
	}
	if len(b.winOrder) >= maxSecondaryWindows {
		b.windowMu.Unlock()
		return nil, &Error{Code: CodeDenied, Message: "too many windows are open"}
	}
	id := b.nextWindowIDLocked(spec.Path)
	b.windowMu.Unlock()

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

// Windows returns every live window, the main window first, then secondaries in opening order.
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

const maxSecondaryWindows = 16

func (b *Battery) emitWindowFocus(id string) {
	b.emitWindowActivity("window_focus", id)
}

func (b *Battery) emitWindowBlur(id string) {
	b.emitWindowActivity("window_blur", id)
}

func (b *Battery) emitWindowActivity(name, id string) {
	if len(b.Windows()) == 0 {
		return
	}
	if err := b.Emit(name, map[string]string{"id": id}); err != nil {
		b.logger.Warn("desktop: emitting "+name+" failed", "window", id, "error", err.Error())
	}
}

func (b *Battery) emitAppearance(a Appearance) {
	if len(b.Windows()) == 0 {
		return
	}
	if err := b.Emit("reduce_transparency", map[string]bool{"on": a.ReduceTransparency}); err != nil {
		b.logger.Warn("desktop: emitting reduce_transparency failed", "error", err.Error())
	}
}

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

func (b *Battery) handleWindowClosed(id string) {
	if id == "main" {
		return
	}
	b.openMu.Lock()
	defer b.openMu.Unlock()
	b.handleWindowClosedLocked(id)
}

func (b *Battery) handleWindowClosedLocked(id string) {
	b.windowMu.Lock()
	defer b.windowMu.Unlock()
	if w := b.windows[id]; w != nil {
		if closed, known := windowClosed(w); known && !closed {
			return
		}
	}
	b.removeWindowLocked(id)
}

func windowClosed(w Window) (closed, known bool) {
	state, ok := w.(interface{ IsClosed() bool })
	if !ok {
		return false, false
	}
	return state.IsClosed(), true
}

func (b *Battery) removeWindowLocked(id string) {
	delete(b.windows, id)
	if s := b.winStore.Load(); s != nil {
		s.forget(id)
	}
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

// OpenSettings opens or focuses the Config.Settings window; unsupported when Config.Settings is nil.
func (b *Battery) OpenSettings(ctx context.Context) (Window, error) {
	if b.cfg.Settings == nil {
		return nil, &Error{Code: CodeUnsupported, Message: "no Settings window is configured (desktop.Config.Settings)"}
	}
	spec := *b.cfg.Settings
	if spec.Title == "" {
		spec.Title = pageSettingsTitle.Text(ctx, nil)
	}
	return b.OpenWindow(spec)
}

// SetTrayTitle changes the tray item's title; unsupported when Config.Tray is nil.
func (b *Battery) SetTrayTitle(title string) error {
	if b.cfg.Tray == nil {
		return &Error{Code: CodeUnsupported, Message: "no tray is configured (desktop.Config.Tray)"}
	}
	return b.shell.SetTrayTitle(title)
}

// Notify shows an OS notification through the shell's notifier.
func (b *Battery) Notify(ctx context.Context, n Notification) error {
	return b.shell.Notifier().Show(ctx, n)
}

// Register adds a capability; it errors after Run froze the registry or on a duplicate name.
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

// Manifest returns the frozen manifest; before Run froze the registry it is an error.
func (b *Battery) Manifest() (Manifest, error) {
	if !b.reg.isFrozen() {
		return Manifest{}, errors.New("desktop: Manifest before Run froze the registry")
	}
	return b.frozenManifest()
}

// Emit dispatches a native event to every open window, main first, and returns the first error.
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
		if err := w.Eval(b.inAppJS(js)); err != nil {
			b.logger.Warn("desktop: emitting an event to a window failed",
				"window", w.ID(), "error", err.Error())
			if firstErr == nil {
				firstErr = err
			}
		}
	}
	return firstErr
}

// EmitTo dispatches a native event to one window; an unknown id is a not_found *Error.
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

func marshalJSONForEval(v any) (string, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

const runTimeout = 30 * time.Second

// Run starts the App on loopback, opens the window and blocks until it closes; call it instead of app.Start.
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

	if os.Getenv(ManifestEnv) == "1" {
		return b.runManifestMode(app, os.Stdout)
	}
	if update.AwaitPredecessor() {
		b.logger.Info("desktop: started after an update; the previous instance has exited")
	}
	if builtDeepLinkScheme != "" {
		if activator, ok := b.shell.(interface {
			PrepareLaunch(appID, appName, scheme string) (handled bool, startupURLs []string, err error)
		}); ok {
			handled, startupURLs, err := activator.PrepareLaunch(b.cfg.ID, b.windowTitle(), builtDeepLinkScheme)
			if err != nil {
				return fmt.Errorf("desktop: prepare native launch: %w", err)
			}
			if handled {
				return nil
			}
			if launch, ok := b.shell.(interface{ ReleaseLaunch() }); ok {
				defer launch.ReleaseLaunch()
			}
			for _, rawURL := range startupURLs {
				b.handleDeepLink(rawURL)
			}
		}
	}

	listenAddr, profile, err := b.prepareProfile()
	if err != nil {
		return err
	}
	if profile != nil {
		defer profile.ReleaseProfile()
	}

	b.sessionCookieKey = sessionCookieName + "_" + b.sessionValue
	b.armGate()
	if len(b.cfg.OpenPrefixes) > 0 {
		b.logger.Warn("desktop: these paths answer without the window's session; the app's own auth must protect them (Config.OpenPrefixes)", "prefixes", b.cfg.OpenPrefixes)
	}

	if !b.cfg.KeepIsolation {
		os.Setenv("GOFASTR_ISOLATION", "off")
	}
	startErr := make(chan error, 1)
	go func() { startErr <- app.Start(listenAddr) }()

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
	if profile != nil {
		if err := profile.CommitProfile(addr); err != nil {
			b.shell.Quit()
			b.shutdownApp()
			return fmt.Errorf("desktop: persist browser origin: %w", err)
		}
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
	b.state.Store(appstate.Open(filepath.Join(b.dataDir, stateFileName), b.logger))
	if b.cfg.RememberWindows {
		b.winStore.Store(newWindowStore(b.state.Load(), b.logger))
	}
	var closing atomic.Bool
	startDone := make(chan error, 1)
	go func() {
		err := <-startErr
		startDone <- err
		if closing.Load() {
			return
		}
		defer func() {
			if v := recover(); v != nil {
				b.logger.Error("desktop: quitting the window panicked", "panic", textsafe.Recovered(v))
			}
		}()
		b.logger.Error("desktop: the app server stopped while the window was open; closing it", "error", err)
		b.shell.Quit()
	}()
	shellErr := b.shell.Run(ctx, WindowConfig{
		AppID:      b.cfg.ID,
		Title:      b.windowTitle(),
		Width:      b.windowWidth(),
		Height:     b.windowHeight(),
		Style:      b.cfg.Style,
		Menu:       b.runMenu,
		Tray:       b.tray,
		Settings:   b.cfg.Settings,
		OnMenu:     b.dispatchMenu,
		OnTrayMenu: b.dispatchTrayMenu,
		OnSettings: func(ctx context.Context) {
			if _, err := b.OpenSettings(ctx); err != nil {
				b.logger.Error("desktop: opening the settings window failed", "error", err)
			}
		},
		OnWindowClosed:  b.handleWindowClosed,
		AllowNavigation: b.allowNavigation,
		OnDeepLink:      b.handleDeepLink,
		Frame:           b.rememberedFrame(MainWindowID),
		OnWindowFrame:   b.onWindowFrame(),
		SidebarWidth:    b.cfg.SidebarWidth,
		OnWindowFocus:   b.emitWindowFocus,
		OnWindowBlur:    b.emitWindowBlur,
		OnAppearance:    b.emitAppearance,
	}, func(w Window) {
		b.windowMu.Lock()
		b.window = w
		b.windows[w.ID()] = w
		b.windowMu.Unlock()
		if err := w.Navigate(enterURL); err != nil {
			b.logger.Error("desktop: initial navigation failed", "error", err)
		}
		b.flushDeepLinks(w)
		for _, spec := range b.cfg.Widgets {
			if _, err := b.OpenWindow(spec); err != nil {
				b.logger.Error("desktop: opening a configured widget window failed",
					"path", spec.Path, "error", err)
			}
		}
		if b.tray != nil {
			if err := b.shell.SetTrayTitle(b.tray.Title); err != nil {
				b.logger.Warn("desktop: Config.Tray is set but this host has no tray; ignoring it",
					"host_error", err.Error())
			}
		}
	})

	if s := b.state.Load(); s != nil {
		_ = s.Flush() // a failed write was already Warned inside
	}

	closing.Store(true)
	shutdownErr := b.shutdownApp()
	select {
	case err := <-startDone:
		if shellErr != nil {
			return shellErr
		}
		if err != nil {
			return fmt.Errorf("desktop: app start returned: %w", err)
		}
	case <-time.After(runTimeout):
	}
	if shellErr != nil {
		return shellErr
	}
	if shutdownErr != nil {
		return fmt.Errorf("desktop: shutdown: %w", shutdownErr)
	}
	return nil
}

// ManifestEnv set to "1" makes Run print the frozen manifest JSON and return without starting the App.
const ManifestEnv = "GOFASTR_DESKTOP_MANIFEST"

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

// LocalUserID returns the per-installation local identity, or "" before Init.
func (b *Battery) LocalUserID() string {
	if b.user == nil {
		return ""
	}
	return b.user.GetID()
}

func (b *Battery) shutdownApp() error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := b.app.Shutdown(ctx); err != nil {
		return err
	}
	return nil
}

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

// AppOptions creates the data dir and returns WithDB on its app.db and WithSecret on its secret file.
func AppOptions(id string) ([]framework.AppOption, error) {
	if err := validateAppID(id); err != nil {
		return nil, err
	}
	dir, err := DataDir(id)
	if err != nil {
		return nil, err
	}
	db, err := localapp.OpenDB(dir)
	if err != nil {
		return nil, err
	}
	secret, err := localapp.LoadOrMintSecret(dir)
	if err != nil {
		db.Close()
		return nil, err
	}
	return []framework.AppOption{framework.WithDB(db), framework.WithSecret(secret)}, nil
}

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

func callerSite(skip int) (string, bool) {
	if _, file, line, ok := runtime.Caller(skip); ok {
		return file + ":" + strconv.Itoa(line), true
	}
	return "unknown", false
}
