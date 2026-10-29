//go:build linux && (amd64 || arm64)

package linux

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/battery/desktop"
)

const trayWatcherXML = `<node><interface name="org.kde.StatusNotifierWatcher">
<method name="RegisterStatusNotifierItem"><arg type="s" direction="in"/></method>
<property name="IsStatusNotifierHostRegistered" type="b" access="read"/>
</interface></node>`

type trayTestWatcher struct {
	host          atomic.Bool
	registrations atomic.Int32
}

func (w *trayTestWatcher) property(_, name string) uintptr {
	if name == "IsStatusNotifierHostRegistered" {
		return trayBool(w.host.Load())
	}
	return 0
}
func (w *trayTestWatcher) method(_, name string, args uintptr) (uintptr, error) {
	if name != "RegisterStatusNotifierItem" {
		return 0, errors.New("unknown method")
	}
	path, ok := trayArgString(args, 0)
	if !ok || path != trayPath {
		return 0, errors.New("invalid item")
	}
	w.registrations.Add(1)
	return 0, nil
}

func startTrayTestWatcher(t *testing.T, host bool) (*trayBroker, *trayTestWatcher) {
	t.Helper()
	w := &trayTestWatcher{}
	w.host.Store(host)
	b := newTrayBroker(&trayModel{}, nil, nil)
	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		defer close(b.done)
		ctx := trayCall("g_main_context_new")
		trayCall("g_main_context_push_thread_default", ctx)
		defer trayCall("g_main_context_unref", ctx)
		defer trayCall("g_main_context_pop_thread_default", ctx)
		if err := b.connect(); err != nil {
			b.ready <- err
			return
		}
		defer b.cleanup(ctx)
		reg, err := registerTrayEndpoint(b.connection, "/StatusNotifierWatcher", trayWatcherXML, "org.kde.StatusNotifierWatcher", w)
		if err != nil {
			b.ready <- err
			return
		}
		b.registrations = append(b.registrations, reg)
		reply, err := b.call("org.freedesktop.DBus", "/org/freedesktop/DBus", "org.freedesktop.DBus", "RequestName", trayTuple(trayString("org.kde.StatusNotifierWatcher"), trayCall("g_variant_new_uint32", 4)), "(u)")
		if err != nil {
			b.ready <- err
			return
		}
		value := trayChild(reply, 0)
		owner := trayCall("g_variant_get_uint32", value)
		trayUnref(value)
		trayUnref(reply)
		if owner != 1 {
			b.ready <- errors.New("watcher name unavailable")
			return
		}
		b.ready <- nil
		for !b.stopping.Load() {
			for i := 0; i < 64 && trayCall("g_main_context_pending", ctx) != 0; i++ {
				trayCall("g_main_context_iteration", ctx, 0)
			}
			time.Sleep(time.Millisecond)
		}
	}()
	if err := <-b.ready; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(b.close)
	return b, w
}
func trayTestWait(t *testing.T, description string, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(6 * time.Second)
	for !condition() {
		if time.Now().After(deadline) {
			t.Fatal("timed out: " + description)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func startTrayTestBus(t *testing.T) *exec.Cmd {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Second)
	t.Cleanup(cancel)
	command := exec.CommandContext(ctx, "dbus-daemon", "--session", "--nofork", "--print-address=1", "--nopidfile")
	fixtureHome := t.TempDir()
	runtimeDir := filepath.Join(fixtureHome, "run")
	if err := os.Mkdir(runtimeDir, 0o700); err != nil {
		t.Fatal(err)
	}
	command.Env = append(os.Environ(), "HOME="+fixtureHome, "XDG_RUNTIME_DIR="+runtimeDir, "XDG_DATA_HOME="+filepath.Join(fixtureHome, "data"), "XDG_CONFIG_HOME="+filepath.Join(fixtureHome, "config"))
	var daemonError bytes.Buffer
	command.Stderr = &daemonError
	stdout, err := command.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = command.Process.Kill(); _ = command.Wait() })
	address, err := bufio.NewReader(stdout).ReadString('\n')
	if err != nil {
		_ = command.Wait()
		t.Fatalf("private bus did not report an address: %v (%s)", err, daemonError.String())
	}
	t.Setenv("DBUS_SESSION_BUS_ADDRESS", strings.TrimSpace(address))
	return command
}

// This test creates its OWN disposable bus. It never exports a watcher onto
// the developer's desktop session and does not claim visible icon rendering.
func TestNativeTray(t *testing.T) {
	if os.Getenv("GOFASTR_LINUX_TRAY") != "1" {
		t.Skip("set GOFASTR_LINUX_TRAY=1 for private-bus native tray contract tests")
	}
	if err := loadTrayGIO(); err != nil {
		t.Fatal(err)
	}
	command := startTrayTestBus(t)
	baseline := trayLiveClosures.Load()
	watcher, host := startTrayTestWatcher(t, false)
	model, err := newTrayModel(context.Background(), desktop.WindowConfig{AppID: "dev.gofastr.tray-test", Title: "Tray fixture", Tray: &desktop.Tray{Title: "Tray_test"}})
	if err != nil {
		t.Fatal(err)
	}
	actions := make(chan trayAction, 16)
	lost := make(chan struct{}, 4)
	b := newTrayBroker(model, func(action trayAction) { actions <- action }, func() bool { lost <- struct{}{}; return true })
	if err := b.start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(b.close)
	trayTestWait(t, "watcher registration without host", func() bool { return host.registrations.Load() > 0 })
	if b.available.Load() {
		t.Fatal("watcher without host made close-to-tray safe")
	}
	host.host.Store(true)
	trayTestWait(t, "live host", b.available.Load)
	// Real native property closure dispatch (five GValues), not a Go-only map.
	reply, err := watcher.call(b.uniqueName, trayPath, "org.freedesktop.DBus.Properties", "GetAll", trayTuple(trayString(trayInterfaces[0])), "(a{sv})")
	if err != nil {
		t.Fatal(err)
	}
	trayUnref(reply)
	reply, err = watcher.call(b.uniqueName, trayMenuPath, trayMenuInterface, "GetLayout", trayTuple(trayInt(0), trayInt(-1), trayArray("s")), "(u(ia{sv}av))")
	if err != nil {
		t.Fatal(err)
	}
	layout := trayChild(reply, 1)
	children := trayChild(layout, 2)
	if got := trayCall("g_variant_n_children", children); got != 2 {
		t.Fatalf("recovery rows=%d", got)
	}
	trayUnref(children)
	trayUnref(layout)
	trayUnref(reply)
	for _, iface := range trayInterfaces {
		reply, err = watcher.call(b.uniqueName, trayPath, "org.freedesktop.DBus.Properties", "Get", trayTuple(trayString(iface), trayString("WindowId")), "(v)")
		if err != nil {
			t.Fatal(err)
		}
		outer := trayChild(reply, 0)
		value := trayCall("g_variant_get_variant", outer)
		typ := "i"
		if iface == "org.freedesktop.StatusNotifierItem" {
			typ = "u"
		}
		want := trayType(typ)
		matches := trayCall("g_variant_is_of_type", value, want) != 0
		trayCall("g_variant_type_free", want)
		trayUnref(value)
		trayUnref(outer)
		trayUnref(reply)
		if !matches {
			t.Fatal("WindowId ABI type mismatch for " + iface)
		}
	}
	for _, query := range []struct {
		ids  []uintptr
		want int
	}{{nil, len(model.nodes)}, {[]uintptr{trayInt(1)}, 1}, {[]uintptr{trayInt(999)}, 0}} {
		reply, err = watcher.call(b.uniqueName, trayMenuPath, trayMenuInterface, "GetGroupProperties", trayTuple(trayArray("i", query.ids...), trayArray("s", trayString("label"))), "(a(ia{sv}))")
		if err != nil {
			t.Fatal(err)
		}
		rows := trayChild(reply, 0)
		got := int(trayCall("g_variant_n_children", rows))
		trayUnref(rows)
		trayUnref(reply)
		if got != query.want {
			t.Fatalf("group count=%d want%d", got, query.want)
		}
	}
	if err := b.setTitle("changed_title"); err != nil {
		t.Fatal(err)
	}
	reply, err = watcher.call(b.uniqueName, trayPath, "org.freedesktop.DBus.Properties", "Get", trayTuple(trayString(trayInterfaces[0]), trayString("Title")), "(v)")
	if err != nil {
		t.Fatal(err)
	}
	v := trayChild(reply, 0)
	titleValue := trayCall("g_variant_get_variant", v)
	title, _ := trayReadString(titleValue, trayTextLimit)
	trayUnref(titleValue)
	trayUnref(v)
	trayUnref(reply)
	if title != "changed_title" {
		t.Fatal("title not updated")
	}
	for _, id := range []int32{1, 2} {
		reply, err = watcher.call(b.uniqueName, trayMenuPath, trayMenuInterface, "Event", trayTuple(trayInt(id), trayString("clicked"), trayCall("g_variant_new_variant", trayInt(0)), trayCall("g_variant_new_uint32", 0)), "()")
		if err != nil {
			t.Fatal(err)
		}
		trayUnref(reply)
	}
	for _, role := range []string{desktop.RoleShow, desktop.RoleQuit} {
		select {
		case action := <-actions:
			if action.plan.Role != role {
				t.Fatalf("action=%s want=%s", action.plan.Role, role)
			}
		case <-time.After(time.Second):
			t.Fatal("missing action")
		}
	}
	reply, err = watcher.call(b.uniqueName, trayMenuPath, trayMenuInterface, "Event", trayTuple(trayInt(0), trayString("clicked"), trayCall("g_variant_new_variant", trayInt(0)), trayCall("g_variant_new_uint32", 0)), "()")
	trayUnref(reply)
	if err == nil {
		t.Fatal("root click accepted")
	}
	host.host.Store(false)
	trayTestWait(t, "host loss", func() bool { return !b.available.Load() })
	select {
	case <-lost:
	case <-time.After(time.Second):
		t.Fatal("host loss did not request reveal")
	}
	host.host.Store(true)
	trayTestWait(t, "host return", b.available.Load)
	watcher.close()
	trayTestWait(t, "watcher loss", func() bool { return !b.available.Load() })
	_, replacement := startTrayTestWatcher(t, true)
	trayTestWait(t, "watcher replacement", func() bool { return b.available.Load() && replacement.registrations.Load() > 0 })
	if err := command.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	trayTestWait(t, "bus death", func() bool { return !b.available.Load() })
	b.close()
	// The fake watcher owns two closures until its cleanup; the item owns six.
	trayTestWait(t, "item callback cleanup", func() bool { return trayLiveClosures.Load() <= baseline+2 })
	t.Log("private-bus SNI properties/menu/actions, no-host guard, loss/replacement, bus death and callback cleanup passed; no visual tray claim")
}
