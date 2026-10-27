//go:build linux && (amd64 || arm64)

package linux

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"
	"unsafe"

	"github.com/DonaldMurillo/gofastr/battery/desktop/internal/ffi"
	"github.com/DonaldMurillo/gofastr/battery/desktop/internal/gtk"
	"github.com/DonaldMurillo/gofastr/core/textsafe"
)

const (
	activationURLLimit   = 2048
	activationQueueLimit = 16
	activationTimeout    = 10 * time.Second
	activationInterface  = "dev.gofastr.Desktop.Activation1"
	activationPath       = "/dev/gofastr/Desktop/Activation"
)

// PrepareLaunch implements the battery's embedded-scheme launch seam. It does
// not install a protocol handler or touch desktop preferences. The broker uses
// GIO only, so a secondary process never initializes GTK/WebKit or app.Start.
func (s *linuxShell) PrepareLaunch(appID, _ string, scheme string) (bool, []string, error) {
	if scheme == "" {
		return false, nil, nil
	}
	if !validActivationScheme(scheme) || appID == "" || len(appID) > 253 {
		return false, nil, errors.New("desktop/linux: invalid activation identity or scheme")
	}
	s.mu.Lock()
	if s.activation != nil || s.started || s.quitting.Load() {
		s.mu.Unlock()
		return false, nil, errors.New("desktop/linux: launch already prepared or stopped")
	}
	b := newActivationBroker(appID, scheme)
	s.activation = b
	s.mu.Unlock()
	result := b.start(os.Args[1:])
	if result.err != nil || result.handled {
		s.ReleaseLaunch()
	}
	return result.handled, result.urls, result.err
}

// ReleaseLaunch is also safe before Run, and is deferred by Battery.Run after
// primary preparation. No UI thread is needed to release the bus name.
func (s *linuxShell) ReleaseLaunch() {
	s.mu.Lock()
	b := s.activation
	s.activation = nil
	s.mu.Unlock()
	if b != nil {
		b.close()
	}
}

func (s *linuxShell) attachActivation(onURL func(string), focus func()) {
	s.mu.RLock()
	b := s.activation
	s.mu.RUnlock()
	if b != nil {
		b.delivery.attach(onURL, focus)
	}
}

func validActivationScheme(s string) bool {
	if len(s) < 1 || len(s) > 32 || s[0] < 'a' || s[0] > 'z' {
		return false
	}
	for _, c := range s {
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '+' || c == '.' || c == '-') {
			return false
		}
	}
	return true
}

func validActivationURL(raw, scheme string) bool {
	if len(raw) == 0 || len(raw) > activationURLLimit || !utf8.ValidString(raw) || strings.IndexByte(raw, 0) >= 0 {
		return false
	}
	u, err := url.Parse(raw)
	return err == nil && strings.EqualFold(u.Scheme, scheme)
}

func activationURLs(args []string, scheme string) []string {
	var urls []string
	for _, arg := range args {
		if validActivationURL(arg, scheme) {
			if len(urls) == activationQueueLimit {
				copy(urls, urls[1:])
				urls[len(urls)-1] = arg
			} else {
				urls = append(urls, arg)
			}
		}
	}
	return urls
}

func activationIdentity(appID string) string {
	// Prefix the digest element with a letter. Existing valid GoFastr IDs need
	// not be valid D-Bus names (e.g. 1.example), so do not tighten that API.
	sum := sha256.Sum256([]byte(appID))
	return fmt.Sprintf("dev.gofastr.Desktop.i%x", sum[:])
}

type activationResult struct {
	handled bool
	urls    []string
	err     error
}

// A single delivery worker preserves order, bounds pending work even while the
// app callback stalls, and keeps user code off the native broker thread.
// Empty strings are focus-only activation; they never reach OnDeepLink.
type activationDelivery struct {
	mu                         sync.Mutex
	pending                    []string
	onURL                      func(string)
	focus                      func()
	attached, draining, closed bool
}

func (d *activationDelivery) enqueue(raw string) bool {
	d.mu.Lock()
	if d.closed {
		d.mu.Unlock()
		return false
	}
	if len(d.pending) == activationQueueLimit {
		copy(d.pending, d.pending[1:])
		d.pending[len(d.pending)-1] = raw
	} else {
		d.pending = append(d.pending, raw)
	}
	start := d.attached && !d.draining
	if start {
		d.draining = true
	}
	d.mu.Unlock()
	if start {
		go d.drain()
	}
	return true
}
func (d *activationDelivery) attach(onURL func(string), focus func()) {
	d.mu.Lock()
	if d.closed {
		d.mu.Unlock()
		return
	}
	d.onURL = onURL
	d.focus = focus
	d.attached = true
	start := len(d.pending) > 0 && !d.draining
	if start {
		d.draining = true
	}
	d.mu.Unlock()
	if start {
		go d.drain()
	}
}
func (d *activationDelivery) drain() {
	for {
		d.mu.Lock()
		if d.closed || len(d.pending) == 0 {
			d.draining = false
			d.mu.Unlock()
			return
		}
		raw := d.pending[0]
		d.pending[0] = ""
		d.pending = d.pending[1:]
		onURL, focus := d.onURL, d.focus
		d.mu.Unlock()
		deliverActivation(raw, onURL, focus)
	}
}

// A host callback can panic; it must not tear down the broker or process.
func deliverActivation(raw string, onURL func(string), focus func()) {
	defer func() {
		if v := recover(); v != nil {
			slog.Default().Error("desktop/linux: activation callback panicked", "panic", textsafe.Recovered(v))
		}
	}()
	if raw == "" {
		if focus != nil {
			focus()
		}
	} else if onURL != nil {
		onURL(raw)
	}
}

func (d *activationDelivery) close() {
	d.mu.Lock()
	d.closed = true
	d.pending = nil
	d.onURL = nil
	d.focus = nil
	d.mu.Unlock()
}

type activationBroker struct {
	id                                     uintptr
	name, scheme                           string
	ready                                  chan activationResult
	done                                   chan struct{}
	once                                   sync.Once
	delivery                               activationDelivery
	nativeMu                               sync.Mutex
	nativeContext, nativeLoop, cancellable uintptr
	closed                                 bool
	connection, registration, closedSignal uintptr // broker thread only
	ownsName                               bool
}

var activationSequence atomic.Uintptr
var activationLiveClosures atomic.Int64
var activationBrokers sync.Map // integer tokens, never Go pointers retained by C

func newActivationBroker(appID, scheme string) *activationBroker {
	return &activationBroker{id: activationSequence.Add(1), name: activationIdentity(appID), scheme: scheme, ready: make(chan activationResult, 1), done: make(chan struct{})}
}
func (b *activationBroker) start(args []string) activationResult {
	if err := loadActivationGIO(); err != nil {
		close(b.done)
		return activationResult{err: err}
	}
	urls := activationURLs(args, b.scheme)
	go b.run(urls)
	return <-b.ready
}
func (b *activationBroker) run(urls []string) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	defer close(b.done)
	defer b.delivery.close()
	activationBrokers.Store(b.id, b)
	defer activationBrokers.Delete(b.id)
	context := activationCall("g_main_context_new")
	loop := activationCall("g_main_loop_new", context, 0)
	activationCall("g_main_context_push_thread_default", context)
	defer activationCall("g_main_context_unref", context)
	defer activationCall("g_main_context_pop_thread_default", context)
	defer activationCall("g_main_loop_unref", loop)
	b.nativeMu.Lock()
	b.nativeContext = context
	b.nativeLoop = loop
	closed := b.closed
	b.nativeMu.Unlock()
	result := activationResult{err: errors.New("desktop/linux: activation preparation cancelled")}
	if !closed {
		result = b.prepare(urls)
	}
	if result.err == nil && !result.handled {
		b.ready <- result
		activationCall("g_main_loop_run", loop)
		b.cleanup()
	} else {
		b.cleanup()
		b.ready <- result
	}
	b.nativeMu.Lock()
	b.nativeContext = 0
	b.nativeLoop = 0
	b.nativeMu.Unlock()
}

// Requiring an existing local bus avoids GIO's autolaunch fallback and remote
// transports. No session bus is created and no desktop preferences are changed.
func validActivationAddress(address string) bool {
	if address == "" || len(address) > 4096 || !utf8.ValidString(address) || strings.IndexByte(address, 0) >= 0 {
		return false
	}
	for _, entry := range strings.Split(address, ";") {
		options, ok := strings.CutPrefix(entry, "unix:")
		if !ok {
			return false
		}
		seen := make(map[string]bool, 3)
		for _, option := range strings.Split(options, ",") {
			key, value, ok := strings.Cut(option, "=")
			key, keyErr := url.PathUnescape(key)
			value, valueErr := url.PathUnescape(value) // '+' stays literal, unlike query decoding
			if !ok || keyErr != nil || valueErr != nil || seen[key] || value == "" || strings.IndexByte(value, 0) >= 0 {
				return false
			}
			seen[key] = true
			switch key {
			case "path":
				if !strings.HasPrefix(value, "/") {
					return false
				}
			case "abstract":
			case "guid":
				if len(value) != 32 || strings.ContainsFunc(value, func(c rune) bool { return !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F') }) {
					return false
				}
			default:
				return false // No server-only dir/tmpdir or unknown options.
			}
		}
		if seen["path"] == seen["abstract"] {
			return false
		}
	}
	return true
}

var errActivationAddress = errors.New("desktop/linux: URL activation requires DBUS_SESSION_BUS_ADDRESS for an existing local unix:path=... or unix:abstract=... bus; launch from a supported desktop session (autolaunch/TCP are not supported)")

func (b *activationBroker) prepare(urls []string) activationResult {
	address := os.Getenv("DBUS_SESSION_BUS_ADDRESS")
	if !validActivationAddress(address) {
		return activationResult{err: errActivationAddress}
	}
	var addressError uintptr
	valid := activationStringCall("g_dbus_is_supported_address", address, uintptr(unsafe.Pointer(&addressError)))
	runtime.KeepAlive(&addressError)
	if addressError != 0 {
		activationCall("g_error_free", addressError) // Can include raw address; do not expose it.
	}
	if valid == 0 {
		return activationResult{err: errActivationAddress}
	}
	cancel := activationCall("g_cancellable_new")
	b.nativeMu.Lock()
	b.cancellable = cancel
	if b.closed {
		activationCall("g_cancellable_cancel", cancel)
	}
	b.nativeMu.Unlock()
	timer := time.AfterFunc(activationTimeout, b.cancel)
	defer func() {
		timer.Stop()
		b.nativeMu.Lock()
		b.cancellable = 0
		activationCall("g_object_unref", cancel)
		b.nativeMu.Unlock()
	}()
	var nativeError uintptr
	// This is an app-owned connection, never GIO's shared g_bus_get connection.
	// AUTHENTICATION_CLIENT|MESSAGE_BUS_CONNECTION authenticates and sends Hello.
	b.connection = activationStringCall("g_dbus_connection_new_for_address_sync", address, 1|8, 0, cancel, uintptr(unsafe.Pointer(&nativeError)))
	runtime.KeepAlive(&nativeError)
	if b.connection == 0 {
		return activationResult{err: activationError("connect activation bus", nativeError)}
	}
	activationCall("g_dbus_connection_set_exit_on_close", b.connection, 0)
	b.closedSignal = activationConnect(b.connection, "closed", activationClosedCallback, b.id)
	if err := b.register(); err != nil {
		return activationResult{err: err}
	}
	// Export before election, so the winning name is never visible without its
	// receiver. DO_NOT_QUEUE forbids silently becoming primary at a later time.
	parameters := activationTuple(activationStringCall("g_variant_new_string", b.name), activationCall("g_variant_new_uint32", 4))
	defer activationCall("g_variant_unref", parameters)
	resultType := activationStringCall("g_variant_type_new", "(u)")
	defer activationCall("g_variant_type_free", resultType)
	reply, err := b.call("org.freedesktop.DBus", "/org/freedesktop/DBus", "org.freedesktop.DBus", "RequestName", parameters, resultType, cancel)
	if err != nil {
		return activationResult{err: err}
	}
	child := activationCall("g_variant_get_child_value", reply, 0)
	owner := activationCall("g_variant_get_uint32", child)
	activationCall("g_variant_unref", child)
	activationCall("g_variant_unref", reply)
	if owner == 1 { // DBUS_REQUEST_NAME_REPLY_PRIMARY_OWNER
		b.ownsName = true
		if activationCall("g_dbus_connection_is_closed", b.connection) != 0 {
			return activationResult{err: errors.New("desktop/linux: activation bus disconnected during preparation")}
		}
		return activationResult{urls: urls}
	}
	if owner != 3 { // DBUS_REQUEST_NAME_REPLY_EXISTS
		return activationResult{err: errors.New("desktop/linux: unexpected activation name election result")}
	}
	if len(urls) == 0 {
		if err := b.forward("", cancel); err != nil {
			return activationResult{err: err}
		}
	} else {
		for _, raw := range urls {
			if err := b.forward(raw, cancel); err != nil {
				return activationResult{err: err}
			}
		}
	}
	return activationResult{handled: true}
}

func (b *activationBroker) register() error {
	const xml = `<node><interface name="` + activationInterface + `"><method name="Activate"><arg type="s" name="uri" direction="in"/></method></interface></node>`
	var nativeError uintptr
	node := activationStringCall("g_dbus_node_info_new_for_xml", xml, uintptr(unsafe.Pointer(&nativeError)))
	runtime.KeepAlive(&nativeError)
	if node == 0 {
		return activationError("parse activation interface", nativeError)
	}
	defer activationCall("g_dbus_node_info_unref", node)
	ifaceBytes, ifacePointer := activationCString(activationInterface)
	iface := activationCall("g_dbus_node_info_lookup_interface", node, ifacePointer)
	runtime.KeepAlive(ifaceBytes)
	path, pathPointer := activationCString(activationPath)
	// The amd64 callback trampoline captures only six integer registers.
	// A GClosure meta-marshaller has six arguments, whereas GDBus's direct
	// vtable handler has eight. Native GValues carry the remaining arguments.
	closure := activationCall("g_cclosure_new", activationMethodCallback, 0, 0)
	activationLiveClosures.Add(1)
	activationCall("g_closure_add_finalize_notifier", closure, b.id, activationClosureFinalizedCallback)
	activationCall("g_closure_ref", closure)
	activationCall("g_closure_sink", closure)
	defer activationCall("g_closure_unref", closure)
	activationCall("g_closure_set_meta_marshal", closure, b.id, activationMethodCallback)
	// Keep the pre-2.84 API for older supported desktops. Its method invocation
	// reference is transferred to us and consumed exactly once by return_* below.
	// GIO refs the native closure/interface; no Go pointer is retained.
	b.registration = activationCall("g_dbus_connection_register_object_with_closures", b.connection, pathPointer, iface, closure, 0, 0, uintptr(unsafe.Pointer(&nativeError)))
	runtime.KeepAlive(path)
	runtime.KeepAlive(&nativeError)
	if b.registration == 0 {
		return activationError("register activation receiver", nativeError)
	}
	return nil
}
func (b *activationBroker) cancel() {
	b.nativeMu.Lock()
	defer b.nativeMu.Unlock()
	if b.cancellable != 0 {
		activationCall("g_cancellable_cancel", b.cancellable)
	}
}
func (b *activationBroker) close() {
	b.once.Do(func() {
		b.delivery.close()
		b.nativeMu.Lock()
		b.closed = true
		if b.cancellable != 0 {
			activationCall("g_cancellable_cancel", b.cancellable)
		}
		if b.nativeContext != 0 {
			// An explicitly attached source cannot run synchronously before loop.run,
			// unlike g_main_context_invoke(). This closes the prepare/run race.
			source := activationCall("g_idle_source_new")
			activationCall("g_source_set_priority", source, uintptr(uint32(0xffffff9c))) // G_PRIORITY_HIGH=-100
			activationCall("g_source_set_callback", source, activationStopCallback, b.id, 0)
			activationCall("g_source_attach", source, b.nativeContext)
			activationCall("g_source_unref", source)
		}
		b.nativeMu.Unlock()
		<-b.done
	})
}
func (b *activationBroker) cleanup() {
	b.delivery.close()
	if b.connection == 0 {
		return
	}
	if b.registration != 0 {
		activationCall("g_dbus_connection_unregister_object", b.connection, b.registration)
		b.registration = 0
	}
	if b.closedSignal != 0 {
		activationCall("g_signal_handler_disconnect", b.connection, b.closedSignal)
		b.closedSignal = 0
	}
	cancel, finish := activationDeadline()
	defer finish()
	if b.ownsName && activationCall("g_dbus_connection_is_closed", b.connection) == 0 {
		// A synchronous release acknowledgement makes immediate reacquisition
		// independent of when the bus notices our socket closing.
		parameters := activationTuple(activationStringCall("g_variant_new_string", b.name))
		resultType := activationStringCall("g_variant_type_new", "(u)")
		reply, err := b.call("org.freedesktop.DBus", "/org/freedesktop/DBus", "org.freedesktop.DBus", "ReleaseName", parameters, resultType, cancel)
		activationCall("g_variant_unref", parameters)
		activationCall("g_variant_type_free", resultType)
		if reply != 0 {
			activationCall("g_variant_unref", reply)
		}
		if err != nil {
			slog.Default().Warn("desktop/linux: activation name release failed", "error", err)
		}
	}
	b.ownsName = false
	if activationCall("g_dbus_connection_is_closed", b.connection) == 0 {
		var nativeError uintptr
		if activationCall("g_dbus_connection_close_sync", b.connection, cancel, uintptr(unsafe.Pointer(&nativeError))) == 0 {
			slog.Default().Warn("desktop/linux: activation connection close failed", "error", activationError("close activation bus", nativeError))
			// A cancelled synchronous wait must not leave this owned connection
			// open. No callback or application pointer survives the async close.
			activationCall("g_dbus_connection_close", b.connection, 0, 0, 0)
		}
		runtime.KeepAlive(&nativeError)
	}
	activationCall("g_object_unref", b.connection)
	b.connection = 0
	// GIO schedules registration user_data_free_func on the context that
	// registered it. Merely unrefing a context with pending sources would leak
	// its closure/data. The connection is now closed and no new calls can arrive;
	// drain this broker-only context before destroying it.
	context := activationCall("g_main_context_get_thread_default")
	for context != 0 && activationCall("g_main_context_pending", context) != 0 {
		activationCall("g_main_context_iteration", context, 0)
	}
}

var activationClosureFinalizedCallback = ffi.NewCallbackAnyThread(func(a *ffi.Args) uintptr {
	activationLiveClosures.Add(-1)
	return 0
})

// Stop a firing timer before freeing the native cancellation argument it uses.
func activationDeadline() (uintptr, func()) {
	cancel := activationCall("g_cancellable_new")
	fired := make(chan struct{})
	timer := time.AfterFunc(activationTimeout, func() {
		activationCall("g_cancellable_cancel", cancel)
		close(fired)
	})
	return cancel, func() {
		if !timer.Stop() {
			<-fired
		}
		activationCall("g_object_unref", cancel)
	}
}

var activationClosedCallback = ffi.NewCallbackAnyThread(func(a *ffi.Args) uintptr {
	if v, ok := activationBrokers.Load(a.Int[3]); ok {
		b := v.(*activationBroker)
		b.delivery.close()
		slog.Default().Warn("desktop/linux: activation bus disconnected; URL activation disabled until restart")
		activationCall("g_main_loop_quit", b.nativeLoop)
	}
	return 0
})

// GValue is GType plus two eight-byte union slots on both supported LP64 ABIs.
// Use native getters for its contents; only the public C array stride is needed.
const activationGValueSize = 24

var activationMethodCallback = ffi.NewCallbackAnyThread(func(a *ffi.Args) uintptr {
	if a.Int[2] != 7 || a.Int[3] == 0 {
		slog.Default().Error("desktop/linux: unexpected activation closure arguments")
		return 0
	}
	parameters := activationCall("g_value_get_variant", a.Int[3]+5*activationGValueSize)
	invocation := activationCall("g_value_get_object", a.Int[3]+6*activationGValueSize)
	v, ok := activationBrokers.Load(a.Int[5])
	if !ok {
		activationMethodError(invocation, "org.freedesktop.DBus.Error.Disconnected", "Activation receiver is closed")
		return 0
	}
	b := v.(*activationBroker)
	// GIO enforces our fixed (s) signature and method before dispatch. Read the
	// native length BEFORE copying, including when another process bypasses our
	// sender-side guard. Invalid UTF-8 cannot be represented by a D-Bus string.
	value := activationCall("g_variant_get_child_value", parameters, 0)
	defer activationCall("g_variant_unref", value)
	var length uintptr
	p := activationCall("g_variant_get_string", value, uintptr(unsafe.Pointer(&length)))
	runtime.KeepAlive(&length)
	if p == 0 || length > activationURLLimit {
		activationMethodError(invocation, "org.freedesktop.DBus.Error.InvalidArgs", "Invalid activation URI")
		return 0
	}
	raw := string(unsafe.Slice((*byte)(nativePointer(p)), int(length)))
	if raw != "" && !validActivationURL(raw, b.scheme) {
		activationMethodError(invocation, "org.freedesktop.DBus.Error.InvalidArgs", "Invalid activation URI")
		return 0
	}
	if !b.delivery.enqueue(raw) {
		activationMethodError(invocation, "org.freedesktop.DBus.Error.Disconnected", "Activation receiver is closed")
		return 0
	}
	// Acknowledges bounded queue acceptance, not application navigation. Existing
	// portable URL validation and session readiness still run after delivery.
	activationCall("g_dbus_method_invocation_return_value", invocation, 0)
	return 0
})

func activationMethodError(invocation uintptr, name, message string) {
	nameBytes, np := activationCString(name)
	messageBytes, mp := activationCString(message)
	activationCall("g_dbus_method_invocation_return_dbus_error", invocation, np, mp)
	runtime.KeepAlive(nameBytes)
	runtime.KeepAlive(messageBytes)
}

var activationStopCallback = ffi.NewCallbackAnyThread(func(a *ffi.Args) uintptr {
	if v, ok := activationBrokers.Load(a.Int[0]); ok {
		b := v.(*activationBroker)
		activationCall("g_main_loop_quit", b.nativeLoop)
	}
	return 0
})

func (b *activationBroker) forward(raw string, cancel uintptr) error {
	parameters := activationTuple(activationStringCall("g_variant_new_string", raw))
	defer activationCall("g_variant_unref", parameters)
	resultType := activationStringCall("g_variant_type_new", "()")
	defer activationCall("g_variant_type_free", resultType)
	reply, err := b.call(b.name, activationPath, activationInterface, "Activate", parameters, resultType, cancel)
	if reply != 0 {
		activationCall("g_variant_unref", reply)
	}
	return err
}

// The caller retains parameters/resultType and owns the returned reply.
func (b *activationBroker) call(destination, objectPath, interfaceName, method string, parameters, resultType, cancel uintptr) (uintptr, error) {
	name, np := activationCString(destination)
	path, pp := activationCString(objectPath)
	iface, ip := activationCString(interfaceName)
	meth, mp := activationCString(method)
	var nativeError uintptr
	reply := activationCall("g_dbus_connection_call_sync", b.connection, np, pp, ip, mp, parameters, resultType, 1 /* NO_AUTO_START */, 5000, cancel, uintptr(unsafe.Pointer(&nativeError)))
	runtime.KeepAlive(name)
	runtime.KeepAlive(path)
	runtime.KeepAlive(iface)
	runtime.KeepAlive(meth)
	runtime.KeepAlive(&nativeError)
	if reply == 0 {
		return 0, activationError("activation "+method, nativeError)
	}
	return reply, nil
}
func activationTuple(children ...uintptr) uintptr {
	var tuple uintptr
	if len(children) == 0 {
		tuple = activationCall("g_variant_new_tuple", 0, 0)
	} else {
		// Keep the conversion in the annotated call expression: a uintptr local
		// would hide this Go backing array from the compiler's escape analysis.
		tuple = activationCall("g_variant_new_tuple", uintptr(unsafe.Pointer(&children[0])), uintptr(len(children)))
	}
	runtime.KeepAlive(children)
	return activationCall("g_variant_ref_sink", tuple)
}

var activationGIO struct {
	sync.Once
	symbols map[string]uintptr
	err     error
}

func loadActivationGIO() error {
	activationGIO.Do(func() {
		activationGIO.symbols = make(map[string]uintptr)
		for library, names := range map[string][]string{
			"libglib-2.0.so.0":    {"g_main_context_new", "g_main_context_unref", "g_main_context_get_thread_default", "g_main_context_pending", "g_main_context_iteration", "g_main_context_push_thread_default", "g_main_context_pop_thread_default", "g_main_loop_new", "g_main_loop_run", "g_main_loop_quit", "g_main_loop_unref", "g_idle_source_new", "g_source_set_priority", "g_source_set_callback", "g_source_attach", "g_source_unref", "g_error_free", "g_free", "g_variant_type_new", "g_variant_type_free", "g_variant_new_string", "g_variant_new_uint32", "g_variant_new_tuple", "g_variant_ref_sink", "g_variant_unref", "g_variant_get_string", "g_variant_get_uint32", "g_variant_get_child_value"},
			"libgobject-2.0.so.0": {"g_object_unref", "g_signal_connect_data", "g_signal_handler_disconnect", "g_cclosure_new", "g_closure_ref", "g_closure_sink", "g_closure_unref", "g_closure_set_meta_marshal", "g_closure_add_finalize_notifier", "g_value_get_variant", "g_value_get_object"},
			"libgio-2.0.so.0":     {"g_cancellable_new", "g_cancellable_cancel", "g_dbus_is_supported_address", "g_dbus_error_get_remote_error", "g_dbus_connection_new_for_address_sync", "g_dbus_connection_set_exit_on_close", "g_dbus_connection_is_closed", "g_dbus_connection_register_object_with_closures", "g_dbus_connection_unregister_object", "g_dbus_connection_call_sync", "g_dbus_connection_close_sync", "g_dbus_connection_close", "g_dbus_node_info_new_for_xml", "g_dbus_node_info_lookup_interface", "g_dbus_node_info_unref", "g_dbus_method_invocation_return_value", "g_dbus_method_invocation_return_dbus_error"},
		} {
			handle, err := gtk.DlopenGlobal(library)
			if err != nil {
				activationGIO.err = err
				return
			}
			for _, name := range names {
				p, e := gtk.Dlsym(handle, name)
				if e != nil {
					activationGIO.err = e
					return
				}
				activationGIO.symbols[name] = p
			}
		}
	})
	return activationGIO.err
}

// Returning the buffer from a non-inlined allocator forces heap storage. These
// addresses are passed synchronously to C; KeepAlive alone would not prevent a
// callback from relocating an inlined buffer on a growing Go stack.
//
//go:noinline
func activationCString(s string) ([]byte, uintptr) {
	b := append([]byte(s), 0)
	return b, uintptr(unsafe.Pointer(&b[0]))
}

// Match activationCall's contract through this wrapper too: callers can pass
// Go out-parameters in args, and those must escape before any native callback.
//
//go:uintptrescapes
func activationStringCall(name, value string, args ...uintptr) uintptr {
	b, p := activationCString(value)
	result := activationCall(name, append([]uintptr{p}, args...)...)
	runtime.KeepAlive(b)
	return result
}
func activationConnect(object uintptr, signal string, callback, token uintptr) uintptr {
	b, p := activationCString(signal)
	id := activationCall("g_signal_connect_data", object, p, callback, token, 0, 0)
	runtime.KeepAlive(b)
	return id
}

//go:uintptrescapes
func activationCall(name string, args ...uintptr) uintptr {
	r, _, _ := ffi.Call(activationGIO.symbols[name], args, nil)
	return r
}
func activationError(operation string, p uintptr) error {
	if p == 0 {
		return fmt.Errorf("desktop/linux: %s failed", operation)
	}
	defer activationCall("g_error_free", p)
	// Native errors can echo a bus address or activation payload. Preserve only
	// a small fixed protocol classification, never peer-supplied message text.
	remote := activationCall("g_dbus_error_get_remote_error", p)
	if remote != 0 {
		name := readString(remote)
		activationCall("g_free", remote)
		switch name {
		case "org.freedesktop.DBus.Error.InvalidArgs", "org.freedesktop.DBus.Error.UnknownMethod", "org.freedesktop.DBus.Error.Disconnected":
			return fmt.Errorf("desktop/linux: %s: %s", operation, name)
		}
	}
	return fmt.Errorf("desktop/linux: %s failed (native error code %d); check the existing local session bus", operation, (*gError)(nativePointer(p)).code)
}
