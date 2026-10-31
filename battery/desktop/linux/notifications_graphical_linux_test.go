//go:build linux && (amd64 || arm64)

package linux

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
	"unsafe"

	"github.com/DonaldMurillo/gofastr/battery/desktop"
	"github.com/DonaldMurillo/gofastr/battery/desktop/internal/gtk"
)

// An actual daemon renders the toast on the test X server. A separate private
// bus (with no desktop service activation files) prevents contacting or starting
// a daemon in the user's session. No WebKit view is needed for notifications.
func TestNativeNotificationGraphical(t *testing.T) {
	if os.Getenv("GOFASTR_LINUX_NATIVE") != "1" || os.Getenv("GOFASTR_LINUX_NOTIFICATIONS") != "1" {
		t.Skip("set GOFASTR_LINUX_NATIVE=1 and GOFASTR_LINUX_NOTIFICATIONS=1 in an X11 graphical session")
	}
	for _, tool := range []string{"dunst", "dunstctl", "xdotool"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Fatal(err)
		}
	}
	startNotificationTestBus(t)
	root := t.TempDir()
	config := filepath.Join(root, "dunstrc")
	instance := "gofastr-notification-" + rand.Text()
	if err := os.WriteFile(config, []byte("[global]\nclass = "+instance+"\n"+`font = Sans 14
format = <b>%s</b>\n%b
markup = full
width = 420
height = 160
origin = top-right
offset = 20x20
padding = 16
horizontal_padding = 16
frame_width = 2
frame_color = "#59b4ff"
background = "#172236"
foreground = "#ffffff"
force_xwayland = true
[urgency_normal]
timeout = 0
`), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	log, err := os.CreateTemp(root, "dunst-*.log")
	if err != nil {
		t.Fatal(err)
	}
	defer log.Close()
	command := exec.CommandContext(ctx, "dunst", "-config", config)
	command.Stdout, command.Stderr = log, log
	command.WaitDelay = time.Second
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = command.Process.Kill(); _ = command.Wait() }()
	if err := waitNotificationCount(ctx, "0"); err != nil {
		detail, _ := os.ReadFile(log.Name())
		t.Fatalf("daemon startup: %v\n%s", err, detail)
	}
	s := New().(*linuxShell)
	result := make(chan error, 1)
	err = runNotificationGTK(ctx, s, "GoFastr notification QA", func() {
		go func() {
			defer s.Quit()
			result <- exerciseNotificationGraphical(ctx, s, root, instance)
		}()
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
		t.Fatal("graphical notification exercise did not finish", ctx.Err())
	}
	t.Log("dunst displayed and dismissed one native notification; inspect notification.png for rendered text/layout")
}

func notificationControl(ctx context.Context, args ...string) (string, error) {
	child, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	command := exec.CommandContext(child, "dunstctl", args...)
	command.WaitDelay = time.Second
	output, err := command.CombinedOutput()
	return strings.TrimSpace(string(output)), err
}

func waitNotificationCount(ctx context.Context, want string) error {
	deadline := time.Now().Add(5 * time.Second)
	for {
		got, err := notificationControl(ctx, "count", "displayed")
		if err == nil && got == want {
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("dunst displayed count = %q (%v), want %s", got, err, want)
		}
		time.Sleep(25 * time.Millisecond)
	}
}

func exerciseNotificationGraphical(ctx context.Context, s *linuxShell, root, instance string) error {
	if id, err := notificationWindow(ctx, instance); err != nil || id != 0 {
		return fmt.Errorf("notification window before Show: %d (%v)", id, err)
	}
	if err := s.Show(ctx, desktop.Notification{Title: "GoFastr native notification", Subtitle: "GTK · libnotify · CGO_ENABLED=0", Body: "Literal <b>markup</b> & Unicode π\nDelivered to a private X11 daemon."}); err != nil {
		return err
	}
	if err := waitNotificationCount(ctx, "1"); err != nil {
		return err
	}
	// Require this daemon's unique mapped WM_CLASS instance, not root pixels
	// that another desktop window could satisfy. Its private config owns the class.

	var data []byte
	deadline := time.Now().Add(5 * time.Second)
	for {
		var captureErr error
		if err := s.Main(func() {
			id, err := notificationWindow(ctx, instance)
			if err != nil {
				captureErr = err
				return
			}
			if id == 0 {
				captureErr = errors.New("notification window not mapped")
				return
			}
			data, captureErr = notificationWindowPNG(id)
		}); err != nil {
			return err
		}
		if captureErr != nil && time.Now().After(deadline) {
			return captureErr
		}
		if notificationHasPixels(data) {
			break
		}
		if time.Now().After(deadline) {
			return errors.New("notification frame/text pixels did not appear")
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		time.Sleep(25 * time.Millisecond)
	}
	output := os.Getenv("GOFASTR_LINUX_TEST_OUTPUT")
	if output == "" {
		output = root
	}
	if err := os.MkdirAll(output, 0700); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(output, "notification.png"), data, 0600); err != nil {
		return err
	}
	if _, err := notificationControl(ctx, "close-all"); err != nil {
		return err
	}
	if err := waitNotificationCount(ctx, "0"); err != nil {
		return err
	}
	deadline = time.Now().Add(5 * time.Second)
	for {
		id, err := notificationWindow(ctx, instance)
		if err != nil {
			return err
		}
		if id == 0 {
			return nil
		}
		if time.Now().After(deadline) {
			return errors.New("owned notification window remained mapped after dismissal")
		}
		time.Sleep(25 * time.Millisecond)
	}
}

func notificationWindowPNG(windowID uintptr) ([]byte, error) {
	symbols := map[string]uintptr{}
	for _, lib := range []struct {
		name    string
		symbols []string
	}{
		{"libgdk-3.so.0", []string{"gdk_x11_window_foreign_new_for_display", "gdk_window_get_width", "gdk_window_get_height", "gdk_pixbuf_get_from_window", "gdk_display_sync"}},
		{"libgdk_pixbuf-2.0.so.0", []string{"gdk_pixbuf_save_to_bufferv"}},
	} {
		handle, err := gtk.DlopenGlobal(lib.name)
		if err != nil {
			return nil, err
		}
		for _, name := range lib.symbols {
			symbol, err := gtk.Dlsym(handle, name)
			if err != nil {
				return nil, err
			}
			symbols[name] = symbol
		}
	}
	capsCall(symbols["gdk_display_sync"], capsCall(gtk.Sym.GdkDisplayGetDefault))
	window := capsCall(symbols["gdk_x11_window_foreign_new_for_display"], capsCall(gtk.Sym.GdkDisplayGetDefault), windowID)
	if window == 0 {
		return nil, errors.New("notification capture requires its mapped X11 window")
	}
	defer capsCall(gtk.Sym.GObjectUnref, window)
	width, height := capsCall(symbols["gdk_window_get_width"], window), capsCall(symbols["gdk_window_get_height"], window)
	pixbuf := capsCall(symbols["gdk_pixbuf_get_from_window"], window, 0, 0, width, height)
	if pixbuf == 0 {
		return nil, errors.New("capture notification pixels")
	}
	defer capsCall(gtk.Sym.GObjectUnref, pixbuf)
	var buffer, length, nativeError uintptr
	kind := capsCString("png")
	ok := capsCall(symbols["gdk_pixbuf_save_to_bufferv"], pixbuf, uintptr(unsafe.Pointer(&buffer)), uintptr(unsafe.Pointer(&length)), uintptr(unsafe.Pointer(&kind[0])), 0, 0, uintptr(unsafe.Pointer(&nativeError)))
	runtime.KeepAlive(kind)
	if nativeError != 0 {
		capsCall(gtk.Sym.GErrorFree, nativeError)
	}
	if buffer != 0 {
		defer capsCall(gtk.Sym.GFree, buffer)
	}
	if ok == 0 || buffer == 0 || length > 16<<20 {
		return nil, errors.New("encode notification capture")
	}
	return bytes.Clone(unsafe.Slice((*byte)(capsNativePointer(buffer)), int(length))), nil
}

// A daemon count is insufficient: require the configured frame and foreground
// in the captured pixels too. Text content/layout still gets visual inspection.
func notificationHasPixels(data []byte) bool {
	image, err := png.Decode(bytes.NewReader(data))
	if err != nil || image.Bounds().Dx() < 420 || image.Bounds().Dy() < 80 {
		return false
	}
	frame, text := 0, 0
	for y := image.Bounds().Min.Y; y < image.Bounds().Max.Y; y++ {
		for x := image.Bounds().Min.X; x < image.Bounds().Max.X; x++ {
			r, g, b, _ := image.At(x, y).RGBA()
			if r == 0x5959 && g == 0xb4b4 && b == 0xffff {
				frame++
			}
			if r > 0xe000 && g > 0xe000 && b > 0xe000 {
				text++
			}
		}
	}
	return frame >= 100 && text >= 20
}

func notificationWindow(ctx context.Context, instance string) (uintptr, error) {
	child, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	cmd := exec.CommandContext(child, "xdotool", "search", "--onlyvisible", "--classname", "^"+regexp.QuoteMeta(instance)+"$")
	cmd.WaitDelay = time.Second
	output, err := cmd.CombinedOutput()
	var status *exec.ExitError
	if errors.As(err, &status) && status.ExitCode() == 1 && len(bytes.TrimSpace(output)) == 0 {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("find owned notification window: %w: %s", err, output)
	}
	ids := strings.Fields(string(output))
	if len(ids) != 1 {
		return 0, fmt.Errorf("expected one owned notification window, got %q", output)
	}
	id, err := strconv.ParseUint(ids[0], 10, 64)
	return uintptr(id), err
}
