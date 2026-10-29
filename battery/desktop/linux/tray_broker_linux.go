//go:build linux && (amd64 || arm64)

package linux

import (
	"errors"
	"log/slog"
	"os"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	"github.com/DonaldMurillo/gofastr/battery/desktop"
	"github.com/DonaldMurillo/gofastr/core/textsafe"
)

const trayPath = "/StatusNotifierItem"
const trayMenuPath = "/StatusNotifierItem/Menu"
const trayMenuInterface = "com.canonical.dbusmenu"

var trayInterfaces = []string{"org.kde.StatusNotifierItem", "org.freedesktop.StatusNotifierItem"}

type trayAction struct {
	plan desktop.MenuItemPlan
	id   string
}
type trayBroker struct {
	model                 *trayModel
	mu                    sync.Mutex
	title                 string
	cancel                uintptr // protected by mu, alive until watcher exited
	connection            uintptr // immutable after ready, owned by broker thread
	registrations         []uint32
	uniqueName            string
	available             atomic.Bool
	stopping              atomic.Bool
	stop, done, watchDone chan struct{}
	ready                 chan error
	updates, recovery     chan struct{}
	actions               chan trayAction
	onAction              func(trayAction)
	onLost                func() bool
	closeOnce             sync.Once
	panicLogged           atomic.Bool
}

func newTrayBroker(model *trayModel, onAction func(trayAction), onLost func() bool) *trayBroker {
	return &trayBroker{model: model, title: model.title, stop: make(chan struct{}), done: make(chan struct{}), watchDone: make(chan struct{}), ready: make(chan error, 1), updates: make(chan struct{}, 1), recovery: make(chan struct{}, 1), actions: make(chan trayAction, 16), onAction: onAction, onLost: onLost}
}

func (b *trayBroker) start() error {
	if err := loadTrayGIO(); err != nil {
		return err
	}
	go b.run()
	if err := <-b.ready; err != nil {
		return err
	}
	go b.deliver()
	return nil
}
func (b *trayBroker) setAvailable(available bool) {
	if b.stopping.Load() {
		available = false
	}
	if b.available.Swap(available) && !available {
		select {
		case b.recovery <- struct{}{}:
		default:
		}
	}
}
func (b *trayBroker) enqueue(action trayAction) bool {
	if b.stopping.Load() {
		return false
	}
	select {
	case b.actions <- action:
		return true
	default:
		return false
	}
}
func (b *trayBroker) deliver() {
	pendingRecovery := false
	for {
		if pendingRecovery {
			if b.onLost == nil || b.deliverRecovery() {
				pendingRecovery = false
			} else {
				select {
				case <-b.stop:
					return
				case <-time.After(100 * time.Millisecond):
				}
				continue
			}
		}
		select {
		case <-b.stop:
			return
		case <-b.recovery:
			pendingRecovery = true
		case action := <-b.actions:
			if b.onAction != nil {
				b.deliverAction(action)
			}
		}
	}
}

func (b *trayBroker) logCallbackPanic(value any) {
	if b.panicLogged.CompareAndSwap(false, true) {
		slog.Default().Error("desktop/linux: tray callback panicked", "panic", textsafe.Recovered(value))
	}
}
func (b *trayBroker) deliverAction(action trayAction) {
	defer func() {
		if value := recover(); value != nil {
			b.logCallbackPanic(value)
		}
	}()
	b.onAction(action)
}
func (b *trayBroker) deliverRecovery() (done bool) {
	defer func() {
		if value := recover(); value != nil {
			b.logCallbackPanic(value)
			done = false
		}
	}()
	return b.onLost()
}

func (b *trayBroker) setTitle(title string) error {
	if !validTrayText(title) {
		return errors.New("desktop/linux: invalid tray title")
	}
	if b.stopping.Load() {
		return errStopped()
	}
	b.mu.Lock()
	b.title = title
	b.mu.Unlock()
	select {
	case b.updates <- struct{}{}:
	default:
	}
	if !b.available.Load() {
		return &desktop.Error{Code: desktop.CodeUnsupported, Message: "no registered Linux status notifier host is available; the window remains visible"}
	}
	return nil
}
func (b *trayBroker) close() {
	b.closeOnce.Do(func() {
		b.stopping.Store(true)
		b.setAvailable(false)
		close(b.stop)
		b.mu.Lock()
		if b.cancel != 0 {
			trayCall("g_cancellable_cancel", b.cancel)
		}
		b.mu.Unlock()
		<-b.done
	})
}

func (b *trayBroker) run() {
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
	for _, iface := range trayInterfaces {
		reg, err := registerTrayEndpoint(b.connection, trayPath, trayItemXML(iface), iface, b)
		if err != nil {
			b.ready <- err
			return
		}
		b.registrations = append(b.registrations, reg)
	}
	reg, err := registerTrayEndpoint(b.connection, trayMenuPath, trayMenuXML, trayMenuInterface, b)
	if err != nil {
		b.ready <- err
		return
	}
	b.registrations = append(b.registrations, reg)
	go b.watch()
	b.ready <- nil
	for {
		if b.stopping.Load() {
			break
		}
		if trayCall("g_dbus_connection_is_closed", b.connection) != 0 {
			b.setAvailable(false)
			break
		}
		// Bound one dispatch turn so a peer cannot prevent shutdown or updates.
		for i := 0; i < 64 && trayCall("g_main_context_pending", ctx) != 0; i++ {
			trayCall("g_main_context_iteration", ctx, 0)
		}
		select {
		case <-b.updates:
			for _, iface := range trayInterfaces {
				b.emit(iface, "NewTitle")
				b.emit(iface, "NewToolTip")
			}
		default:
		}
		time.Sleep(5 * time.Millisecond)
	}
	b.mu.Lock()
	if b.cancel != 0 {
		trayCall("g_cancellable_cancel", b.cancel)
	}
	b.mu.Unlock()
	<-b.watchDone
}

func (b *trayBroker) connect() error {
	address := os.Getenv("DBUS_SESSION_BUS_ADDRESS")
	if !validActivationAddress(address) {
		return errors.New("desktop/linux: tray requires an existing local Unix session bus; window stays visible")
	}
	b.mu.Lock()
	b.cancel = trayCall("g_cancellable_new")
	cancel := b.cancel
	b.mu.Unlock()
	var nativeError uintptr
	addressBytes, p := activationCString(address)
	finished := make(chan struct{})
	timer := time.AfterFunc(3*time.Second, func() { trayCall("g_cancellable_cancel", cancel); close(finished) })
	b.connection = trayCall("g_dbus_connection_new_for_address_sync", p, 1|8, 0, cancel, uintptr(unsafe.Pointer(&nativeError)))
	if !timer.Stop() {
		<-finished
	}
	runtime.KeepAlive(addressBytes)
	if b.connection == 0 {
		b.mu.Lock()
		trayCall("g_object_unref", b.cancel)
		b.cancel = 0
		b.mu.Unlock()
		return trayNativeError("connect tray session bus", nativeError)
	}
	trayCall("g_dbus_connection_set_exit_on_close", b.connection, 0)
	b.uniqueName, _ = readBoundedString(trayCall("g_dbus_connection_get_unique_name", b.connection), 256)
	return nil
}

func (b *trayBroker) cleanup(ctx uintptr) {
	b.setAvailable(false)
	for _, id := range b.registrations {
		trayCall("g_dbus_connection_unregister_object", b.connection, uintptr(id))
	}
	if b.connection != 0 {
		if trayCall("g_dbus_connection_is_closed", b.connection) == 0 {
			trayCall("g_dbus_connection_close", b.connection, 0, 0, 0)
		}
		trayCall("g_object_unref", b.connection)
	}
	b.mu.Lock()
	if b.cancel != 0 {
		trayCall("g_object_unref", b.cancel)
		b.cancel = 0
	}
	b.mu.Unlock()
	for trayCall("g_main_context_pending", ctx) != 0 {
		trayCall("g_main_context_iteration", ctx, 0)
	}
}

// Outgoing synchronous queries live on a separate goroutine. The broker's
// context keeps serving property queries while the watcher registers this item.
// No shared GIO connection or automatic bus activation is used.
func (b *trayBroker) watch() {
	defer close(b.watchDone)
	registeredOwner := ""
	for {
		if b.stopping.Load() || trayCall("g_dbus_connection_is_closed", b.connection) != 0 {
			b.setAvailable(false)
			return
		}
		found := false
		for _, watcher := range []string{"org.kde.StatusNotifierWatcher", "org.freedesktop.StatusNotifierWatcher"} {
			owner := b.owner(watcher)
			if owner == "" {
				continue
			}
			found = true
			identity := watcher + owner
			if registeredOwner != identity {
				b.setAvailable(false)
				reply, err := b.call(owner, "/StatusNotifierWatcher", watcher, "RegisterStatusNotifierItem", trayTuple(trayString(trayPath)), "()")
				trayUnref(reply)
				if err != nil {
					registeredOwner = ""
					break
				}
				registeredOwner = identity
			}
			reply, err := b.call(owner, "/StatusNotifierWatcher", "org.freedesktop.DBus.Properties", "Get", trayTuple(trayString(watcher), trayString("IsStatusNotifierHostRegistered")), "(v)")
			live := false
			if err == nil {
				outer := trayChild(reply, 0)
				value := trayCall("g_variant_get_variant", outer)
				typ := trayType("b")
				if trayCall("g_variant_is_of_type", value, typ) != 0 {
					live = trayCall("g_variant_get_boolean", value) != 0
				}
				trayCall("g_variant_type_free", typ)
				trayUnref(value)
				trayUnref(outer)
				trayUnref(reply)
			}
			// Bind the proof to this exact watcher generation. A stale reply may
			// never resurrect recovery after the well-known owner was replaced.
			b.setAvailable(live && b.owner(watcher) == owner)
			break
		}
		if !found {
			registeredOwner = ""
			b.setAvailable(false)
		}
		select {
		case <-b.stop:
			return
		case <-time.After(300 * time.Millisecond):
		}
	}
}

func (b *trayBroker) owner(name string) string {
	r, err := b.call("org.freedesktop.DBus", "/org/freedesktop/DBus", "org.freedesktop.DBus", "GetNameOwner", trayTuple(trayString(name)), "(s)")
	if err != nil {
		return ""
	}
	defer trayUnref(r)
	v := trayChild(r, 0)
	defer trayUnref(v)
	s, ok := trayReadString(v, 255)
	if !ok {
		return ""
	}
	return s
}

// call takes a floating parameters tuple and returns an owned reply.
func (b *trayBroker) call(destination, path, iface, method string, parameters uintptr, resultType string) (uintptr, error) {
	trayCall("g_variant_ref_sink", parameters)
	defer trayUnref(parameters)
	d, dp := activationCString(destination)
	p, pp := activationCString(path)
	i, ip := activationCString(iface)
	m, mp := activationCString(method)
	typ := trayType(resultType)
	defer trayCall("g_variant_type_free", typ)
	var nativeError uintptr
	r := trayCall("g_dbus_connection_call_sync", b.connection, dp, pp, ip, mp, parameters, typ, 1, 750, b.cancel, uintptr(unsafe.Pointer(&nativeError)))
	runtime.KeepAlive(d)
	runtime.KeepAlive(p)
	runtime.KeepAlive(i)
	runtime.KeepAlive(m)
	if r == 0 {
		return 0, trayNativeError("tray bus request", nativeError)
	}
	return r, nil
}
func (b *trayBroker) emit(iface, signal string) {
	p, pp := activationCString(trayPath)
	i, ip := activationCString(iface)
	s, sp := activationCString(signal)
	trayCall("g_dbus_connection_emit_signal", b.connection, 0, pp, ip, sp, 0, 0)
	runtime.KeepAlive(p)
	runtime.KeepAlive(i)
	runtime.KeepAlive(s)
}
