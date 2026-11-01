//go:build linux && (amd64 || arm64)

package linux

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/battery/desktop"
)

// onToolkitThread runs every call on one OS thread, inline when the caller
// is already on it, and hands a panic back to the caller's goroutine. No
// display needed: the toolkit thread is plain Go until GTK runs on it.
func TestToolkitThreadIsStable(t *testing.T) {
	var first, second, inner int64
	if err := onToolkitThread(func() {
		first = int64(syscall.Gettid())
		// A nested call from the toolkit thread must run inline, not
		// deadlock waiting for itself.
		if err := onToolkitThread(func() { inner = int64(syscall.Gettid()) }); err != nil {
			t.Error(err)
		}
	}); err != nil {
		t.Fatal(err)
	}
	if err := onToolkitThread(func() { second = int64(syscall.Gettid()) }); err != nil {
		t.Fatal(err)
	}
	if first == 0 || first != second || first != inner {
		t.Fatalf("toolkit calls ran on threads %d, %d, %d; want one thread", first, second, inner)
	}
	if first == int64(syscall.Gettid()) {
		t.Fatal("toolkit work ran on the test goroutine's thread")
	}
	func() {
		defer func() {
			if v := recover(); v != "toolkit panic" {
				t.Fatalf("recovered %v, want the toolkit panic re-raised on the caller", v)
			}
		}()
		_ = onToolkitThread(func() { panic("toolkit panic") })
		t.Fatal("panic was swallowed")
	}()
	var after int64
	if err := onToolkitThread(func() { after = int64(syscall.Gettid()) }); err != nil || after != first {
		t.Fatalf("after a panic: thread %d (err %v), want %d", after, err, first)
	}
}

// runtime.Goexit in toolkit work ends the toolkit goroutine, and with it
// the thread GTK is bound to. Every later call must fail, never block on a
// channel nobody reads. It poisons the process, so it runs in a child.
func TestToolkitThreadLostAfterGoexit(t *testing.T) {
	if os.Getenv("GOFASTR_TOOLKIT_GOEXIT_CHILD") == "1" {
		if err := onToolkitThread(func() { _ = 1 }); err != nil {
			fmt.Println("FIRST", err)
			os.Exit(2)
		}
		err := onToolkitThread(func() { runtime.Goexit() })
		fmt.Println("GOEXIT", errors.Is(err, errToolkitThreadLost))
		done := make(chan error, 1)
		go func() { done <- onToolkitThread(func() {}) }()
		select {
		case err := <-done:
			fmt.Println("AFTER", errors.Is(err, errToolkitThreadLost))
		case <-time.After(5 * time.Second):
			fmt.Println("AFTER blocked")
		}
		os.Exit(0)
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestToolkitThreadLostAfterGoexit$", "-test.count=1")
	cmd.Env = append(os.Environ(), "GOFASTR_TOOLKIT_GOEXIT_CHILD=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("child: %v\n%s", err, out)
	}
	for _, want := range []string{"GOEXIT true", "AFTER true"} {
		if !strings.Contains(string(out), want) {
			t.Fatalf("child output lacks %q:\n%s", want, out)
		}
	}
}

// Three shell lifetimes run one after another in one process (#605). Each
// gets a working WebKit view and its own ephemeral cookie store, all run on
// one OS thread, and a Run while a lifetime is live is refused rather than
// fighting it for GTK's default main context.
func TestNativeGTKLifetimes(t *testing.T) {
	if os.Getenv("GOFASTR_LINUX_NATIVE") != "1" {
		t.Skip("set GOFASTR_LINUX_NATIVE=1 in a graphical session")
	}
	var served atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := served.Add(1)
		http.SetCookie(w, &http.Cookie{Name: fmt.Sprintf("life%d", n), Value: "1", Path: "/"})
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<!doctype html><title>lifetime</title><p>lifetime</p>`)
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	var threads []int64
	for life := 1; life <= 3; life++ {
		s := New().(*linuxShell)
		result := make(chan error, 1)
		var tid int64
		// Each lifetime is started from a goroutine that exits while locked
		// to its thread, so the runtime destroys that thread afterwards. A
		// shell that ran GTK on its caller's thread would land on a new
		// thread every lifetime and fail the check below.
		runErr := make(chan error, 1)
		go func() {
			runtime.LockOSThread()
			runErr <- s.Run(ctx, desktop.WindowConfig{Title: fmt.Sprintf("GoFastr lifetime %d", life), Width: 480, Height: 320}, func(w desktop.Window) {
				tid = int64(syscall.Gettid())
				if err := w.Navigate(server.URL); err != nil {
					result <- err
					s.Quit()
					return
				}
				go func() { defer s.Quit(); result <- exerciseLifetime(ctx, w) }()
			})
		}()
		if err := <-runErr; err != nil {
			t.Fatalf("lifetime %d: Run: %v", life, err)
		}
		select {
		case err := <-result:
			if err != nil {
				t.Fatalf("lifetime %d: %v", life, err)
			}
		case <-ctx.Done():
			t.Fatalf("lifetime %d did not finish: %v", life, ctx.Err())
		}
		if owner := toolkitOwner.Load(); owner != nil {
			t.Fatalf("lifetime %d: toolkit ownership retained after Run returned", life)
		}
		threads = append(threads, tid)
	}
	for _, tid := range threads[1:] {
		if tid != threads[0] {
			t.Fatalf("lifetimes ran on threads %v; GTK must stay on one thread", threads)
		}
	}
	t.Logf("three GTK/WebKit lifetimes on thread %d, each with a fresh cookie store", threads[0])
}

// exerciseLifetime waits for the page, checks the cookie store holds only
// this lifetime's cookie, and checks a concurrent Run is refused.
func exerciseLifetime(ctx context.Context, w desktop.Window) error {
	e := w.(desktop.PageEvaluator)
	deadline := time.Now().Add(20 * time.Second)
	var cookies string
	for time.Now().Before(deadline) {
		out, err := e.EvalAsync(ctx, `return document.readyState === "complete" ? document.cookie : ""`)
		if err == nil && len(out) > 2 {
			cookies = string(out)
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if cookies == "" {
		return errors.New("page did not load")
	}
	if strings.Count(cookies, "life") != 1 {
		return fmt.Errorf("cookie store carried another lifetime's cookies: %s", cookies)
	}
	if err := New().Run(ctx, desktop.WindowConfig{}, nil); !errors.Is(err, errToolkitBusy) {
		return fmt.Errorf("concurrent Run: %v, want errToolkitBusy", err)
	}
	return nil
}
