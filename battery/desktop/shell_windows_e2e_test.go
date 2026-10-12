//go:build desktop_e2e && windows && amd64

package desktop_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/sys/windows/registry"

	"github.com/DonaldMurillo/gofastr/battery/desktop"
	"github.com/DonaldMurillo/gofastr/battery/desktop/desktoptest"
	"github.com/DonaldMurillo/gofastr/battery/desktop/native"
	uiapp "github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/framework/uihost"
)

const windowsE2EAppID = "windows-shell-e2e.gofastr.dev"
const windowsE2EScheme = "gofastr-windows-shell-e2e"
const windowsActivationE2EEnv = "GOFASTR_DESKTOP_TEST_URL_ACTIVATION"

type windowsE2EPage struct{}

func (windowsE2EPage) Render() render.HTML {
	return render.Tag("main", nil,
		render.Tag("h1", map[string]string{"id": "e2e-title"}, render.Text("Windows WebView2 e2e")),
		render.Tag("p", map[string]string{"id": "e2e-copy"}, render.Text("The page is running in WebView2.")),
	)
}

type windowsE2ETwo struct{}

func (windowsE2ETwo) Render() render.HTML {
	return render.Tag("main", nil,
		render.Tag("h1", map[string]string{"id": "e2e-two-title"}, render.Text("Second WebView2 page")),
	)
}

func buildWindowsE2EApp() (*framework.App, *desktop.Battery, error) {
	dataDir := os.Getenv("GOFASTR_DESKTOP_DATA_DIR")
	if dataDir == "" {
		dataDir = filepath.Join(os.TempDir(), "gofastr-windows-e2e")
	}
	dataDir = filepath.Join(dataDir, windowsE2EAppID)
	opts, err := desktop.AppOptions(windowsE2EAppID)
	if err != nil {
		return nil, nil, err
	}
	app := framework.NewApp(append(opts, framework.WithConfig(framework.AppConfig{Name: "windowsShellE2E"}))...)
	site := uiapp.NewApp("windowsShellE2E")
	site.Register("/", windowsE2EPage{}, nil)
	site.Register("/two", windowsE2ETwo{}, nil)
	app.Mount(uihost.New(site))
	battery := native.New(desktop.Config{
		ID: windowsE2EAppID, Title: "Windows WebView2 E2E", DataDir: dataDir,
		Width: 900, Height: 640, RememberWindows: true,
		DeepLink: &desktop.DeepLinkConfig{Scheme: windowsE2EScheme},
		Style:    desktop.WindowStyle{Chrome: desktop.ChromeUnified, Material: desktop.MaterialWindow},
		Menu:     &desktop.Menu{Items: []desktop.MenuItem{{Title: "Go", Children: []desktop.MenuItem{{Title: "Two", Navigate: "/two"}}}}},
		Tray:     &desktop.Tray{Title: "WindowsE2E", Tooltip: "Windows WebView2 test", Menu: &desktop.Menu{Items: []desktop.MenuItem{{Title: "Show Window", Role: desktop.RoleShow}}}},
	})
	app.RegisterBattery(battery)
	return app, battery, nil
}

func TestMain(m *testing.M) {
	if isWindowsActivationChild() {
		app, battery, err := buildWindowsE2EApp()
		if err == nil {
			err = battery.Run(app)
		}
		if err != nil {
			fmt.Fprintf(os.Stderr, "Windows activation child: %v\n", err)
			os.Exit(1)
		}
		os.Exit(0)
	}
	if os.Getenv(windowsActivationE2EEnv) == "1" {
		if err := ensureWindowsE2EProtocolAvailable(); err != nil {
			fmt.Fprintf(os.Stderr, "Windows activation test: %v\n", err)
			os.Exit(1)
		}
	}
	code := m.Run()
	if code == 0 {
		code = desktoptest.NativePhase(buildWindowsE2EApp, runWindowsE2E)
	}
	if os.Getenv(windowsActivationE2EEnv) == "1" {
		removeWindowsE2EProtocol()
	}
	os.Exit(code)
}

func isWindowsActivationChild() bool {
	if os.Getenv(windowsActivationE2EEnv) != "1" {
		return false
	}
	prefix := strings.ToLower(windowsE2EScheme + "://")
	for _, arg := range os.Args[1:] {
		if strings.HasPrefix(strings.ToLower(arg), prefix) {
			return true
		}
	}
	return false
}

func removeWindowsE2EProtocol() {
	classPath := `Software\Classes\` + windowsE2EScheme
	root, err := registry.OpenKey(registry.CURRENT_USER, classPath, registry.QUERY_VALUE)
	if err != nil {
		return
	}
	owner, _, ownerErr := root.GetStringValue("GoFastrAppID")
	_ = root.Close()
	if ownerErr != nil || owner != windowsE2EAppID {
		return
	}
	exe, err := os.Executable()
	if err != nil {
		return
	}
	exe, err = filepath.Abs(exe)
	if err != nil {
		return
	}
	command, err := registry.OpenKey(registry.CURRENT_USER, classPath+`\shell\open\command`, registry.QUERY_VALUE)
	if err != nil {
		return
	}
	registered, _, commandErr := command.GetStringValue("")
	_ = command.Close()
	if commandErr != nil || registered != fmt.Sprintf(`"%s" "%%1"`, exe) {
		return
	}
	for _, path := range []string{
		classPath + `\shell\open\command`,
		classPath + `\shell\open`,
		classPath + `\shell`,
		classPath,
	} {
		_ = registry.DeleteKey(registry.CURRENT_USER, path)
	}
}

func ensureWindowsE2EProtocolAvailable() error {
	classPath := `Software\Classes\` + windowsE2EScheme
	key, err := registry.OpenKey(registry.CURRENT_USER, classPath, registry.QUERY_VALUE)
	if err == nil {
		_ = key.Close()
		return fmt.Errorf("URL scheme %q is already registered for this user; refusing to replace it", windowsE2EScheme)
	}
	if !errors.Is(err, registry.ErrNotExist) {
		return fmt.Errorf("inspect URL scheme %q: %w", windowsE2EScheme, err)
	}
	return nil
}

// TestWindowsWebView2E2E is the selection point for the manual Windows
// run. Keep it separate so `-run ^TestWindowsWebView2E2E$` avoids tests
// whose Unix permission-bit assertions do not apply to Windows.
func TestWindowsWebView2E2E(t *testing.T) {}

type windowsPhaseLog struct {
	mu     sync.Mutex
	failed bool
}

func (l *windowsPhaseLog) Helper()                         {}
func (l *windowsPhaseLog) Logf(format string, args ...any) { fmt.Printf(format+"\n", args...) }
func (l *windowsPhaseLog) Fatal(args ...any)               { l.Fatalf("%s", fmt.Sprint(args...)) }
func (l *windowsPhaseLog) Fatalf(format string, args ...any) {
	l.mu.Lock()
	l.failed = true
	l.mu.Unlock()
	fmt.Fprintf(os.Stderr, "WINDOWS E2E: "+format+"\n", args...)
}
func (l *windowsPhaseLog) Failed() bool { l.mu.Lock(); defer l.mu.Unlock(); return l.failed }

func runWindowsE2E(h *desktoptest.NativeHarness) bool {
	log := &windowsPhaseLog{}
	h.SetTB(log)
	h.Wait("the root page", func() bool {
		raw, err := h.EvalQuiet("return location.pathname")
		var path string
		return err == nil && json.Unmarshal(raw, &path) == nil && path == "/"
	})
	if text := h.TextQuiet("#e2e-title"); text != "Windows WebView2 e2e" {
		log.Fatalf("page title text = %q", text)
	}
	if w, ok := h.Battery.Window(); !ok || w.Native() == 0 {
		log.Fatalf("the battery has no native WebView2 window")
	}
	state, err := h.WindowState("main")
	if err != nil || !state.Visible || state.ContentClass != "WebView2" {
		log.Fatalf("main window state = %+v, err = %v", state, err)
	}
	if state.TitleVisibility != "visible" {
		log.Fatalf("native menu caption title visibility = %q, want visible", state.TitleVisibility)
	}
	if state.Material != "none" && state.Material != "mica" {
		log.Fatalf("Windows 11 material = %q, want mica or the unsupported-system fallback", state.Material)
	}

	// The page script runs in the embedded browser and waits for the Go
	// evaluator to poll its promise result through ExecuteScript.
	var pageResult struct {
		Path string `json:"path"`
		Text string `json:"text"`
	}
	raw, err := h.EvalQuiet(`return {path: location.pathname, text: document.querySelector('#e2e-copy').textContent}`)
	if err != nil || json.Unmarshal(raw, &pageResult) != nil || pageResult.Path != "/" || pageResult.Text != "The page is running in WebView2." {
		log.Fatalf("page evaluation = %s, err = %v", raw, err)
	}

	// Permission-gated clipboard access exercises the bridge through the
	// live page, then checks the value returned by Windows itself.
	driver, ok := h.Shell.(desktop.NativeDriver)
	if !ok {
		log.Fatalf("the Windows shell does not expose NativeDriver")
	} else {
		driver.ScriptPrompts(desktop.DecisionAllow, desktop.DecisionAllow, desktop.DecisionAllow)
	}
	write := h.Call("clipboard", "writeText", map[string]any{"text": "webview2 clipboard round trip"})
	if !write.OK {
		log.Fatalf("clipboard.writeText: %s %s", write.Code, write.Message)
	}
	read := h.Call("clipboard", "readText", nil)
	var clip struct {
		Text string `json:"text"`
	}
	if !read.OK || json.Unmarshal(read.Result, &clip) != nil || clip.Text != "webview2 clipboard round trip" {
		log.Fatalf("clipboard.readText = %s (%s: %s)", read.Result, read.Code, read.Message)
	}
	notice := h.Call("notifications", "show", map[string]any{"title": "Windows e2e", "body": "Notification area balloon"})
	if !notice.OK {
		log.Fatalf("notifications.show: %s %s", notice.Code, notice.Message)
	}
	if entries := h.Notifications(); len(entries) != 1 || entries[0].Title != "Windows e2e" {
		log.Fatalf("native notification log = %+v", entries)
	}

	if ok {
		if err := driver.ActivateMenu("Go", "Two"); err != nil {
			log.Fatalf("activate Go > Two menu item: %v", err)
		}
		h.WaitLocation("/two")
		h.Navigate("/")
		var opened struct {
			ID string `json:"id"`
		}
		result := h.Call("windows", "open", map[string]any{"path": "/two", "title": "Second window", "width": 540, "height": 360})
		if !result.OK || json.Unmarshal(result.Result, &opened) != nil || opened.ID == "" {
			log.Fatalf("windows.open = %+v", result)
		} else {
			h.Wait("the secondary WebView2 window", func() bool { return h.Window(opened.ID) != nil })
			h.Wait("the secondary page to load", func() bool {
				w := h.Window(opened.ID)
				if w == nil {
					return false
				}
				raw, err := w.EvalQuiet("return location.pathname")
				var path string
				return err == nil && json.Unmarshal(raw, &path) == nil && path == "/two"
			})
			closed := h.Call("windows", "close", map[string]any{"id": opened.ID})
			if !closed.OK {
				log.Fatalf("windows.close: %s %s", closed.Code, closed.Message)
			}
			h.Wait("the secondary window to close", func() bool { return h.Window(opened.ID) == nil })
		}
	}
	if os.Getenv(windowsActivationE2EEnv) == "1" {
		phaseWindowsDeepLinkActivation(h, log)
	}

	// This invokes the production ICoreWebView2::CapturePreview path.
	img := h.Snapshot(log)
	if img.Bounds().Dx() < 300 || img.Bounds().Dy() < 200 {
		log.Fatalf("CapturePreview returned a small image: %v", img.Bounds())
	}
	var viewport struct {
		Width  int `json:"w"`
		Height int `json:"h"`
	}
	h.EvalInto(log, "return {w: window.innerWidth, h: window.innerHeight}", &viewport)
	// The HWND dimensions include the title bar and borders. WebView2
	// CapturePreview reports content pixels at the current display scale.
	if img.Bounds().Dx() < viewport.Width || img.Bounds().Dy() < viewport.Height {
		log.Fatalf("CapturePreview size %v is smaller than viewport %+v", img.Bounds(), viewport)
	}

	if dir := os.Getenv("GOFASTR_DESKTOP_PROOF_DIR"); dir != "" {
		path := filepath.Join(dir, "windows-webview2-capture.png")
		if err := os.MkdirAll(dir, 0o700); err == nil {
			if f, err := os.Create(path); err == nil {
				_ = png.Encode(f, img)
				_ = f.Close()
				fmt.Printf("Windows CapturePreview proof: %s\n", path)
			}
		}
	}

	return !log.Failed()
}

func phaseWindowsDeepLinkActivation(h *desktoptest.NativeHarness, log *windowsPhaseLog) {
	h.RecordEvents(log, "deep_link")
	exe, err := os.Executable()
	if err != nil {
		log.Fatalf("locate the e2e executable: %v", err)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, exe, windowsE2EScheme+"://two")
	output, err := cmd.CombinedOutput()
	if err != nil {
		log.Fatalf("second app activation failed: %v; output: %s", err, output)
		return
	}
	h.WaitLocation("/two")
	event := h.WaitEvent("deep_link")
	var payload struct {
		URL  string `json:"url"`
		Path string `json:"path"`
	}
	if err := event.Unmarshal(&payload); err != nil || payload.URL != windowsE2EScheme+"://two" || payload.Path != "/two" {
		log.Fatalf("deep_link event = %+v (decoded %+v, err %v)", event, payload, err)
		return
	}
	h.Navigate("/")
}
