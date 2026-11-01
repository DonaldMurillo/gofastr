//go:build linux && (amd64 || arm64)

package linux

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/battery/desktop"
)

// This is operator-driven QA on a real desktop session and its own GTK
// theme, portal and window manager. CI runs the same sequence unattended
// under Xvfb in TestNativePickersAutomated. Run this one on its own so the
// operator sees one picker sequence at a time.
// No UI automation or production behavior is substituted for the native picker.
func TestNativeInteractivePickers(t *testing.T) {
	if !interactivePickersEnabled(os.Getenv("GOFASTR_LINUX_NATIVE"), os.Getenv("GOFASTR_LINUX_INTERACTIVE")) {
		t.Skip("manual QA: set GOFASTR_LINUX_NATIVE=1 and GOFASTR_LINUX_INTERACTIVE=1 in a graphical session")
	}
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
	t.Logf("MANUAL QA: use only the disposable fixtures in %q; all four steps must finish within five minutes", root)
	t.Log("For path entry, GTK's chooser normally supports Ctrl+L. Do not close the parent window between steps.")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	shell := New()
	result := make(chan error, 1)
	err := shell.Run(ctx, desktop.WindowConfig{Title: "GoFastr interactive picker QA", Width: 500, Height: 300}, func(desktop.Window) {
		go func() {
			defer shell.Quit()
			result <- exerciseInteractivePickers(ctx, t.Logf, shell.Dialogs(), root, input, existing)
		}()
	})
	// If the operator closes the parent early, release any pending picker too.
	cancel()
	if err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-result:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("shell exited before the interactive picker sequence completed")
	}
	for _, path := range []string{input, existing} {
		got, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(got, contents) {
			t.Fatalf("picker fixture changed: %s (read error: %v)", path, err)
		}
	}
	t.Log("PASS: exact file/folder/destination paths returned; existing fixtures unchanged; new destination not created")
	t.Log("Record the visible overwrite confirmation and Cancel→retry behavior separately: final-path assertions cannot prove those operator observations.")
}

func interactivePickersEnabled(native, interactive string) bool {
	return native == "1" && interactive == "1"
}

func TestInteractivePickerOptIn(t *testing.T) {
	for _, native := range []string{"", "0", "1", "true"} {
		for _, interactive := range []string{"", "0", "1", "true"} {
			want := native == "1" && interactive == "1"
			if got := interactivePickersEnabled(native, interactive); got != want {
				t.Fatalf("native=%q interactive=%q: enabled=%v, want %v", native, interactive, got, want)
			}
		}
	}
}

func exerciseInteractivePickers(ctx context.Context, logf func(string, ...any), dialogs desktop.Dialogs, root, input, existing string) error {
	logf("STEP 1/4: Open file. Select %q with the Text files filter.", input)
	paths, err := dialogs.OpenFile(ctx, desktop.OpenOptions{Filters: []desktop.FileFilter{{Name: "Text files", Extensions: []string{"txt"}}}})
	if err != nil {
		return fmt.Errorf("open file: %w", err)
	}
	if len(paths) != 1 || paths[0] != input {
		return fmt.Errorf("open file returned %q, want only %q", paths, input)
	}
	logf("STEP 2/4: Open folder. Select %q itself.", root)
	path, err := dialogs.OpenFolder(ctx)
	if err != nil {
		return fmt.Errorf("open folder: %w", err)
	}
	if path != root {
		return fmt.Errorf("open folder returned %q, want %q", path, root)
	}
	newPath := filepath.Join(root, "new.txt")
	logf("STEP 3/4: Save file. Select destination %q; the picker must not create the file.", newPath)
	path, err = dialogs.SaveFile(ctx, desktop.SaveOptions{DefaultName: "new.txt"})
	if err != nil {
		return fmt.Errorf("new destination: %w", err)
	}
	if path != newPath {
		return fmt.Errorf("new destination returned %q, want %q", path, newPath)
	}
	if _, err := os.Lstat(newPath); !os.IsNotExist(err) {
		return fmt.Errorf("new destination was created or cannot be checked: %v", err)
	}
	logf("STEP 4/4: Save file to existing %q. On the overwrite confirmation, choose Cancel (not the main chooser's Cancel).", existing)
	logf("MANUAL CHECK: confirmation closes, chooser remains, and no result is returned. Click Save again; require a second overwrite confirmation, then choose Replace.")
	path, err = dialogs.SaveFile(ctx, desktop.SaveOptions{DefaultName: "overwrite.txt"})
	if err != nil {
		return fmt.Errorf("overwrite destination: %w", err)
	}
	if path != existing {
		return fmt.Errorf("overwrite destination returned %q, want %q", path, existing)
	}
	return nil
}
