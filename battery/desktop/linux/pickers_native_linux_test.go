//go:build linux && (amd64 || arm64)

package linux

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/battery/desktop"
	"github.com/DonaldMurillo/gofastr/battery/desktop/internal/gtk"
)

// The unattended twin of TestNativeInteractivePickers. The pickers are the
// real GTK choosers: with no portal on the bus, GtkFileChooserNative shows an
// in-process GtkFileChooserDialog. xdotool types into them through the X
// server, the way a user would, and the test finds each dialog among the
// process's own mapped toplevels. It proves the selected paths, that Save
// creates nothing, and the overwrite confirmation: Cancel there keeps the
// chooser open with no result, and Replace returns the existing path.
func TestNativePickersAutomated(t *testing.T) {
	if os.Getenv("GOFASTR_LINUX_NATIVE") != "1" || os.Getenv("GOFASTR_LINUX_PICKERS") != "1" {
		t.Skip("set GOFASTR_LINUX_NATIVE=1 and GOFASTR_LINUX_PICKERS=1 in a graphical session with xdotool")
	}
	if _, err := exec.LookPath("xdotool"); err != nil {
		t.Fatal("GOFASTR_LINUX_PICKERS=1 needs xdotool on PATH")
	}
	// A portal would move the chooser out of process, where the toplevel
	// checks below cannot see it.
	t.Setenv("GTK_USE_PORTAL", "0")
	root := t.TempDir()
	if err := os.Chmod(root, 0700); err != nil {
		t.Fatal(err)
	}
	input := filepath.Join(root, "input.txt")
	existing := filepath.Join(root, "overwrite.txt")
	contents := []byte("GoFastr disposable picker fixture; do not modify.\n")
	for _, path := range []string{input, existing} {
		if err := os.WriteFile(path, contents, 0600); err != nil {
			t.Fatal(err)
		}
	}
	// GTK opens a chooser on Recent by default, which has no current folder
	// for a folder chooser to select. Open on the working directory instead,
	// through GIO's built-in keyfile settings backend in a private config.
	config := t.TempDir()
	settings := filepath.Join(config, "glib-2.0", "settings")
	if err := os.MkdirAll(settings, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(settings, "keyfile"), []byte("[org/gtk/settings/file-chooser]\nstartup-mode='cwd'\n"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_CONFIG_HOME", config)
	t.Setenv("GSETTINGS_BACKEND", "keyfile")
	t.Chdir(root)
	syms, err := loadPickerSymbols()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	s := New().(*linuxShell)
	result := make(chan error, 1)
	err = s.Run(ctx, desktop.WindowConfig{Title: "GoFastr picker automation", Width: 500, Height: 300}, func(desktop.Window) {
		go func() {
			defer s.Quit()
			result <- exerciseAutomatedPickers(ctx, t, s, syms, root, input, existing)
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
		t.Fatal("picker automation did not finish", ctx.Err())
	}
	for _, path := range []string{input, existing} {
		if got, err := os.ReadFile(path); err != nil || !bytes.Equal(got, contents) {
			t.Fatalf("picker fixture changed: %s (read error: %v)", path, err)
		}
	}
}

func exerciseAutomatedPickers(ctx context.Context, t *testing.T, s *linuxShell, syms pickerSymbols, root, input, existing string) error {
	dialogs := s.Dialogs()
	type answer struct {
		paths []string
		err   error
	}
	start := func(fn func() ([]string, error)) chan answer {
		ch := make(chan answer, 1)
		go func() { p, err := fn(); ch <- answer{p, err} }()
		return ch
	}
	one := func(path string, err error) ([]string, error) { return []string{path}, err }
	// pick waits for the chooser titled title, types path into its location
	// or name entry, presses Enter and returns the chooser's toplevel.
	pick := func(title, path string) (toplevel, error) {
		t.Logf("picker %q: typing %s", title, path)
		chooser, err := syms.await(ctx, s, func(w toplevel) bool { return w.title == title })
		if err != nil {
			return chooser, fmt.Errorf("chooser %q never appeared: %w", title, err)
		}
		if err := xdo(ctx, "windowfocus", strconv.FormatUint(uint64(chooser.xid), 10)); err != nil {
			return chooser, err
		}
		if err := xdo(ctx, "key", "--clearmodifiers", "ctrl+l", "ctrl+a"); err != nil {
			return chooser, err
		}
		if err := xdo(ctx, "type", "--delay", "5", path); err != nil {
			return chooser, err
		}
		// The entry loads the typed folder asynchronously; an Enter during
		// that load is dropped.
		time.Sleep(500 * time.Millisecond)
		return chooser, xdo(ctx, "key", "Return")
	}
	settle := func(ch chan answer, want string) error {
		select {
		case got := <-ch:
			if got.err != nil {
				return got.err
			}
			if len(got.paths) != 1 || got.paths[0] != want {
				return fmt.Errorf("picker returned %q, want only %q", got.paths, want)
			}
			return nil
		case <-time.After(15 * time.Second):
			windows, _ := syms.mapped(s)
			shot := saveScreen(t, s, "picker-timeout")
			return fmt.Errorf("no picker result for %q; mapped windows %+v; screen %s", want, windows, shot)
		}
	}

	opened := start(func() ([]string, error) {
		return dialogs.OpenFile(ctx, desktop.OpenOptions{Filters: []desktop.FileFilter{{Name: "Text files", Extensions: []string{"txt"}}}})
	})
	if _, err := pick(dialogOpenFile.Text(ctx), input); err != nil {
		return err
	}
	if err := settle(opened, input); err != nil {
		return fmt.Errorf("open file: %w", err)
	}

	folder := start(func() ([]string, error) { return one(dialogs.OpenFolder(ctx)) })
	// The typed trailing slash keeps GTK's inline folder completion from
	// taking the Enter.
	if _, err := pick(dialogOpenFolder.Text(ctx), root+"/"); err != nil {
		return err
	}
	if err := settle(folder, root); err != nil {
		return fmt.Errorf("open folder: %w", err)
	}

	newPath := filepath.Join(root, "new.txt")
	saved := start(func() ([]string, error) {
		return one(dialogs.SaveFile(ctx, desktop.SaveOptions{DefaultName: "new.txt"}))
	})
	if _, err := pick(dialogSaveFile.Text(ctx), newPath); err != nil {
		return err
	}
	if err := settle(saved, newPath); err != nil {
		return fmt.Errorf("save new: %w", err)
	}
	if _, err := os.Lstat(newPath); !os.IsNotExist(err) {
		return fmt.Errorf("save created the destination or it cannot be checked: %v", err)
	}

	overwrite := start(func() ([]string, error) {
		return one(dialogs.SaveFile(ctx, desktop.SaveOptions{DefaultName: "overwrite.txt"}))
	})
	chooser, err := pick(dialogSaveFile.Text(ctx), existing)
	if err != nil {
		return err
	}
	isConfirm := func(w toplevel) bool { return w.handle != chooser.handle && w.title != s.cfg.Title }
	confirm, err := syms.await(ctx, s, isConfirm)
	if err != nil {
		return fmt.Errorf("no overwrite confirmation for an existing file: %w", err)
	}
	if err := xdo(ctx, "windowfocus", strconv.FormatUint(uint64(confirm.xid), 10)); err != nil {
		return err
	}
	if err := xdo(ctx, "key", "Escape"); err != nil {
		return err
	}
	if err := syms.awaitGone(ctx, s, confirm.handle); err != nil {
		return fmt.Errorf("overwrite confirmation stayed after Cancel: %w", err)
	}
	select {
	case got := <-overwrite:
		return fmt.Errorf("cancelling the overwrite confirmation ended the picker: %q, %v", got.paths, got.err)
	case <-time.After(500 * time.Millisecond):
	}
	if windows, err := syms.mapped(s); err != nil || !containsHandle(windows, chooser.handle) {
		return fmt.Errorf("chooser closed with the overwrite confirmation (err %v)", err)
	}
	t.Log("overwrite confirmation Cancel kept the chooser open with no result")
	if err := xdo(ctx, "windowfocus", strconv.FormatUint(uint64(chooser.xid), 10)); err != nil {
		return err
	}
	if err := xdo(ctx, "key", "Return"); err != nil {
		return err
	}
	confirm, err = syms.await(ctx, s, isConfirm)
	if err != nil {
		return fmt.Errorf("no second overwrite confirmation: %w", err)
	}
	if err := xdo(ctx, "windowfocus", strconv.FormatUint(uint64(confirm.xid), 10)); err != nil {
		return err
	}
	if err := xdo(ctx, "key", "--clearmodifiers", "alt+r"); err != nil { // _Replace
		return err
	}
	if err := settle(overwrite, existing); err != nil {
		return fmt.Errorf("save over existing: %w", err)
	}
	return nil
}

func xdo(ctx context.Context, args ...string) error {
	if out, err := exec.CommandContext(ctx, "xdotool", args...).CombinedOutput(); err != nil {
		return fmt.Errorf("xdotool %s: %v %s", strings.Join(args, " "), err, out)
	}
	return nil
}

type toplevel struct {
	handle, xid uintptr
	title       string
}

func containsHandle(windows []toplevel, handle uintptr) bool {
	for _, w := range windows {
		if w.handle == handle {
			return true
		}
	}
	return false
}

type pickerSymbols struct{ listToplevels, listFree, mappedFn, title, xid uintptr }

// Test-only entry points, kept out of the production symbol table.
func loadPickerSymbols() (pickerSymbols, error) {
	var out pickerSymbols
	libs := map[string]uintptr{}
	for _, name := range []string{"libgtk-3.so.0", "libgdk-3.so.0", "libglib-2.0.so.0"} {
		lib, err := gtk.DlopenGlobal(name)
		if err != nil {
			return out, err
		}
		libs[name] = lib
	}
	for _, s := range []struct {
		lib, name string
		target    *uintptr
	}{
		{"libgtk-3.so.0", "gtk_window_list_toplevels", &out.listToplevels},
		{"libgtk-3.so.0", "gtk_widget_get_mapped", &out.mappedFn},
		{"libgtk-3.so.0", "gtk_window_get_title", &out.title},
		{"libgdk-3.so.0", "gdk_x11_window_get_xid", &out.xid},
		{"libglib-2.0.so.0", "g_list_free", &out.listFree},
	} {
		addr, err := gtk.Dlsym(libs[s.lib], s.name)
		if err != nil {
			return out, err
		}
		*s.target = addr
	}
	return out, nil
}

// mapped lists the process's mapped toplevel windows, read on the UI thread.
func (p pickerSymbols) mapped(s *linuxShell) ([]toplevel, error) {
	var out []toplevel
	err := s.Main(func() {
		type glist struct{ data, next, prev uintptr }
		list := call(p.listToplevels)
		for node := list; node != 0; {
			item := *(*glist)(capsNativePointer(node))
			if item.data != 0 && call(p.mappedFn, item.data)&0xff != 0 {
				w := toplevel{handle: item.data, title: capsGoString(call(p.title, item.data))}
				if gdkWindow := call(gtk.Sym.GtkWidgetGetWindow, item.data); gdkWindow != 0 {
					w.xid = call(p.xid, gdkWindow)
				}
				out = append(out, w)
			}
			node = item.next
		}
		call(p.listFree, list)
	})
	return out, err
}

func (p pickerSymbols) await(ctx context.Context, s *linuxShell, match func(toplevel) bool) (toplevel, error) {
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) && ctx.Err() == nil {
		windows, err := p.mapped(s)
		if err != nil {
			return toplevel{}, err
		}
		for _, w := range windows {
			if w.xid != 0 && match(w) {
				// Mapped is not yet viewable to the X server; give it a frame.
				time.Sleep(100 * time.Millisecond)
				return w, nil
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	return toplevel{}, errors.New("timed out")
}

func (p pickerSymbols) awaitGone(ctx context.Context, s *linuxShell, handle uintptr) error {
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) && ctx.Err() == nil {
		windows, err := p.mapped(s)
		if err != nil {
			return err
		}
		if !containsHandle(windows, handle) {
			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	return errors.New("timed out")
}

// saveScreen writes the X root window to a PNG under GOFASTR_LINUX_TEST_OUTPUT
// (or the test's temp dir) so a stalled picker step can be read as pixels.
func saveScreen(t *testing.T, s *linuxShell, name string) string {
	px, err := loadPixelSymbols()
	if err != nil {
		return err.Error()
	}
	pixbufLib, err := gtk.DlopenGlobal("libgdk_pixbuf-2.0.so.0")
	if err != nil {
		return err.Error()
	}
	savev, err := gtk.Dlsym(pixbufLib, "gdk_pixbuf_savev")
	if err != nil {
		return err.Error()
	}
	dir := os.Getenv("GOFASTR_LINUX_TEST_OUTPUT")
	if dir == "" {
		dir = t.TempDir()
	}
	path := filepath.Join(dir, name+".png")
	err = s.Main(func() {
		root := call(px.rootWindow)
		pixbuf := call(px.fromWindow, root, 0, 0, 1280, 800)
		if pixbuf == 0 {
			return
		}
		defer call(gtk.Sym.GObjectUnref, pixbuf)
		withString(path, func(file uintptr) {
			withString("png", func(kind uintptr) { call(savev, pixbuf, file, kind, 0, 0, 0) })
		})
	})
	if err != nil {
		return err.Error()
	}
	return path
}
