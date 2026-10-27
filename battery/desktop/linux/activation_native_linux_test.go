//go:build linux && (amd64 || arm64)

package linux

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/battery/desktop"
	"github.com/DonaldMurillo/gofastr/framework"
)

// Run this test binary once inside dbus-run-session. All child processes inherit
// that SAME private bus. It needs GIO, not a display, GTK initialization or WebKit.
// Compile with -ldflags=-X=github.com/DonaldMurillo/gofastr/battery/desktop.builtDeepLinkScheme=gofastr605test
// so the startup-failure integration exercises the real embedded-scheme gate.
func TestNativeActivation(t *testing.T) {
	if os.Getenv("GOFASTR_LINUX_ACTIVATION") != "1" {
		t.Skip("set GOFASTR_LINUX_ACTIVATION=1 inside one private dbus-run-session")
	}
	if os.Getenv("DBUS_SESSION_BUS_ADDRESS") == "" {
		t.Fatal("test requires an explicitly supplied private session bus")
	}
	id := fmt.Sprintf("dev.gofastr.test.%d.%d", os.Getpid(), time.Now().UnixNano())
	p := startActivationChild(t, id, "primary", "gofastr605test://cold/path?q=%2f#raw")
	if line := p.line(t); line != "PRIMARY [\"gofastr605test://cold/path?q=%2f#raw\"]" {
		t.Fatal(line)
	}
	runActivationChild(t, id, "invalid")
	runActivationChild(t, id, "invalid-method")
	p.send(t, "count")
	if line := p.line(t); line != "COUNT 0" {
		t.Fatal(line)
	}
	runActivationChild(t, id, "flood")
	p.send(t, "count")
	if line := p.line(t); line != "COUNT 16" {
		t.Fatal(line)
	}
	p.send(t, "attach")
	for i := 4; i < 20; i++ {
		want := fmt.Sprintf("URL %q", fmt.Sprintf("gofastr605test://warm/%d?q=%%2f#raw", i))
		if line := p.line(t); line != want {
			t.Fatalf("got %s want %s", line, want)
		}
	}
	other := startActivationChild(t, id+".other", "primary")
	if line := other.line(t); line != "PRIMARY []" {
		t.Fatal(line)
	}
	other.send(t, "attach")
	runActivationChild(t, id+".other", "secondary")
	if line := other.line(t); line != "FOCUS" {
		t.Fatal(line)
	}
	other.send(t, "release")
	if line := other.line(t); line != "RELEASED" {
		t.Fatal(line)
	}
	other.send(t, "exit")
	other.wait(t)
	runActivationChild(t, id, "secondary")
	if line := p.line(t); line != "FOCUS" {
		t.Fatal(line)
	}
	p.send(t, "release")
	if line := p.line(t); line != "RELEASED" {
		t.Fatal(line)
	}
	// Keep the released process alive: process exit must not hide a leaked name.
	// Clean release must actually relinquish the name, not merely stop callbacks.
	p2 := startActivationChild(t, id, "primary")
	if line := p2.line(t); line != "PRIMARY []" {
		t.Fatal(line)
	}
	p2.send(t, "release")
	if line := p2.line(t); line != "RELEASED" {
		t.Fatal(line)
	}
	p.send(t, "exit")
	p2.send(t, "exit")
	p.wait(t)
	p2.wait(t)
	runActivationChild(t, id, "no-bus")
	runActivationChild(t, id+".rapid", "rapid")
	blocked := startActivationChild(t, id+".blocked", "blocked-primary")
	if line := blocked.line(t); line != "BLOCKED" {
		t.Fatal(line)
	}
	runActivationChild(t, id+".blocked", "cancel-secondary")
	blocked.send(t, "exit")
	blocked.wait(t)
	runActivationChild(t, id+".startup", "startup-failure")
	t.Run("owned-bus-death", func(t *testing.T) {
		address, stop := startOwnedActivationBus(t)
		p := startActivationChildAtBus(t, id+".busdeath", "bus-death", address)
		if line := p.line(t); line != "PRIMARY []" {
			t.Fatal(line)
		}
		// Only kill the daemon created above, never the inherited session bus.
		stop()
		p.send(t, "bus-died")
		if line := p.line(t); line != "BUS_DEATH_SURVIVED" {
			t.Fatal(line)
		}
		p.wait(t)
	})
}

func TestActivationChild(t *testing.T) {
	role := os.Getenv("GOFASTR_ACTIVATION_CHILD")
	if role == "" {
		t.Skip("subprocess helper")
	}
	id := os.Getenv("GOFASTR_ACTIVATION_ID")
	if role == "no-bus" {
		t.Setenv("DBUS_SESSION_BUS_ADDRESS", "unix:path=/nonexistent-gofastr-activation-bus")
		s := New().(*linuxShell)
		handled, _, err := s.PrepareLaunch(id, "test", "gofastr605test")
		if handled || err == nil {
			t.Fatalf("missing bus accepted: handled=%v err=%v", handled, err)
		}
		s.ReleaseLaunch()
		fmt.Println("NO_BUS_REFUSED")
		return
	}
	if role == "rapid" {
		baseline := activationLiveClosures.Load()
		for i := 0; i < 20; i++ {
			s := New().(*linuxShell)
			handled, _, err := s.PrepareLaunch(id, "test", "gofastr605test")
			if err != nil || handled {
				t.Fatalf("rapid launch %d: handled=%v err=%v", i, handled, err)
			}
			s.ReleaseLaunch()
			s.ReleaseLaunch()
			if live := activationLiveClosures.Load(); live != baseline {
				t.Fatalf("rapid launch %d retained native closures: live=%d baseline=%d", i, live, baseline)
			}
		}
		return
	}
	if role == "blocked-primary" {
		if err := loadActivationGIO(); err != nil {
			t.Fatal(err)
		}
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		nativeContext := activationCall("g_main_context_new")
		activationCall("g_main_context_push_thread_default", nativeContext)
		defer activationCall("g_main_context_unref", nativeContext)
		defer activationCall("g_main_context_pop_thread_default", nativeContext)
		b := newActivationBroker(id, "gofastr605test")
		r := b.prepare(nil)
		defer b.cleanup()
		if r.err != nil || r.handled {
			t.Fatalf("blocked primary: %+v", r)
		}
		fmt.Println("BLOCKED")
		// Intentionally do not iterate this private context. The next process must
		// cancel its activation instead of waiting forever for remote Activate.
		line, err := bufio.NewReader(os.Stdin).ReadString('\n')
		if err != nil || line != "exit\n" {
			t.Fatalf("blocked helper exit: %q %v", line, err)
		}
		return
	}
	if role == "cancel-secondary" {
		start := time.Now()
		s := New().(*linuxShell)
		handled, _, err := s.PrepareLaunch(id, "test", "gofastr605test")
		defer s.ReleaseLaunch()
		if err == nil || handled {
			t.Fatalf("nonresponsive primary accepted: %v %v", handled, err)
		}
		if elapsed := time.Since(start); elapsed > activationTimeout+5*time.Second {
			t.Fatalf("preparation cancellation exceeded bound: %s", elapsed)
		}
		return
	}
	if role == "invalid" || role == "flood" {
		// Bypass sender filtering to exercise receiver bounds against an arbitrary
		// same-session caller. Production start() never accepts this unchecked list.
		var urls []string
		if role == "invalid" {
			urls = []string{"wrong://ignored", "gofastr605test://" + strings.Repeat("x", activationURLLimit), "gofastr605test://control/\n"}
		} else {
			for i := 0; i < 20; i++ {
				urls = append(urls, fmt.Sprintf("gofastr605test://warm/%d?q=%%2f#raw", i))
			}
		}
		if err := loadActivationGIO(); err != nil {
			t.Fatal(err)
		}
		if role == "invalid" {
			// Each invalid input gets its own call: the first refusal must not
			// prevent the overlength input from reaching the receiver as well.
			for i, raw := range urls {
				b := newActivationBroker(id, "gofastr605test")
				go b.run([]string{raw})
				r := <-b.ready
				b.close()
				if r.handled || r.err == nil || !strings.Contains(r.err.Error(), "InvalidArgs") {
					t.Fatalf("invalid input %d: result=%+v, want receiver InvalidArgs", i, r)
				}
			}
		} else {
			b := newActivationBroker(id, "gofastr605test")
			go b.run(urls)
			r := <-b.ready
			b.close()
			if r.err != nil || !r.handled {
				t.Fatalf("raw forward result=%+v", r)
			}
		}
		return
	}
	if role == "invalid-method" {
		testActivationInvalidMethods(t, id)
		return
	}
	if role == "startup-failure" {
		testActivationStartupFailure(t, id)
		return
	}
	closureBaseline := activationLiveClosures.Load()
	s := New().(*linuxShell)
	handled, urls, err := s.PrepareLaunch(id, "test", "gofastr605test")
	if err != nil {
		t.Fatal(err)
	}
	defer s.ReleaseLaunch()
	if role == "secondary" {
		if !handled {
			t.Fatal("secondary unexpectedly became primary")
		}
		return
	}
	if handled {
		t.Fatal("primary unexpectedly forwarded")
	}
	fmt.Printf("PRIMARY %q\n", urls)
	if role == "bus-death" {
		testActivationBusDeath(t, s, id, closureBaseline)
		return
	}
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		switch scanner.Text() {
		case "count":
			s.mu.RLock()
			b := s.activation
			s.mu.RUnlock()
			b.delivery.mu.Lock()
			n := len(b.delivery.pending)
			b.delivery.mu.Unlock()
			fmt.Printf("COUNT %d\n", n)
		case "attach":
			s.attachActivation(func(raw string) { fmt.Printf("URL %q\n", raw) }, func() { fmt.Println("FOCUS") })
		case "release":
			s.ReleaseLaunch()
			fmt.Println("RELEASED")
		case "exit":
			return
		default:
			t.Fatalf("unknown helper command %q", scanner.Text())
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
}

type activationChildProcess struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	lines  chan string
	exited chan error
	mu     sync.Mutex
	output []string
}

func startActivationChild(t *testing.T, id, role string, args ...string) *activationChildProcess {
	t.Helper()
	return startActivationChildAtBus(t, id, role, "", args...)
}
func startActivationChildAtBus(t *testing.T, id, role, address string, args ...string) *activationChildProcess {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	argv := append([]string{"-test.run=^TestActivationChild$", "-test.v", "-test.timeout=40s", "--"}, args...)
	cmd := exec.Command(exe, argv...)
	cmd.Env = activationChildEnv(id, role)
	if address != "" {
		var env []string
		for _, value := range cmd.Env {
			if !strings.HasPrefix(value, "DBUS_SESSION_BUS_ADDRESS=") {
				env = append(env, value)
			}
		}
		cmd.Env = append(env, "DBUS_SESSION_BUS_ADDRESS="+address)
	}
	in, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stderr = cmd.Stdout
	p := &activationChildProcess{cmd: cmd, stdin: in, lines: make(chan string, 128), exited: make(chan error, 1)}
	if err = cmd.Start(); err != nil {
		t.Fatal(err)
	}
	go func() {
		scanner := bufio.NewScanner(out)
		for scanner.Scan() {
			line := scanner.Text()
			p.mu.Lock()
			p.output = append(p.output, line)
			p.mu.Unlock()
			if strings.HasPrefix(line, "PRIMARY ") || strings.HasPrefix(line, "COUNT ") || strings.HasPrefix(line, "URL ") || line == "FOCUS" || line == "RELEASED" || line == "BLOCKED" || line == "BUS_DEATH_SURVIVED" {
				p.lines <- line
			}
		}
		scanErr := scanner.Err()
		close(p.lines)
		p.exited <- errors.Join(scanErr, cmd.Wait())
	}()
	t.Cleanup(func() {
		_ = in.Close()
		_ = cmd.Process.Kill()
	})
	return p
}
func (p *activationChildProcess) line(t *testing.T) string {
	t.Helper()
	select {
	case s, ok := <-p.lines:
		if !ok {
			p.mu.Lock()
			output := strings.Join(p.output, "\n")
			p.mu.Unlock()
			t.Fatalf("child stopped: %s", output)
		}
		return s
	case <-time.After(20 * time.Second):
		t.Fatal("activation child response timeout")
		return ""
	}
}
func (p *activationChildProcess) send(t *testing.T, s string) {
	t.Helper()
	if _, err := fmt.Fprintln(p.stdin, s); err != nil {
		t.Fatal(err)
	}
}
func (p *activationChildProcess) wait(t *testing.T) {
	t.Helper()
	select {
	case err := <-p.exited:
		if err != nil {
			p.mu.Lock()
			output := strings.Join(p.output, "\n")
			p.mu.Unlock()
			t.Fatalf("child: %v\n%s", err, output)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("activation child exit timeout")
	}
}
func activationChildEnv(id, role string) []string {
	env := []string{}
	for _, s := range os.Environ() {
		if !strings.HasPrefix(s, "GOFASTR_ACTIVATION_ID=") && !strings.HasPrefix(s, "GOFASTR_ACTIVATION_CHILD=") {
			env = append(env, s)
		}
	}
	return append(env, "GOFASTR_ACTIVATION_ID="+id, "GOFASTR_ACTIVATION_CHILD="+role)
}
func runActivationChild(t *testing.T, id, role string) {
	t.Helper()
	p := startActivationChild(t, id, role)
	p.wait(t)
}

// The base D-Bus signature and UTF-8 validation belong to GDBus. Construct only
// valid GVariants here; malformed UTF-8 is covered by the Go input guard tests.
func testActivationInvalidMethods(t *testing.T, id string) {
	t.Helper()
	if err := loadActivationGIO(); err != nil {
		t.Fatal(err)
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	nativeContext := activationCall("g_main_context_new")
	activationCall("g_main_context_push_thread_default", nativeContext)
	defer activationCall("g_main_context_unref", nativeContext)
	defer activationCall("g_main_context_pop_thread_default", nativeContext)
	b := newActivationBroker(id, "gofastr605test")
	defer b.cleanup()
	// Leave a live remote connection without enqueueing a focus or valid URL.
	r := b.prepare([]string{"wrong://ignored"})
	if r.handled || r.err == nil || !strings.Contains(r.err.Error(), "InvalidArgs") {
		t.Fatalf("protocol probe setup: %+v", r)
	}
	parameters := activationCall("g_variant_new_tuple", 0, 0)
	parameters = activationCall("g_variant_ref_sink", parameters)
	defer activationCall("g_variant_unref", parameters)
	resultType := activationStringCall("g_variant_type_new", "()")
	defer activationCall("g_variant_type_free", resultType)
	for _, tc := range []struct{ method, want string }{
		{"NotAnActivationMethod", "UnknownMethod"},
		{"Activate", "InvalidArgs"}, // () instead of the sole accepted (s)
	} {
		reply, err := b.call(b.name, activationPath, activationInterface, tc.method, parameters, resultType, 0)
		if reply != 0 {
			activationCall("g_variant_unref", reply)
		}
		if reply != 0 || err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("method %s: reply=%x error=%v, want %s", tc.method, reply, err, tc.want)
		}
	}
}

// This wrapper observes the real Linux launch seam without changing the battery
// gate or replacing native ownership with a fake. It never starts GTK.
type activationStartupProbe struct {
	*linuxShell
	prepared, released int
	broker             *activationBroker
}

func (s *activationStartupProbe) PrepareLaunch(id, name, scheme string) (bool, []string, error) {
	s.prepared++
	handled, urls, err := s.linuxShell.PrepareLaunch(id, name, scheme)
	s.mu.RLock()
	s.broker = s.activation
	s.mu.RUnlock()
	return handled, urls, err
}
func (s *activationStartupProbe) ReleaseLaunch() {
	s.released++
	s.linuxShell.ReleaseLaunch()
}
func testActivationStartupFailure(t *testing.T, id string) {
	t.Helper()
	t.Setenv(desktop.ManifestEnv, "")
	t.Setenv("GOFASTR_ISOLATION", "off")
	t.Setenv("GOFASTR_DESKTOP_DATA_DIR", t.TempDir())
	closureBaseline := activationLiveClosures.Load()
	s := &activationStartupProbe{linuxShell: New().(*linuxShell)}
	defer s.linuxShell.ReleaseLaunch()
	b := desktop.New(desktop.Config{
		ID: id, Title: "Native activation startup failure", Shell: s,
		DeepLink: &desktop.DeepLinkConfig{Scheme: "gofastr605test"}, KeepIsolation: true,
	})
	app := framework.NewApp(framework.WithConfig(framework.AppConfig{Name: "activation-startup-failure"}))
	app.RegisterBattery(b)
	// Omitting UIHost makes actual app.Start fail before a listener or UI starts.
	err := b.Run(app)
	if s.prepared == 0 {
		t.Fatal("native startup test requires -ldflags=-X=github.com/DonaldMurillo/gofastr/battery/desktop.builtDeepLinkScheme=gofastr605test; embedded launch gate was not exercised")
	}
	if err == nil || !strings.Contains(err.Error(), "no *uihost.UIHost") {
		t.Fatalf("startup error=%v, want missing UIHost", err)
	}
	if s.prepared != 1 || s.released != 1 || s.started || s.broker == nil {
		t.Fatalf("startup lifecycle: prepared=%d released=%d shellStarted=%v broker=%p", s.prepared, s.released, s.started, s.broker)
	}
	select {
	case <-s.broker.done:
	default:
		t.Fatal("startup failure returned before native broker cleanup")
	}
	if live := activationLiveClosures.Load(); live != closureBaseline {
		t.Fatalf("startup failure retained native closures: live=%d baseline=%d", live, closureBaseline)
	}
	s.mu.RLock()
	active := s.activation
	s.mu.RUnlock()
	if active != nil {
		t.Fatal("startup failure retained shell activation ownership")
	}
	// Reacquire while this process remains alive, so exit cannot hide a leak.
	next := New().(*linuxShell)
	defer next.ReleaseLaunch()
	handled, _, err := next.PrepareLaunch(id, "replacement", "gofastr605test")
	if err != nil || handled {
		t.Fatalf("startup failure retained bus name: handled=%v error=%v", handled, err)
	}
}

func testActivationBusDeath(t *testing.T, s *linuxShell, id string, closureBaseline int64) {
	t.Helper()
	s.mu.RLock()
	b := s.activation
	s.mu.RUnlock()
	if b == nil {
		t.Fatal("bus-death helper has no primary broker")
	}
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil || line != "bus-died\n" {
		t.Fatalf("bus-death helper command: %q %v", line, err)
	}
	select {
	case <-b.done:
	case <-time.After(activationTimeout + 5*time.Second):
		t.Fatal("owned bus closed but broker did not stop")
	}
	b.delivery.mu.Lock()
	closed, pending := b.delivery.closed, len(b.delivery.pending)
	b.delivery.mu.Unlock()
	if !closed || pending != 0 {
		t.Fatalf("bus death left delivery open: closed=%v pending=%d", closed, pending)
	}
	if live := activationLiveClosures.Load(); live != closureBaseline {
		t.Fatalf("bus death retained native closures: live=%d baseline=%d", live, closureBaseline)
	}
	s.ReleaseLaunch()
	s.ReleaseLaunch()
	next := New().(*linuxShell)
	defer next.ReleaseLaunch()
	handled, _, err := next.PrepareLaunch(id, "after bus death", "gofastr605test")
	if handled || err == nil {
		t.Fatalf("new launch accepted dead bus: handled=%v error=%v", handled, err)
	}
	fmt.Println("BUS_DEATH_SURVIVED")
}

func startOwnedActivationBus(t *testing.T) (string, func()) {
	t.Helper()
	// This daemon is exclusively test-owned. The inherited bus is neither
	// reconfigured nor signalled; only this exact exec.Cmd Process is killed.
	cmd := exec.Command("dbus-daemon", "--session", "--nofork", "--nopidfile", "--print-address=1")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("start test-owned dbus-daemon: %v", err)
	}
	done := make(chan struct{})
	go func() {
		_ = cmd.Wait()
		close(done)
	}()
	var once sync.Once
	stop := func() {
		once.Do(func() {
			_ = cmd.Process.Kill()
			select {
			case <-done:
			case <-time.After(10 * time.Second):
				t.Error("test-owned dbus-daemon did not exit after kill")
			}
		})
	}
	t.Cleanup(stop)
	type addressResult struct {
		address string
		err     error
	}
	address := make(chan addressResult, 1)
	go func() {
		s, err := bufio.NewReader(stdout).ReadString('\n')
		address <- addressResult{strings.TrimSpace(s), err}
	}()
	select {
	case result := <-address:
		if result.err != nil || !strings.HasPrefix(result.address, "unix:") {
			stop()
			message := "daemon is still exiting"
			select {
			case <-done:
				message = stderr.String()
			default:
			}
			t.Fatalf("test-owned bus address=%q error=%v: %s", result.address, result.err, message)
		}
		return result.address, stop
	case <-time.After(10 * time.Second):
		stop()
		t.Fatal("test-owned dbus-daemon did not publish its address")
		return "", stop
	}
}
