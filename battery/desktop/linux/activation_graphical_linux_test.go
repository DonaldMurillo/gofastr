//go:build linux && (amd64 || arm64)

package linux

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image/png"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/battery/desktop"
	appui "github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/framework/ui"
	"github.com/DonaldMurillo/gofastr/framework/uihost"
	"github.com/DonaldMurillo/gofastr/internal/fileperm"
)

const (
	graphicalActivationRole = "GOFASTR_ACTIVATION_GRAPHICAL_CHILD"
	graphicalActivationID   = "GOFASTR_ACTIVATION_GRAPHICAL_ID"
	graphicalColdURL        = "gofastr605test://cold?source=%2f#cold-fragment"
	graphicalWarmURL        = "gofastr605test://warm?source=%2f#warm-fragment"
	graphicalSentinelURL    = "gofastr605test://warm?after=invalid#sentinel"
	graphicalWindowTitle    = "Activation GTK fixture"
)

// Run in a private session bus with an authenticated display, separately from
// other GTK lifetimes. Build this test binary with:
// -ldflags=-X=github.com/DonaldMurillo/gofastr/battery/desktop.builtDeepLinkScheme=gofastr605test
// Cold and warm URLs enter real subprocess argv; no callback or OS handler is
// forged, and no desktop protocol registration is installed by this fixture.
func TestNativeActivationGraphical(t *testing.T) {
	if os.Getenv("GOFASTR_LINUX_NATIVE") != "1" || os.Getenv("GOFASTR_LINUX_ACTIVATION") != "1" {
		t.Skip("set GOFASTR_LINUX_NATIVE=1 and GOFASTR_LINUX_ACTIVATION=1 in a private bus and graphical session")
	}
	if os.Getenv("DBUS_SESSION_BUS_ADDRESS") == "" {
		t.Fatal("graphical activation test requires an explicitly supplied private session bus")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	id := fmt.Sprintf("dev.gofastr.graphical.%d.%d", os.Getpid(), time.Now().UnixNano())
	output, err := runGraphicalActivationChild(ctx, id, "primary", graphicalColdURL)
	if err != nil {
		t.Fatalf("graphical activation child failed: %v\n%s", err, output)
	}
	t.Log(strings.TrimSpace(output))
}

// Each helper process has at most one real GTK lifetime. A secondary drives
// Battery.Run too, but must return before app.Start and shell.Run.
func TestActivationGraphicalChild(t *testing.T) {
	role := os.Getenv(graphicalActivationRole)
	if role == "" {
		t.Skip("graphical activation subprocess helper")
	}
	if role != "primary" && role != "secondary" {
		t.Fatal("unknown graphical activation helper role")
	}
	t.Setenv(desktop.ManifestEnv, "")
	t.Setenv("GOFASTR_DESKTOP_DATA_DIR", t.TempDir())
	t.Setenv("GOFASTR_ISOLATION", "off")
	id := os.Getenv(graphicalActivationID)
	evidence := &graphicalActivationEvidence{}
	shell := &graphicalActivationShell{linuxShell: New().(*linuxShell), secondary: role == "secondary", evidence: evidence}
	probe := &graphicalStartProbe{}
	app, battery := newGraphicalActivationApp(id, shell, probe)
	if role == "secondary" {
		if err := battery.Run(app); err != nil {
			t.Fatalf("secondary Battery.Run failed: %v", err)
		}
		if shell.prepared.Load() != 1 || !shell.handled.Load() || probe.started.Load() != 0 || shell.runs.Load() != 0 {
			t.Fatalf("secondary lifecycle: prepared=%d handled=%v appInit=%d shellRun=%d", shell.prepared.Load(), shell.handled.Load(), probe.started.Load(), shell.runs.Load())
		}
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 75*time.Second)
	defer cancel()
	result := make(chan error, 1)
	closureBaseline := activationLiveClosures.Load()
	go func() {
		defer shell.Quit()
		result <- exerciseGraphicalActivation(ctx, id, battery, evidence)
	}()
	// A driver timeout also requests shutdown even if a native operation stalls.
	watchdog := time.AfterFunc(80*time.Second, shell.Quit)
	defer watchdog.Stop()
	runErr := battery.Run(app)
	cancel()
	if runErr != nil {
		t.Fatalf("primary Battery.Run failed: %s", evidence.redact(runErr.Error()))
	}
	if err := <-result; err != nil {
		t.Fatal(evidence.redact(err.Error()))
	}
	if shell.prepared.Load() != 1 || shell.handled.Load() || probe.started.Load() != 1 || shell.runs.Load() != 1 {
		t.Fatalf("primary lifecycle: prepared=%d handled=%v appInit=%d shellRun=%d (build with the documented embedded-scheme ldflags)", shell.prepared.Load(), shell.handled.Load(), probe.started.Load(), shell.runs.Load())
	}
	if live := activationLiveClosures.Load(); live != closureBaseline {
		t.Fatalf("graphical shutdown retained native closures: live=%d baseline=%d", live, closureBaseline)
	}
}

type graphicalStartProbe struct{ started atomic.Int32 }

func (*graphicalStartProbe) Name() string { return "activationstartprobe" }
func (p *graphicalStartProbe) Init(*framework.App) error {
	p.started.Add(1)
	return nil
}

type graphicalActivationScreen string

func (s graphicalActivationScreen) Render() render.HTML {
	return html.Heading(html.HeadingConfig{Level: 1}, render.Text(string(s)))
}

func newGraphicalActivationApp(id string, shell *graphicalActivationShell, probe *graphicalStartProbe) (*framework.App, *desktop.Battery) {
	site := appui.NewApp("Activation GTK fixture")
	layout := appui.NewLayout("main", appui.LayoutSpec{}, func(_ context.Context, l *appui.LayoutTree) render.HTML {
		return ui.Container(ui.ContainerConfig{Width: ui.ContainerPage, Pad: ui.ContainerPadPage}, l.Primary())
	})
	for path, title := range map[string]string{"/": "Activation home", "/cold": "Cold activation page", "/warm": "Warm activation page"} {
		site.Register(path, graphicalActivationScreen(title), layout)
	}
	app := framework.NewApp(framework.WithConfig(framework.AppConfig{Name: "activation-graphical"}))
	// The assertion diagnostics below are deliberate and bounded. Do not let
	// generic HTTP/native errors accidentally print a boot URL or cookie.
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	app.SetLogger(logger)
	app.Mount(uihost.New(site))
	battery := desktop.New(desktop.Config{ID: id, Title: graphicalWindowTitle, Shell: shell, Logger: logger, KeepIsolation: true,
		DeepLink: &desktop.DeepLinkConfig{Scheme: "gofastr605test"}})
	app.RegisterBattery(probe)
	app.RegisterBattery(battery, probe.Name())
	return app, battery
}

type graphicalActivationShell struct {
	*linuxShell
	secondary bool
	evidence  *graphicalActivationEvidence
	prepared  atomic.Int32
	handled   atomic.Bool
	runs      atomic.Int32
}

func (s *graphicalActivationShell) PrepareLaunch(id, name, scheme string) (bool, []string, error) {
	s.prepared.Add(1)
	handled, urls, err := s.linuxShell.PrepareLaunch(id, name, scheme)
	s.handled.Store(handled)
	return handled, urls, err
}
func (s *graphicalActivationShell) Run(ctx context.Context, cfg desktop.WindowConfig, ready func(desktop.Window)) error {
	s.runs.Add(1)
	if s.prepared.Load() != 1 {
		return errors.New("embedded launch gate was not exercised; build with the documented builtDeepLinkScheme ldflags")
	}
	if s.secondary {
		return errors.New("secondary unexpectedly reached shell.Run")
	}
	return s.linuxShell.Run(ctx, cfg, func(w desktop.Window) {
		ready(&graphicalActivationWindow{linuxWindow: w.(*linuxWindow), evidence: s.evidence})
	})
}

type graphicalActivationEvidence struct {
	mu      sync.Mutex
	bootURL string // memory-only: never included in diagnostic output
}

func (e *graphicalActivationEvidence) redact(message string) string {
	e.mu.Lock()
	boot := e.bootURL
	e.mu.Unlock()
	if boot != "" {
		message = strings.ReplaceAll(message, boot, "<boot URL redacted>")
		if u, err := url.Parse(boot); err == nil && u.Query().Get("t") != "" {
			message = strings.ReplaceAll(message, u.Query().Get("t"), "<boot token redacted>")
		}
	}
	return message
}

type graphicalActivationWindow struct {
	*linuxWindow // Preserve the real PageEvaluator and every other native method.
	evidence     *graphicalActivationEvidence
}

func (w *graphicalActivationWindow) Navigate(raw string) error {
	if u, err := url.Parse(raw); err == nil && u.Path == "/__gofastr/desktop/enter" {
		w.evidence.mu.Lock()
		w.evidence.bootURL = raw
		w.evidence.mu.Unlock()
	}
	return w.linuxWindow.Navigate(raw)
}
func (w *graphicalActivationWindow) Eval(js string) error {
	if strings.Contains(js, "window.__gofastr.desktop._dispatch(") {
		// Install an observer immediately before the ORIGINAL native dispatch.
		// No dispatch, URL, runtime readiness or navigation is synthesized. If
		// the desktop runtime is absent, the real event remains absent and fails.
		js = `(() => { const d = window.__gofastr && window.__gofastr.desktop;
if (d && typeof d.on === 'function' && !window.__gofastrActivationEvents) {
 const events = window.__gofastrActivationEvents = [];
 d.on('deep_link', p => { if (events.length < 16) events.push({url:p.url,path:p.path}); });
} })();` + js
	}
	return w.linuxWindow.Eval(js)
}

type graphicalActivationEvent struct {
	URL  string `json:"url"`
	Path string `json:"path"`
}
type graphicalActivationState struct {
	Path, Search, Hash, Heading string
	Ready                       bool
	Events                      []graphicalActivationEvent
	Paths                       []string
}

func readGraphicalActivationState(ctx context.Context, evaluator desktop.PageEvaluator) (graphicalActivationState, error) {
	var state graphicalActivationState
	raw, err := evaluator.EvalAsync(ctx, `return {
 path:location.pathname,
 search:location.pathname === '/__gofastr/desktop/enter' ? '<redacted>' : location.search,
 hash:location.hash, heading:document.querySelector('h1')?.textContent || '',
 ready:!!(window.__gofastr && __gofastr.desktop && __gofastr.desktop.manifest),
 events:window.__gofastrActivationEvents || [], paths:window.__gofastrActivationPaths || []};`)
	if err != nil {
		return state, errors.New("page evaluation unavailable")
	}
	if err := json.Unmarshal(raw, &state); err != nil {
		return state, errors.New("page observation did not decode")
	}
	return state, nil
}

func waitGraphicalActivation(ctx context.Context, evaluator desktop.PageEvaluator, phase, path, search, heading string, events []graphicalActivationEvent, paths []string) error {
	var last graphicalActivationState
	for {
		probe, cancel := context.WithTimeout(ctx, 2*time.Second)
		state, err := readGraphicalActivationState(probe, evaluator)
		cancel()
		if err == nil {
			last = state
			if state.Ready && state.Path == path && state.Search == search && state.Hash == "" && state.Heading == heading && reflect.DeepEqual(state.Events, events) && (paths == nil || reflect.DeepEqual(state.Paths, paths)) {
				return nil
			}
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("%s: timed out waiting for native route and exact page events; last=%+v", phase, last)
		case <-time.After(40 * time.Millisecond):
		}
	}
}

func exerciseGraphicalActivation(ctx context.Context, id string, battery *desktop.Battery, evidence *graphicalActivationEvidence) error {
	var evaluator desktop.PageEvaluator
	for evaluator == nil {
		if w, ok := battery.Window(); ok {
			evaluator, _ = w.(desktop.PageEvaluator)
		}
		if evaluator == nil {
			select {
			case <-ctx.Done():
				return errors.New("primary native window did not open")
			case <-time.After(20 * time.Millisecond):
			}
		}
	}
	events := []graphicalActivationEvent{{graphicalColdURL, "/cold?source=%2f"}}
	if err := waitGraphicalActivation(ctx, evaluator, "cold argv", "/cold", "?source=%2f", "Cold activation page", events, nil); err != nil {
		return err
	}
	fmt.Println("GRAPHICAL cold argv: native route, rendered page and exact deep_link event passed")
	if err := checkGraphicalSessionGates(ctx, evaluator, evidence, true); err != nil {
		return err
	}
	if output, err := runGraphicalActivationChild(ctx, id, "secondary", graphicalWarmURL); err != nil {
		return fmt.Errorf("warm secondary failed: %v; %s", err, output)
	}
	events = append(events, graphicalActivationEvent{graphicalWarmURL, "/warm?source=%2f"})
	if err := waitGraphicalActivation(ctx, evaluator, "warm subprocess", "/warm", "?source=%2f", "Warm activation page", events, nil); err != nil {
		return err
	}
	fmt.Println("GRAPHICAL warm subprocess: Battery.Run forwarded without app/shell startup; native route/event passed")
	if _, err := evaluator.EvalAsync(ctx, `window.__gofastrActivationPaths = [];
window.addEventListener('gofastr:navigate', () => {
 const paths = window.__gofastrActivationPaths;
 if (paths.length < 16) paths.push(location.pathname + location.search);
}); return true;`); err != nil {
		return errors.New("could not install native-page route observer for refusal checks")
	}
	for _, raw := range []string{"gofastr605test://user:fixture@cold", "gofastr605test://warm/../cold"} {
		if output, err := runGraphicalActivationChild(ctx, id, "secondary", raw); err != nil {
			return fmt.Errorf("portable rejection secondary failed: %v; %s", err, output)
		}
	}
	// A later accepted link is an ordered completion barrier for both refused
	// URLs. Check the entire event list, not a timing-dependent negative sleep.
	if output, err := runGraphicalActivationChild(ctx, id, "secondary", graphicalSentinelURL); err != nil {
		return fmt.Errorf("sentinel secondary failed: %v; %s", err, output)
	}
	events = append(events, graphicalActivationEvent{graphicalSentinelURL, "/warm?after=invalid"})
	if err := waitGraphicalActivation(ctx, evaluator, "portable refusal barrier", "/warm", "?after=invalid", "Warm activation page", events, []string{"/warm?after=invalid"}); err != nil {
		return err
	}
	if err := checkGraphicalSessionGates(ctx, evaluator, evidence, false); err != nil {
		return err
	}
	fmt.Println("GRAPHICAL userinfo/traversal refused; ordered sentinel, session gate and authenticated bridge passed")
	if hold, err := time.ParseDuration(os.Getenv("GOFASTR_LINUX_TEST_HOLD")); err == nil && hold > 0 && hold <= 30*time.Second {
		select {
		case <-time.After(hold):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return snapshotGraphicalActivation(ctx, battery)
}

func checkGraphicalSessionGates(ctx context.Context, evaluator desktop.PageEvaluator, evidence *graphicalActivationEvidence, replay bool) error {
	raw, err := evaluator.EvalAsync(ctx, `const response = await fetch('/', {credentials:'same-origin'});
const title = await __gofastr.desktop.window.title();
return {status:response.status,title:title.title,origin:location.origin,cookieVisible:document.cookie.includes('__gofastr_desktop')};`)
	if err != nil {
		return errors.New("authenticated page or desktop bridge probe failed")
	}
	var page struct {
		Status        int
		Title, Origin string
		CookieVisible bool
	}
	if json.Unmarshal(raw, &page) != nil || page.Status != 200 || page.Title != graphicalWindowTitle || page.CookieVisible {
		return fmt.Errorf("authenticated page/bridge/HttpOnly check failed: status=%d title=%q cookieVisible=%v", page.Status, page.Title, page.CookieVisible)
	}
	origin, err := url.Parse(page.Origin)
	if err != nil || origin.Scheme != "http" || origin.Hostname() != "127.0.0.1" || origin.Port() == "" || origin.Path != "" {
		return errors.New("page origin is not the expected loopback listener")
	}
	client := &http.Client{Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	defer client.CloseIdleConnections()
	for _, endpoint := range []struct{ method, path string }{{http.MethodGet, "/"}, {http.MethodPost, "/__gofastr/desktop/call/window/title"}} {
		req, err := http.NewRequestWithContext(ctx, endpoint.method, page.Origin+endpoint.path, strings.NewReader("{}"))
		if err != nil {
			return errors.New("cookieless request construction failed")
		}
		req.Header.Set("Content-Type", "application/json")
		response, err := client.Do(req)
		if err != nil {
			return errors.New("cookieless loopback request failed")
		}
		response.Body.Close()
		if response.StatusCode != http.StatusForbidden {
			return fmt.Errorf("cookieless %s %s status=%d, want 403", endpoint.method, endpoint.path, response.StatusCode)
		}
	}
	if replay {
		evidence.mu.Lock()
		boot := evidence.bootURL
		evidence.mu.Unlock()
		if boot == "" {
			return errors.New("native window did not observe the real boot navigation")
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, boot, nil)
		if err != nil {
			return errors.New("consumed boot-token replay construction failed")
		}
		response, err := client.Do(req)
		if err != nil {
			return errors.New("consumed boot-token replay transport failed")
		}
		response.Body.Close()
		if response.StatusCode != http.StatusForbidden {
			return fmt.Errorf("consumed boot-token replay status=%d, want 403", response.StatusCode)
		}
		fmt.Println("GRAPHICAL boot/session: authenticated page+bridge passed, cookieless access and consumed-token replay refused")
	}
	return nil
}

// Capture actual WebKit pixels after the final rendered route, not a synthetic
// rendering or a DOM assertion. The optional output uses the existing native
// test convention; the PNG is validated even when no output directory is set.
func snapshotGraphicalActivation(ctx context.Context, battery *desktop.Battery) error {
	window, ok := battery.Window()
	if !ok {
		return errors.New("activation window closed before native snapshot")
	}
	snapshotter, ok := window.(interface {
		Snapshot(context.Context) ([]byte, error)
	})
	if !ok {
		return errors.New("activation window has no native snapshot support")
	}
	data, err := snapshotter.Snapshot(ctx)
	if err != nil {
		return errors.New("native activation snapshot failed")
	}
	config, err := png.DecodeConfig(bytes.NewReader(data))
	if err != nil || config.Width < 100 || config.Height < 100 {
		return errors.New("native activation snapshot is not a valid window-sized PNG")
	}
	if directory := os.Getenv("GOFASTR_LINUX_TEST_OUTPUT"); directory != "" {
		if err := os.MkdirAll(directory, 0700); err != nil {
			return err
		}
		if err := fileperm.WriteOwnerOnly(filepath.Join(directory, "activation-native.png"), data); err != nil {
			return err
		}
	}
	fmt.Printf("GRAPHICAL native snapshot validated: %dx%d\n", config.Width, config.Height)
	return nil
}

func runGraphicalActivationChild(ctx context.Context, id, role, rawURL string) (string, error) {
	executable, err := os.Executable()
	if err != nil {
		return "", err
	}
	childCtx, cancel := context.WithTimeout(ctx, 110*time.Second)
	if role == "secondary" {
		cancel()
		childCtx, cancel = context.WithTimeout(ctx, 15*time.Second)
	}
	defer cancel()
	cmd := exec.CommandContext(childCtx, executable, "-test.run=^TestActivationGraphicalChild$", "-test.v", "-test.timeout=105s", "--", rawURL)
	for _, value := range os.Environ() {
		if !strings.HasPrefix(value, graphicalActivationRole+"=") && !strings.HasPrefix(value, graphicalActivationID+"=") {
			cmd.Env = append(cmd.Env, value)
		}
	}
	cmd.Env = append(cmd.Env, graphicalActivationRole+"="+role, graphicalActivationID+"="+id)
	output, err := cmd.CombinedOutput()
	return string(output), err
}
