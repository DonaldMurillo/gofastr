//go:build linux && (amd64 || arm64)

package linux

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/battery/desktop"
	"github.com/DonaldMurillo/gofastr/battery/desktop/internal/gtk"
)

const notificationInterface = "org.freedesktop.Notifications"
const notificationPath = "/org/freedesktop/Notifications"
const notificationTestXML = `<node><interface name="org.freedesktop.Notifications">
<method name="GetCapabilities"><arg type="as" direction="out"/></method>
<method name="GetServerInformation"><arg type="s" direction="out"/><arg type="s" direction="out"/><arg type="s" direction="out"/><arg type="s" direction="out"/></method>
<method name="Notify"><arg type="s" direction="in"/><arg type="u" direction="in"/><arg type="s" direction="in"/><arg type="s" direction="in"/><arg type="s" direction="in"/><arg type="as" direction="in"/><arg type="a{sv}" direction="in"/><arg type="i" direction="in"/><arg type="u" direction="out"/></method>
</interface></node>`

type notificationTestMessage struct {
	app, icon, title, body string
	replacement, actions   uintptr
	timeout                int32
}

type notificationTestDaemon struct {
	messages          chan notificationTestMessage
	reject            atomic.Bool
	markup            atomic.Bool
	capabilitiesError atomic.Bool
	pause             atomic.Pointer[notificationTestPause]
}

type notificationTestPause struct{ entered, release chan struct{} }

func (*notificationTestDaemon) property(_, _ string) uintptr { return 0 }
func (d *notificationTestDaemon) method(_, name string, args uintptr) (uintptr, error) {
	switch name {
	case "GetCapabilities":
		if d.capabilitiesError.Load() {
			return 0, errors.New("fixture capability query failure")
		}
		if d.markup.Load() {
			return trayTuple(trayArray("s", trayString("body"), trayString("body-markup"))), nil
		}
		return trayTuple(trayArray("s", trayString("body"))), nil
	case "GetServerInformation":
		return trayTuple(trayString("GoFastr test daemon"), trayString("GoFastr"), trayString("1"), trayString("1.2")), nil
	case "Notify":
		m := notificationTestMessage{}
		for index, target := range map[int]*string{0: &m.app, 2: &m.icon, 3: &m.title, 4: &m.body} {
			value, ok := trayArgString(args, index)
			if !ok {
				return 0, errors.New("invalid notification text")
			}
			*target = value
		}
		replacement, actions := trayChild(args, 1), trayChild(args, 5)
		defer trayUnref(replacement)
		defer trayUnref(actions)
		m.replacement = trayCall("g_variant_get_uint32", replacement)
		m.actions = trayCall("g_variant_n_children", actions)
		m.timeout = trayArgInt(args, 7)
		select {
		case d.messages <- m:
		default:
			return 0, errors.New("unexpected notification flood")
		}
		if pause := d.pause.Load(); pause != nil {
			close(pause.entered)
			select {
			case <-pause.release:
			case <-time.After(5 * time.Second):
				return 0, errors.New("fixture reply hold expired")
			}
		}
		if d.reject.Load() {
			return 0, errors.New("fixture refusal")
		}
		return trayTuple(trayCall("g_variant_new_uint32", 42)), nil
	}
	return 0, errors.New("unexpected notification method")
}

// Reuse the native GIO endpoint/connection machinery, but own a fresh connection
// and main context. No service is ever exported on the user's desktop bus.
func startNotificationTestDaemon() (*trayBroker, *notificationTestDaemon, error) {
	d := &notificationTestDaemon{messages: make(chan notificationTestMessage, 16)}
	d.markup.Store(true)
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
		reg, err := registerTrayEndpoint(b.connection, notificationPath, notificationTestXML, notificationInterface, d)
		if err != nil {
			b.ready <- err
			return
		}
		b.registrations = append(b.registrations, reg)
		reply, err := b.call("org.freedesktop.DBus", "/org/freedesktop/DBus", "org.freedesktop.DBus", "RequestName", trayTuple(trayString(notificationInterface), trayCall("g_variant_new_uint32", 4)), "(u)")
		if err != nil {
			b.ready <- err
			return
		}
		value := trayChild(reply, 0)
		owner := trayCall("g_variant_get_uint32", value)
		trayUnref(value)
		trayUnref(reply)
		if owner != 1 {
			b.ready <- errors.New("notification fixture name unavailable")
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
		b.close()
		return nil, nil, err
	}
	return b, d, nil
}

// This proves the native libnotify/DBus contract, not visible delivery. The
// separate graphical test runs an actual notification daemon and captures it.
func TestNativeNotifications(t *testing.T) {
	if os.Getenv("GOFASTR_LINUX_NATIVE") != "1" || os.Getenv("GOFASTR_LINUX_NOTIFICATIONS") != "1" {
		t.Skip("set GOFASTR_LINUX_NATIVE=1 and GOFASTR_LINUX_NOTIFICATIONS=1 in a graphical session")
	}
	if err := loadTrayGIO(); err != nil {
		t.Fatal(err)
	}
	startNotificationTestBus(t)
	baseline := trayLiveClosures.Load()
	broker, daemon, err := startNotificationTestDaemon()
	if err != nil {
		t.Fatal(err)
	}
	defer broker.close()
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	s := New().(*linuxShell)
	result := make(chan error, 1)
	err = runNotificationGTK(ctx, s, "GoFastr notification contract", func() {
		go func() { defer s.Quit(); result <- exerciseNativeNotifications(ctx, s, broker, daemon) }()
	})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-result:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("notification exercise did not finish", ctx.Err())
	}
	if live := trayLiveClosures.Load(); live != baseline {
		t.Fatalf("notification fixture retained native closures: %d, baseline %d", live, baseline)
	}
	// Ownership is released with the lifetime, so a second shell can run on the
	// same toolkit thread. Show must work again in that lifetime (#605).
	if owner := toolkitOwner.Load(); owner != nil {
		t.Fatalf("toolkit ownership retained after the lifetime ended: %p", owner)
	}
	// The exercise closed its daemons; the second lifetime gets its own.
	secondBroker, secondDaemon, err := startNotificationTestDaemon()
	if err != nil {
		t.Fatal(err)
	}
	defer secondBroker.close()
	again := New().(*linuxShell)
	err = runNotificationGTK(ctx, again, "GoFastr notification second lifetime", func() {
		go func() {
			defer again.Quit()
			result <- again.Show(ctx, desktop.Notification{Title: "Second lifetime", Body: "plain body"})
		}()
	})
	if err != nil {
		t.Fatalf("second shell lifetime: %v", err)
	}
	if err := <-result; err != nil {
		t.Fatalf("second lifetime Show: %v", err)
	}
	select {
	case got := <-secondDaemon.messages:
		if got.app != "GoFastr notification second lifetime" || got.title != "Second lifetime" {
			t.Fatalf("second lifetime notification: %#v", got)
		}
	case <-ctx.Done():
		t.Fatal("second lifetime notification never reached the daemon")
	}
	t.Log("native libnotify payload, refusal, daemon loss/restart, cleanup and a second GTK lifetime verified")
}

func exerciseNativeNotifications(ctx context.Context, s *linuxShell, broker *trayBroker, daemon *notificationTestDaemon) error {
	for _, n := range []desktop.Notification{
		{Title: `Title <b>π & "text"</b>`, Subtitle: `Sub <i>&</i>`, Body: "Body <b>literal</b> & \"quotes\"\nnext π"},
		{Title: "Second", Body: "plain body"},
	} {
		if err := s.Show(ctx, n); err != nil {
			return err
		}
		select {
		case got := <-daemon.messages:
			wantBody := "plain body"
			if n.Subtitle != "" {
				wantBody = "Sub &lt;i&gt;&amp;&lt;/i&gt;\nBody &lt;b&gt;literal&lt;/b&gt; &amp; &#34;quotes&#34;\nnext π"
			}
			if got.app != s.cfg.Title || got.title != n.Title || got.body != wantBody || got.icon != "" || got.replacement != 0 || got.actions != 0 || got.timeout != -1 {
				return fmt.Errorf("native notification payload: %#v; want title %q, body %q", got, n.Title, wantBody)
			}
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	daemon.markup.Store(false)
	plain := desktop.Notification{Title: "No body markup", Subtitle: "Sub <i>&</i>", Body: "Literal <b>markup</b> & π"}
	if err := s.Show(ctx, plain); err != nil {
		return err
	}
	if got := <-daemon.messages; got.body != plain.Subtitle+"\n"+plain.Body {
		return fmt.Errorf("plain daemon received escaped body: %q", got.body)
	}
	daemon.markup.Store(true)
	daemon.capabilitiesError.Store(true)
	if err := s.Show(ctx, desktop.Notification{Title: "Unknown capabilities", Body: "<b>literal</b>"}); err != nil {
		return err
	}
	if got := <-daemon.messages; got.body != "&lt;b&gt;literal&lt;/b&gt;" {
		return fmt.Errorf("unknown daemon received raw markup: %q", got.body)
	}
	daemon.capabilitiesError.Store(false)
	if err := exerciseNotificationStall(ctx, s, daemon); err != nil {
		return err
	}

	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if err := s.Show(cancelled, desktop.Notification{Title: "cancelled"}); !errors.Is(err, desktop.ErrCancelled) {
		return fmt.Errorf("pre-cancelled notification: %v", err)
	}
	if err := s.Show(ctx, desktop.Notification{Title: "invalid\x00title"}); err == nil {
		return errors.New("invalid notification accepted")
	}
	if len(daemon.messages) != 0 {
		return errors.New("cancelled/invalid notification reached the daemon")
	}

	for _, symbol := range []*uintptr{&gtk.Sym.NotifyNotificationShow, &gtk.Sym.NotifyGetServerCaps} {
		var unsupported error
		if err := s.Main(func() {
			saved := *symbol
			*symbol = 0
			defer func() { *symbol = saved }()
			unsupported = s.Show(ctx, desktop.Notification{Title: "no libnotify"})
		}); err != nil {
			return err
		}
		if !errors.Is(unsupported, desktop.ErrUnsupported) {
			return fmt.Errorf("optional libnotify: %v", unsupported)
		}
	}

	daemon.reject.Store(true)
	if err := s.Show(ctx, desktop.Notification{Title: "refused"}); err == nil || err.Error() != "desktop/linux: show notification" {
		return fmt.Errorf("daemon refusal: %v", err)
	}
	<-daemon.messages
	reply, err := broker.call("org.freedesktop.DBus", "/org/freedesktop/DBus", "org.freedesktop.DBus", "ReleaseName", trayTuple(trayString(notificationInterface)), "(u)")
	if err != nil {
		return err
	}
	trayUnref(reply)
	broker.close()
	if err := s.Show(ctx, desktop.Notification{Title: "no daemon"}); err == nil {
		return errors.New("notification succeeded without a daemon")
	}
	replacement, messages, err := startNotificationTestDaemon()
	if err != nil {
		return err
	}
	defer replacement.close()
	messages.markup.Store(false)
	if err := s.Show(ctx, desktop.Notification{Title: "daemon restarted", Body: "<b>plain restart</b>"}); err != nil {
		return fmt.Errorf("daemon restart: %w", err)
	}
	select {
	case got := <-messages.messages:
		if got.title != "daemon restarted" || got.body != "<b>plain restart</b>" {
			return errors.New("replacement daemon received wrong notification")
		}
	case <-ctx.Done():
		return ctx.Err()
	}
	retained := false
	capsOperations.Range(func(_, value any) bool { retained = retained || value.(*capsOperation).shell == s; return true })
	if retained {
		return errors.New("notification operation retained after settlement")
	}
	return nil
}

// Exercise Show on a real GTK loop without creating a WebKit view. This fixture
// takes the same toolkit ownership and toolkit thread as Run, and supplies only
// the shell state Show/Main/Quit need. It does not claim WebKit startup coverage.
func runNotificationGTK(ctx context.Context, s *linuxShell, title string, ready func()) error {
	if !toolkitOwner.CompareAndSwap(nil, s) {
		return errToolkitBusy
	}
	defer toolkitOwner.CompareAndSwap(s, nil)
	var err error
	if terr := onToolkitThread(func() { err = runNotificationLoop(ctx, s, title, ready) }); terr != nil {
		return terr
	}
	return err
}

func runNotificationLoop(ctx context.Context, s *linuxShell, title string, ready func()) error {
	if err := gtk.Load(); err != nil {
		return err
	}
	if call(gtk.Sym.GtkInitCheck, 0, 0) == 0 {
		return errors.New("notification fixture cannot open display")
	}
	s.cfg.Title = title
	s.uiThread.Store(int64(syscall.Gettid()))
	defer s.uiThread.Store(0)
	s.running.Store(true)
	defer func() { s.closeCapabilities(); s.running.Store(false); cancelShellJobs(s); close(s.done) }()
	go func() {
		select {
		case <-ctx.Done():
			s.Quit()
		case <-s.done:
		}
	}()
	ready()
	if !s.quitting.Load() {
		s.inLoop = true
		call(gtk.Sym.GtkMain)
		s.inLoop = false
	}
	return nil
}

// No servicedir/session.conf includes: installing dunst on the worker must not
// turn the missing-daemon case into implicit service activation.
func startNotificationTestBus(t *testing.T) {
	t.Helper()
	root := t.TempDir()
	config := filepath.Join(root, "bus.conf")
	if err := os.WriteFile(config, []byte(`<busconfig><type>session</type>
<listen>unix:tmpdir=/tmp</listen><auth>EXTERNAL</auth>
<policy context="default"><allow user="*"/><allow own="*"/>
<allow send_destination="*"/><allow receive_sender="*"/></policy>
</busconfig>`), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Second)
	t.Cleanup(cancel)
	cmd := exec.CommandContext(ctx, "dbus-daemon", "--config-file="+config, "--nofork", "--print-address=1", "--nopidfile")
	cmd.WaitDelay = time.Second
	log, err := os.CreateTemp(root, "bus-*.log")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = log.Close() })
	cmd.Stderr = log
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
	address, err := bufio.NewReader(stdout).ReadString('\n')
	if err != nil {
		t.Fatalf("notification test bus address: %v", err)
	}
	t.Setenv("DBUS_SESSION_BUS_ADDRESS", strings.TrimSpace(address))
	t.Setenv("NO_AT_BRIDGE", "1")
}

// Native libnotify calls are synchronous. Cancellation releases the caller but
// cannot preempt the GTK thread or retract an already-sent Notify request.
// Bound the fixture itself so a regression cannot hold the worker forever.
func exerciseNotificationStall(ctx context.Context, s *linuxShell, daemon *notificationTestDaemon) error {
	pause := &notificationTestPause{entered: make(chan struct{}), release: make(chan struct{})}
	daemon.pause.Store(pause)
	defer daemon.pause.Store(nil)
	var released atomic.Bool
	release := func() {
		if released.CompareAndSwap(false, true) {
			close(pause.release)
		}
	}
	defer release()
	requestCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	result := make(chan error, 1)
	go func() { result <- s.Show(requestCtx, desktop.Notification{Title: "Bounded stalled daemon"}) }()
	select {
	case <-pause.entered:
	case <-ctx.Done():
		return ctx.Err()
	}
	<-daemon.messages
	cancel()
	select {
	case err := <-result:
		if !errors.Is(err, desktop.ErrCancelled) {
			return fmt.Errorf("stalled notification cancellation: %v", err)
		}
	case <-time.After(time.Second):
		return errors.New("caller remained blocked after cancellation")
	}
	heartbeat := make(chan error, 1)
	go func() { heartbeat <- s.Main(func() {}) }()
	// Observe a real queued dispatch before making the negative heartbeat
	// assertion. An unscheduled goroutine would otherwise be a false witness.
	deadline := time.Now().Add(time.Second)
	for {
		queued := false
		mainJobs.Range(func(_, value any) bool {
			job := value.(*mainJob)
			queued = queued || job.shell == s && job.state.Load() == 0
			return true
		})
		if queued {
			break
		}
		select {
		case err := <-heartbeat:
			return fmt.Errorf("GTK heartbeat completed before native reply: %v", err)
		default:
		}
		if time.Now().After(deadline) {
			return errors.New("heartbeat was not queued during native wait")
		}
		time.Sleep(time.Millisecond)
	}
	select {
	case err := <-heartbeat:
		return fmt.Errorf("expected native GTK wait before daemon reply, heartbeat returned %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	release()
	select {
	case err := <-heartbeat:
		if err != nil {
			return err
		}
	case <-time.After(time.Second):
		return errors.New("GTK did not recover after bounded daemon reply")
	}
	return nil
}
