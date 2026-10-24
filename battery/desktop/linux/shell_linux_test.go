//go:build linux && (amd64 || arm64)

package linux

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
	"unsafe"

	"github.com/DonaldMurillo/gofastr/battery/desktop"
	"github.com/DonaldMurillo/gofastr/battery/desktop/internal/gtk"
	"github.com/DonaldMurillo/gofastr/core/i18n"
)

func TestNewIsLazy(t *testing.T) {
	before := gtk.DlopenCount()
	shell := New()
	if _, ok := shell.(*linuxShell); !ok {
		t.Fatalf("New returned %T", shell)
	}
	if gtk.DlopenCount() != before {
		t.Fatal("New loaded native libraries")
	}
	if err := shell.SetTrayTitle("test"); err == nil || strings.Contains(err.Error(), "GOOS=") {
		t.Fatalf("unexpected tray result: %v", err)
	}
}
func TestQuitBeforeRun(t *testing.T) {
	s := New()
	s.Quit()
	if err := s.Run(context.Background(), desktop.WindowConfig{}, func(desktop.Window) { t.Fatal("ready after Quit") }); err != nil {
		t.Fatal(err)
	}
}
func TestCancelledBeforeRun(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := New().Run(ctx, desktop.WindowConfig{}, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v", err)
	}
}
func TestGTKModifiers(t *testing.T) {
	if got := gtkModifiers(1<<20 | 1<<17 | 1<<19); got != 13 {
		t.Fatalf("got modifier mask %d", got)
	}
	if gtkModifiers(1<<18) != 4 {
		t.Fatal("Control not preserved")
	}
}

func TestSettingsKeepsShellLocale(t *testing.T) {
	ctx := i18n.WithContext(context.Background(), i18n.Locale{Tag: "fr"})
	s := New().(*linuxShell)
	s.textCtx = ctx
	got := make(chan context.Context, 1)
	s.cfg.OnSettings = func(ctx context.Context) { got <- ctx }
	action := menuAction{window: &linuxWindow{shell: s}, plan: desktop.MenuItemPlan{Role: desktop.RoleSettings}}
	action.activate()
	select {
	case called := <-got:
		if i18n.FromContext(called).Tag != "fr" {
			t.Fatalf("Settings lost the shell's locale: %v", i18n.FromContext(called))
		}
	case <-time.After(time.Second):
		t.Fatal("Settings callback did not run")
	}
	// An event arriving for a closed window must not open Settings.
	action.window.closed.Store(true)
	action.activate()
	select {
	case <-got:
		t.Fatal("closed window dispatched Settings")
	case <-time.After(20 * time.Millisecond):
	}
}
func TestReadBoundedString(t *testing.T) {
	b := []byte{'a', 'b', 0}
	var pin runtime.Pinner
	pin.Pin(&b[0])
	defer pin.Unpin()
	ptr := uintptr(unsafe.Pointer(&b[0]))
	if text, ok := readBoundedString(ptr, 3); text != "ab" || !ok {
		t.Fatalf("complete: %q %v", text, ok)
	}
	if text, ok := readBoundedString(ptr, 2); text != "ab" || ok {
		t.Fatalf("truncated: %q %v", text, ok)
	}
}
func TestRejectNUL(t *testing.T) {
	if validateString("title\x00suffix") == nil {
		t.Fatal("NUL accepted")
	}
}

// This test runs real GTK/WebKit, never a fake shell or an external browser.
// Opt in in a graphical Linux session: CGO_ENABLED=0 GOFASTR_LINUX_NATIVE=1
// go test ./battery/desktop/linux -run TestNativeGTKWebKit -count=1 -v.
// Each invocation gets one GTK main-loop lifetime and a fresh cookie store.
func TestNativeGTKWebKit(t *testing.T) {
	if os.Getenv("GOFASTR_LINUX_NATIVE") != "1" {
		t.Skip("set GOFASTR_LINUX_NATIVE=1 in a graphical session")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			http.SetCookie(w, &http.Cookie{Name: "shared", Value: "linux-native", Path: "/"})
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<!doctype html><html><head><title>GoFastr Linux</title><style>body{font:24px sans-serif;background:#16223a;color:white;padding:48px}button{padding:16px}</style></head><body><h1>GoFastr Linux native</h1><p>GTK 3 · WebKitGTK · CGO_ENABLED=0</p><button id="test" onclick="this.textContent='Clicked'">Native WebView works</button></body></html>`)
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	s := New().(*linuxShell)
	result := make(chan error, 1)
	err := s.Run(ctx, desktop.WindowConfig{Title: "GoFastr Linux Native Test", Width: 840, Height: 600}, func(window desktop.Window) {
		if err := window.Navigate(server.URL); err != nil {
			result <- err
			s.Quit()
			return
		}
		go func() { defer s.Quit(); result <- exerciseNative(ctx, s, window, server.URL) }()
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
		t.Fatal("native test did not finish:", ctx.Err())
	}
}

func exerciseNative(ctx context.Context, s *linuxShell, w desktop.Window, origin string) error {
	evaluator := w.(desktop.PageEvaluator)
	awaitPage := func(e desktop.PageEvaluator, id string) error {
		deadline := time.Now().Add(20 * time.Second)
		for time.Now().Before(deadline) {
			out, err := e.EvalAsync(ctx, `return document.readyState === "complete" && window.__gofastr_desktop && window.__gofastr_desktop.window`)
			if err == nil && string(out) == `"`+id+`"` {
				return nil
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(50 * time.Millisecond):
			}
		}
		return fmt.Errorf("document/bootstrap did not load for %s", id)
	}
	if err := awaitPage(evaluator, "main"); err != nil {
		return err
	}
	out, err := evaluator.EvalAsync(ctx, `return await Promise.resolve({answer:42,os:window.__gofastr_desktop.os})`)
	if err != nil {
		return err
	}
	var value struct {
		Answer int    `json:"answer"`
		OS     string `json:"os"`
	}
	if err = json.Unmarshal(out, &value); err != nil || value.Answer != 42 || value.OS != "linux" {
		return fmt.Errorf("async eval: %s (%v)", out, err)
	}
	if _, err := evaluator.EvalAsync(ctx, `throw new Error("expected native failure")`); err == nil {
		return errors.New("JavaScript exception was ignored")
	}
	if _, err := evaluator.EvalAsync(ctx, `return "x".repeat(5*1024*1024)`); err == nil {
		return errors.New("oversized JavaScript result was accepted")
	}
	if err = w.SetTitle("GoFastr Linux verified"); err != nil {
		return err
	}
	if w.Title() != "GoFastr Linux verified" {
		return errors.New("title did not update")
	}
	if err = w.SetFrame(desktop.Frame{X: 60, Y: 60, Width: 900, Height: 640}); err != nil {
		return err
	}
	if err = s.Clipboard().WriteText(ctx, "Linux clipboard ✓"); err != nil {
		return err
	}
	text, err := s.Clipboard().ReadText(ctx)
	if err != nil || text != "Linux clipboard ✓" {
		return fmt.Errorf("clipboard: %q (%v)", text, err)
	}
	second, err := s.OpenWindow("w2", desktop.WindowSpec{Title: "Shared session", Width: 420, Height: 260}, origin+"/secondary")
	if err != nil {
		return err
	}
	if err = awaitPage(second.(desktop.PageEvaluator), "w2"); err != nil {
		return err
	}
	cookie, err := second.(desktop.PageEvaluator).EvalAsync(ctx, `return document.cookie`)
	if err != nil || !strings.Contains(string(cookie), "shared=linux-native") {
		return fmt.Errorf("secondary cookies: %s (%v)", cookie, err)
	}
	if err = second.Close(); err != nil {
		return err
	}
	prompt := make(chan error, 1)
	go func() {
		decision, e := s.Prompt(ctx, desktop.PermissionRequest{Capability: "clipboard", Method: "readText", Permission: "clipboard.read", Description: "Native Linux integration test"})
		if e == nil && decision != desktop.DecisionAllowOnce {
			e = fmt.Errorf("prompt decision %v", decision)
		}
		prompt <- e
	}()
	if err = s.ClickPrompt("Allow once"); err != nil {
		return err
	}
	if err = <-prompt; err != nil {
		return err
	}
	pickerCtx, pickerCancel := context.WithCancel(ctx)
	picker := make(chan error, 1)
	go func() { _, e := s.Dialogs().OpenFile(pickerCtx, desktop.OpenOptions{}); picker <- e }()
	time.Sleep(150 * time.Millisecond)
	pickerCancel()
	if err = <-picker; !errors.Is(err, desktop.ErrCancelled) {
		return fmt.Errorf("file dialog cancellation: %v", err)
	}
	// Give layout/paint and picker destruction one frame before reading pixels.
	time.Sleep(300 * time.Millisecond)
	if hold, err := time.ParseDuration(os.Getenv("GOFASTR_LINUX_TEST_HOLD")); err == nil && hold > 0 && hold <= 30*time.Second {
		time.Sleep(hold)
	}
	data, err := w.Snapshot(ctx)
	if err != nil {
		return err
	}
	config, err := png.DecodeConfig(bytes.NewReader(data))
	if err != nil || config.Width < 100 || config.Height < 100 {
		return fmt.Errorf("snapshot invalid: %+v (%v)", config, err)
	}
	if dir := os.Getenv("GOFASTR_LINUX_TEST_OUTPUT"); dir != "" {
		if err = os.MkdirAll(dir, 0700); err != nil {
			return err
		}
		if err = os.WriteFile(filepath.Join(dir, "linux-native.png"), data, 0600); err != nil {
			return err
		}
	}
	// Stress fixed callbacks and GC while WebKit/GTK hold native references.
	for i := 0; i < 75; i++ {
		if _, err = evaluator.EvalAsync(ctx, `return 1+1`); err != nil {
			return err
		}
		if i%10 == 0 {
			runtime.GC()
		}
	}
	return w.Close()
}
