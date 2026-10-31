//go:build linux && (amd64 || arm64)

package linux

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/battery/desktop"
	"github.com/DonaldMurillo/gofastr/battery/desktop/internal/gtk"
	"github.com/DonaldMurillo/gofastr/internal/fileperm"
)

const trayGraphicalRole = "GOFASTR_TRAY_GRAPHICAL_CHILD"

// Each child owns one real GTK lifetime and its own disposable session bus.
// The fixture exports no watcher on the desktop bus. It validates native
// hide/reveal/destruction and actual WebKit pixels, not a panel's icon renderer.
func TestNativeTrayGraphical(t *testing.T) {
	if os.Getenv("GOFASTR_LINUX_NATIVE") != "1" || os.Getenv("GOFASTR_LINUX_TRAY") != "1" {
		t.Skip("set GOFASTR_LINUX_NATIVE=1 and GOFASTR_LINUX_TRAY=1 in a graphical session")
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, role := range []string{"recovery", "busy-gtk", "blocked-handler", "bus-death", "quit", "no-host", "no-bus"} {
		t.Run(role, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 65*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, executable, "-test.run=^TestTrayGraphicalChild$", "-test.v", "-test.timeout=60s")
			for _, entry := range os.Environ() {
				if !strings.HasPrefix(entry, trayGraphicalRole+"=") {
					cmd.Env = append(cmd.Env, entry)
				}
			}
			cmd.Env = append(cmd.Env, trayGraphicalRole+"="+role)
			output, err := nativeTrayChildOutput(cmd)
			if err != nil || !trayGraphicalChildPassed(output) {
				t.Fatalf("native tray %s failed: %v\n%s", role, err, output)
			}
			t.Log(strings.TrimSpace(string(output)))
		})
	}
}

func trayGraphicalChildPassed(output []byte) bool {
	return bytes.Contains(output, []byte("\n--- PASS: TestTrayGraphicalChild ("))
}

func TestTrayGraphicalRequiresChildPass(t *testing.T) {
	for _, output := range []string{"", "testing: warning: no tests to run\nPASS\n", "=== RUN   TestTrayGraphicalChild\n--- SKIP: TestTrayGraphicalChild (0.00s)\nPASS\n"} {
		if trayGraphicalChildPassed([]byte(output)) {
			t.Fatal("missing or skipped graphical child was accepted")
		}
	}
	if !trayGraphicalChildPassed([]byte("=== RUN   TestTrayGraphicalChild\n--- PASS: TestTrayGraphicalChild (1.00s)\nPASS\n")) {
		t.Fatal("actual child pass was rejected")
	}
}

func TestTrayGraphicalChild(t *testing.T) {
	role := os.Getenv(trayGraphicalRole)
	if role == "" || os.Getenv("GOFASTR_LINUX_NATIVE") != "1" || os.Getenv("GOFASTR_LINUX_TRAY") != "1" {
		t.Skip("graphical tray subprocess helper")
	}
	if role != "recovery" && role != "busy-gtk" && role != "blocked-handler" && role != "bus-death" && role != "quit" && role != "no-host" && role != "no-bus" {
		t.Fatal("unknown graphical tray fixture role")
	}
	if err := loadTrayGIO(); err != nil {
		t.Fatal(err)
	}
	baseline := trayLiveClosures.Load()
	var daemon *exec.Cmd
	var watcher *trayBroker
	var host *trayTestWatcher
	if role == "no-bus" {
		t.Setenv("DBUS_SESSION_BUS_ADDRESS", "")
	} else {
		daemon = startTrayTestBus(t)
		watcher, host = startTrayTestWatcher(t, role != "no-host")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<!doctype html><title>Tray recovery</title><style>body{font:24px sans-serif;background:#163451;color:white;padding:48px}</style><h1>Linux tray recovery verified</h1><p>Native GTK window · WebKitGTK · CGO_ENABLED=0</p><p>Close hides only with a live host. Host loss restores this window.</p>`)
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	s := New().(*linuxShell)
	result := make(chan error, 1)
	var main *linuxWindow
	probe := &trayGraphicalHandler{entered: make(chan struct{}, 1), release: make(chan struct{})}
	defer close(probe.release)
	cfg := desktop.WindowConfig{AppID: "dev.gofastr.tray-graphical", Title: "Linux tray recovery", Width: 840, Height: 520, Tray: &desktop.Tray{Title: "Tray fixture", CloseHidesWindow: true}}
	if role == "blocked-handler" {
		cfg.Tray.Menu = &desktop.Menu{Items: []desktop.MenuItem{{ID: "blocked", Navigate: "/"}}}
		cfg.OnTrayMenu = probe.invoke
		cfg.OnMenu = func(id string) { probe.wrongCallback.Store(true); probe.invoke(id) }
	}
	runErr := s.Run(ctx, cfg, func(window desktop.Window) {
		main = window.(*linuxWindow)
		// Match Battery's immediate ready-time title call, before waiting for
		// page load or eventual proof. Healthy hosts must not spuriously fail.
		titleErr := s.SetTrayTitle("configured fixture")
		if (role == "no-host" || role == "no-bus") != (titleErr != nil) {
			result <- errors.New("ready-time tray title availability did not match the host")
			s.Quit()
			return
		}
		if err := window.Navigate(server.URL); err != nil {
			result <- err
			s.Quit()
			return
		}
		go func() {
			defer s.Quit()
			result <- exerciseTrayGraphical(ctx, role, s, main, server.URL, watcher, host, daemon, probe)
		}()
	})
	if runErr != nil {
		t.Fatal(runErr)
	}
	select {
	case err := <-result:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("native tray driver did not finish")
	}
	if main == nil || !main.closed.Load() || s.running.Load() {
		t.Fatal("tray shutdown did not destroy main window")
	}
	if _, ok := trayShells.Load(s); ok {
		t.Fatal("tray retained shell after shutdown")
	}
	if watcher != nil {
		watcher.close()
	}
	trayTestWait(t, "graphical tray closure cleanup", func() bool { return trayLiveClosures.Load() == baseline })
	t.Log("real GTK close/show/loss/quit lifecycle passed: " + role)
}

type trayGraphicalHandler struct {
	entered, release chan struct{}
	calls            atomic.Int32
	wrongCallback    atomic.Bool
}

func (p *trayGraphicalHandler) invoke(string) {
	p.calls.Add(1)
	select {
	case p.entered <- struct{}{}:
	default:
	}
	<-p.release
}

func waitTrayGraphical(ctx context.Context, phase string, check func() bool) error {
	deadline := time.NewTimer(7 * time.Second)
	defer deadline.Stop()
	for !check() {
		select {
		case <-ctx.Done():
			return fmt.Errorf("%s: driver cancelled", phase)
		case <-deadline.C:
			return fmt.Errorf("%s: condition did not become true", phase)
		case <-time.After(20 * time.Millisecond):
		}
	}
	return nil
}

func trayGraphicalVisible(w *linuxWindow) bool {
	visible := false
	_ = w.withMain(func() { visible = call(gtk.Sym.GtkWidgetGetVisible, w.handle) != 0 })
	return visible
}

func exerciseTrayGraphical(ctx context.Context, role string, s *linuxShell, w *linuxWindow, origin string, watcher *trayBroker, host *trayTestWatcher, daemon *exec.Cmd, probe *trayGraphicalHandler) error {
	if err := waitTrayGraphical(ctx, "rendered native page", func() bool {
		value, err := w.EvalAsync(ctx, `return document.querySelector('h1')?.textContent || ''`)
		return err == nil && string(value) == `"Linux tray recovery verified"`
	}); err != nil {
		return err
	}
	value, present := trayShells.Load(s)
	if role == "no-bus" {
		if present || s.SetTrayTitle("updated") == nil || !trayGraphicalVisible(w) {
			return errors.New("unavailable bus did not leave a visible ordinary window and unsupported tray")
		}
	} else if !present {
		return errors.New("production shell did not start its tray broker")
	}
	if role == "no-host" || role == "no-bus" {
		if present && value.(*trayWindowState).broker.available.Load() {
			return errors.New("watcher without a live host enabled hiding")
		}
		if err := w.Close(); err != nil {
			return err
		}
		return waitTrayGraphical(ctx, "ordinary close without live host", w.closed.Load)
	}
	state := value.(*trayWindowState)
	if err := waitTrayGraphical(ctx, "live host proof", state.broker.available.Load); err != nil {
		return err
	}
	if err := s.SetTrayTitle("updated fixture"); err != nil {
		return fmt.Errorf("live host title: %w", err)
	}
	hide := func() error {
		if err := w.Close(); err != nil {
			return err
		}
		return waitTrayGraphical(ctx, "main close hides without destruction", func() bool {
			return !w.closed.Load() && state.hidden.Load() && !trayGraphicalVisible(w)
		})
	}
	revealed := func() error {
		return waitTrayGraphical(ctx, "lost host reveals main", func() bool {
			return !w.closed.Load() && !state.hidden.Load() && trayGraphicalVisible(w)
		})
	}
	if err := hide(); err != nil {
		return err
	}
	if role == "blocked-handler" {
		if err := trayGraphicalMenu(watcher, state.broker, 1); err != nil {
			return err
		}
		select {
		case <-probe.entered:
		case <-ctx.Done():
			return errors.New("custom tray callback did not start")
		}
		if probe.wrongCallback.Load() {
			return errors.New("tray used ordinary OnMenu instead of completion-aware OnTrayMenu")
		}
		for range 16 {
			if err := trayGraphicalMenu(watcher, state.broker, 1); err != nil {
				return err
			}
		}
		if trayGraphicalMenu(watcher, state.broker, 1) == nil {
			return errors.New("remote clicks exceeded one in-flight and sixteen pending callbacks")
		}
		host.host.Store(false)
		if err := revealed(); err != nil {
			return fmt.Errorf("blocked callback suppressed host-loss recovery: %w", err)
		}
		host.host.Store(true)
		if err := waitTrayGraphical(ctx, "host restored with callback blocked", state.broker.available.Load); err != nil {
			return err
		}
		if err := hide(); err != nil {
			return err
		}
		// Custom row1, separator2, mandatory Show3/Quit4.
		if err := trayGraphicalMenu(watcher, state.broker, 3); err != nil {
			return err
		}
		if err := revealed(); err != nil {
			return err
		}
		if probe.calls.Load() != 1 {
			return errors.New("native delivery spawned multiple blocked handlers")
		}
		if err := trayGraphicalMenu(watcher, state.broker, 4); err != nil {
			return err
		}
		return waitTrayGraphical(ctx, "Quit bypasses blocked callback", w.closed.Load)
	}
	if role == "quit" {
		if err := trayGraphicalMenu(watcher, state.broker, 2); err != nil {
			return err
		}
		return waitTrayGraphical(ctx, "explicit tray Quit destroys hidden main", w.closed.Load)
	}
	if role == "bus-death" {
		if err := daemon.Process.Kill(); err != nil {
			return err
		}
		return revealed()
	}
	if role == "busy-gtk" {
		// Deliberately occupy the actual GTK thread past Main's dispatch
		// deadline. The first queued reveal expires; the retained recovery
		// must retry after GTK resumes. Do not call a GTK observer while busy.
		entered, released := make(chan struct{}), make(chan struct{})
		go func() {
			_ = s.Main(func() {
				close(entered)
				select {
				case <-time.After(mainTimeout + 2*time.Second):
				case <-ctx.Done():
				}
				close(released)
			})
		}()
		select {
		case <-entered:
		case <-ctx.Done():
			return errors.New("GTK busy fixture did not start")
		}
		host.host.Store(false)
		select {
		case <-released:
		case <-ctx.Done():
			return errors.New("GTK busy fixture did not release")
		}
		return revealed()
	}
	if err := hide(); err != nil { // repeated close must not destroy the hidden main
		return err
	}
	if err := trayGraphicalMenu(watcher, state.broker, 1); err != nil {
		return err
	}
	if err := revealed(); err != nil {
		return err
	}
	second, err := s.OpenWindow("tray-secondary", desktop.WindowSpec{Title: "Tray secondary", Width: 320, Height: 240}, origin)
	if err != nil {
		return err
	}
	if err := second.Close(); err != nil {
		return err
	}
	if err := waitTrayGraphical(ctx, "secondary close destroys only secondary", second.(*linuxWindow).closed.Load); err != nil {
		return err
	}
	if w.closed.Load() || !trayGraphicalVisible(w) {
		return errors.New("secondary close affected main window")
	}
	coalesced := false
	if err := s.Main(func() {
		state.showAboutMain()
		first := state.about
		state.showAboutMain()
		coalesced = first != 0 && first == state.about
	}); err != nil {
		return err
	}
	aboutOK := false
	if err := s.Main(func() {
		if state.about != 0 {
			call(gtk.Sym.GtkWidgetDestroy, state.about)
			aboutOK = state.about == 0
		}
	}); err != nil || !aboutOK || !coalesced {
		return errors.New("tray About dialog did not coalesce and release on destruction")
	}
	if err := hide(); err != nil {
		return err
	}
	host.retain.Store(false)
	host.registered.Store(false)
	if err := revealed(); err != nil {
		return fmt.Errorf("item removed while host remains live: %w", err)
	}
	host.retain.Store(true)
	if err := waitTrayGraphical(ctx, "item re-registered", state.broker.available.Load); err != nil {
		return err
	}
	if err := hide(); err != nil {
		return err
	}
	host.host.Store(false)
	if err := revealed(); err != nil {
		return err
	}
	host.host.Store(true)
	if err := waitTrayGraphical(ctx, "host restored", state.broker.available.Load); err != nil {
		return err
	}
	if err := hide(); err != nil {
		return err
	}
	watcher.close()
	if err := revealed(); err != nil {
		return err
	}
	// Snapshot real WebKit pixels after recovery, not a DOM-only assertion.
	if hold, err := time.ParseDuration(os.Getenv("GOFASTR_LINUX_TEST_HOLD")); err == nil && hold > 0 && hold <= 15*time.Second {
		select {
		case <-time.After(hold):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	data, err := w.Snapshot(ctx)
	if err != nil {
		return err
	}
	config, err := png.DecodeConfig(bytes.NewReader(data))
	if err != nil || config.Width < 100 || config.Height < 100 {
		return errors.New("recovered WebKit snapshot was not a window-sized PNG")
	}
	if directory := os.Getenv("GOFASTR_LINUX_TEST_OUTPUT"); directory != "" {
		if err := os.MkdirAll(directory, 0700); err != nil {
			return err
		}
		return fileperm.WriteOwnerOnly(filepath.Join(directory, "tray-recovered-native.png"), data)
	}
	return nil
}

func trayGraphicalMenu(watcher, item *trayBroker, id int32) error {
	reply, err := watcher.call(item.uniqueName, trayMenuPath, trayMenuInterface, "Event", trayTuple(trayInt(id), trayString("clicked"), trayCall("g_variant_new_variant", trayInt(0)), trayCall("g_variant_new_uint32", 0)), "()")
	trayUnref(reply)
	return err
}
