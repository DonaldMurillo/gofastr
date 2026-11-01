//go:build linux && (amd64 || arm64)

package linux

//gofastr:test-reads /core-ui/runtime/src/desktop.js

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/battery/desktop"
	"github.com/DonaldMurillo/gofastr/battery/desktop/internal/gtk"
)

func TestChooseEffect(t *testing.T) {
	glass := desktop.WindowStyle{Material: desktop.MaterialGlass}
	glassTransparent := desktop.WindowStyle{Material: desktop.MaterialGlass, Transparent: true}
	transparent := desktop.WindowStyle{Transparent: true}
	for _, tc := range []struct {
		name                   string
		style                  desktop.WindowStyle
		composited, rgba, blur bool
		want                   windowEffect
	}{
		{"plain window", desktop.WindowStyle{}, true, true, true, effectOpaque},
		{"transparent, compositor", transparent, true, true, false, effectTransparent},
		{"transparent, no compositor", transparent, false, true, false, effectOpaque},
		{"transparent, no rgba visual", transparent, true, false, false, effectOpaque},
		{"material with blur", glass, true, true, true, effectMaterial},
		// A tint over the sharp desktop reads worse than an opaque window.
		{"material, compositor without blur", glass, true, true, false, effectOpaque},
		{"material without blur but transparent", glassTransparent, true, true, false, effectTransparent},
		{"material, no compositor", glass, false, true, true, effectOpaque},
		{"unknown material", desktop.WindowStyle{Material: "chrome"}, true, true, true, effectOpaque},
	} {
		if got := chooseEffect(tc.style, tc.composited, tc.rgba, tc.blur); got != tc.want {
			t.Errorf("%s: got %v, want %v", tc.name, got, tc.want)
		}
	}
	for m := range materialTint {
		if !strings.HasPrefix(materialTint[m], "0.") {
			t.Errorf("material %q tint %q is not an alpha below 1", m, materialTint[m])
		}
	}
}

// The shell's drag handler and the runtime's startDrag must agree on the
// handler name and the message bytes, or a borderless Linux window never
// moves and nothing reports why.
func TestDragMessageMatchesRuntime(t *testing.T) {
	src, err := os.ReadFile(filepath.Join("..", "..", "..", "core-ui", "runtime", "src", "desktop.js"))
	if err != nil {
		t.Fatal(err)
	}
	js := string(src)
	if !strings.Contains(js, "const DRAG_MSG = '"+dragMessage+"';") {
		t.Errorf("desktop.js DRAG_MSG no longer equals the Linux shell's %s", dragMessage)
	}
	if !strings.Contains(js, "messageHandlers.gofastr") {
		t.Error("desktop.js no longer posts to messageHandlers.gofastr, the handler the Linux shell registers")
	}
}

// Real GTK, WebKit, X server and compositor. Without a compositor a
// transparent window stays opaque. With xcompmgr running, a transparent
// borderless window shows the root window through every pixel the page
// leaves unpainted, a material without compositor blur stays opaque, and a
// press-and-drag on a data-cui-window-drag handle moves the window while a
// startDrag with no press does not. Pixels come from the root window, so
// they are what the compositor put on screen, not WebKit's own snapshot.
//
// Needs xcompmgr and xdotool on PATH; GOFASTR_LINUX_COMPOSITOR=1 makes a
// missing tool a failure instead of a skip (the blocking CI runner sets it).
func TestNativeWindowEffects(t *testing.T) {
	if os.Getenv("GOFASTR_LINUX_NATIVE") != "1" {
		t.Skip("set GOFASTR_LINUX_NATIVE=1 in a graphical session")
	}
	for _, tool := range []string{"xcompmgr", "xdotool"} {
		if _, err := exec.LookPath(tool); err != nil {
			if os.Getenv("GOFASTR_LINUX_COMPOSITOR") == "1" {
				t.Fatalf("%s is required: %v", tool, err)
			}
			t.Skipf("%s not on PATH", tool)
		}
	}
	// The drag handle uses the same delegated listener and message the
	// runtime's desktop module ships (TestDragMessageMatchesRuntime pins it).
	page := `<!doctype html><title>fx</title>
<style>html,body{margin:0;background:transparent}#red{position:absolute;left:0;top:0;width:100px;height:100px;background:#ff0000}
#handle{position:absolute;left:120px;top:0;width:180px;height:60px;background:#0000ff}</style>
<div id="red"></div><div id="handle" data-cui-window-drag></div>
<script>document.addEventListener('mousedown',e=>{if(e.target.closest('[data-cui-window-drag]'))window.webkit.messageHandlers.gofastr.postMessage('{"type":"drag"}')})</script>`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, page)
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	s := New().(*linuxShell)
	result := make(chan error, 1)
	err := s.Run(ctx, desktop.WindowConfig{Title: "GoFastr effects", Width: 200, Height: 120}, func(main desktop.Window) {
		go func() { defer s.Quit(); result <- exerciseEffects(ctx, t, s, server.URL) }()
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
		t.Fatal("effects test did not finish:", ctx.Err())
	}
}

func exerciseEffects(ctx context.Context, t *testing.T, s *linuxShell, origin string) error {
	borderless := func(x, y int) desktop.WindowStyle {
		return desktop.WindowStyle{Chrome: desktop.ChromeNone, Transparent: true, X: &x, Y: &y}
	}
	open := func(id string, style desktop.WindowStyle) (*linuxWindow, error) {
		w, err := s.OpenWindow(id, desktop.WindowSpec{Title: id, Width: 300, Height: 200, Style: style}, origin)
		if err != nil {
			return nil, err
		}
		lw := w.(*linuxWindow)
		return lw, awaitPainted(ctx, lw)
	}

	// No compositor yet: the transparent request falls back to opaque.
	before, err := open("fx-before", borderless(40, 300))
	if err != nil {
		return err
	}
	if before.effect != effectOpaque {
		return fmt.Errorf("without a compositor: effect %v, want opaque", before.effect)
	}

	compositor := exec.Command("xcompmgr")
	if err := compositor.Start(); err != nil {
		return err
	}
	defer func() { _ = compositor.Process.Kill(); _ = compositor.Wait() }()
	if err := waitComposited(ctx, s); err != nil {
		return err
	}

	clear, err := open("fx-clear", borderless(400, 100))
	if err != nil {
		return err
	}
	if clear.effect != effectTransparent {
		return fmt.Errorf("with xcompmgr: effect %v, want transparent", clear.effect)
	}
	x, y := 760, 300
	glass, err := open("fx-glass", desktop.WindowStyle{Chrome: desktop.ChromeNone, Material: desktop.MaterialGlass, X: &x, Y: &y})
	if err != nil {
		return err
	}
	if glass.effect != effectOpaque {
		return fmt.Errorf("material under xcompmgr (no blur): effect %v, want opaque", glass.effect)
	}
	time.Sleep(500 * time.Millisecond) // let xcompmgr repaint
	if err := checkPixels(s, t, map[string]pixelWant{
		// The page's red square paints through in every window.
		"transparent window, red square": {400 + 50, 100 + 50, isRed},
		// Unpainted page, transparent window: the compositor's root fill.
		"transparent window, empty page": {400 + 150, 100 + 150, notWhite},
		// The opaque fallbacks paint WebKit's white page background.
		"opaque fallback, empty page":   {40 + 150, 300 + 150, isWhite},
		"material fallback, empty page": {760 + 150, 300 + 150, isWhite},
	}); err != nil {
		return err
	}

	// startDrag with no press must not move the window.
	if _, err := clear.EvalAsync(ctx, `window.webkit.messageHandlers.gofastr.postMessage('{"type":"drag"}'); return true`); err != nil {
		return err
	}
	time.Sleep(300 * time.Millisecond)
	if f, err := clear.Frame(); err != nil || f.X != 400 || f.Y != 100 {
		return fmt.Errorf("a pressless startDrag moved the window to %+v (%v)", f, err)
	}
	// Press on the handle, drag by (+120, +80), release.
	steps := [][]string{
		{"mousemove", "--sync", "560", "120"},
		{"mousedown", "1"},
		{"sleep", "0.4"},
		{"mousemove", "--sync", "600", "150"},
		{"mousemove", "--sync", "640", "180"},
		{"mousemove", "--sync", "680", "200"},
		{"sleep", "0.3"},
		{"mouseup", "1"},
	}
	for _, step := range steps {
		if out, err := exec.CommandContext(ctx, "xdotool", step...).CombinedOutput(); err != nil {
			return fmt.Errorf("xdotool %v: %v %s", step, err, out)
		}
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		f, err := clear.Frame()
		if err == nil && f.X == 520 && f.Y == 180 {
			break
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("drag on the handle left the window at %+v (%v), want (520,180)", f, err)
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Log("opaque fallback, transparent pixels through xcompmgr, material fallback and handle drag verified")
	return nil
}

func awaitPainted(ctx context.Context, w *linuxWindow) error {
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		out, err := w.EvalAsync(ctx, `return document.readyState === "complete" && !!document.getElementById("red")`)
		if err == nil && string(out) == "true" {
			time.Sleep(300 * time.Millisecond) // first paint after load
			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	return fmt.Errorf("%s: page did not load", w.id)
}

func waitComposited(ctx context.Context, s *linuxShell) error {
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		var composited bool
		if err := s.Main(func() {
			s.mu.RLock()
			main := s.windows[desktop.MainWindowID]
			s.mu.RUnlock()
			screen := call(gtk.Sym.GtkWidgetGetScreen, main.handle)
			composited = call(gtk.Sym.GdkScreenIsComposited, screen)&0xff != 0
		}); err != nil {
			return err
		}
		if composited {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
	return errors.New("GDK never reported a compositing manager after xcompmgr started")
}

type pixelWant struct {
	x, y  int
	check func(r, g, b uint8) bool
}

func isRed(r, g, b uint8) bool   { return r > 200 && g < 60 && b < 60 }
func isWhite(r, g, b uint8) bool { return r > 240 && g > 240 && b > 240 }
func notWhite(r, g, b uint8) bool {
	return !isWhite(r, g, b) && !isRed(r, g, b)
}

// checkPixels reads screen pixels from the root window on the UI thread.
// gdk_pixbuf_get_from_window on the root returns what the X server holds,
// which under a compositing manager is the composited result.
func checkPixels(s *linuxShell, t *testing.T, wants map[string]pixelWant) error {
	syms, err := loadPixelSymbols()
	if err != nil {
		return err
	}
	var failures []string
	err = s.Main(func() {
		root := call(syms.rootWindow)
		for name, want := range wants {
			pixbuf := call(syms.fromWindow, root, uintptr(want.x), uintptr(want.y), 1, 1)
			if pixbuf == 0 {
				failures = append(failures, name+": no pixbuf")
				continue
			}
			p := (*[3]uint8)(nativePointer(call(syms.pixels, pixbuf)))
			r, g, b := p[0], p[1], p[2]
			call(gtk.Sym.GObjectUnref, pixbuf)
			t.Logf("%s at (%d,%d): #%02x%02x%02x", name, want.x, want.y, r, g, b)
			if !want.check(r, g, b) {
				failures = append(failures, fmt.Sprintf("%s at (%d,%d) is #%02x%02x%02x", name, want.x, want.y, r, g, b))
			}
		}
	})
	if err != nil {
		return err
	}
	if len(failures) > 0 {
		return errors.New(strings.Join(failures, "; "))
	}
	return nil
}

type pixelSymbols struct{ rootWindow, fromWindow, pixels uintptr }

// Test-only entry points, kept out of the production symbol table.
func loadPixelSymbols() (pixelSymbols, error) {
	var out pixelSymbols
	gdk, err := gtk.DlopenGlobal("libgdk-3.so.0")
	if err != nil {
		return out, err
	}
	pixbuf, err := gtk.DlopenGlobal("libgdk_pixbuf-2.0.so.0")
	if err != nil {
		return out, err
	}
	for _, s := range []struct {
		lib    uintptr
		name   string
		target *uintptr
	}{
		{gdk, "gdk_get_default_root_window", &out.rootWindow},
		{gdk, "gdk_pixbuf_get_from_window", &out.fromWindow},
		{pixbuf, "gdk_pixbuf_get_pixels", &out.pixels},
	} {
		addr, err := gtk.Dlsym(s.lib, s.name)
		if err != nil {
			return out, err
		}
		*s.target = addr
	}
	return out, nil
}
