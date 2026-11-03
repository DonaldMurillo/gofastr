package desktop

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/battery/desktop/internal/update"
)

// The install step on a FileLayout platform (the Windows shape): two
// files beside the running executable swap as a set, and a failure
// part way puts every file back.

// layoutPlatform is fakeUpdatePlatform plus FileLayout over dir/Notes.exe
// and dir/WebView2Loader.dll.
type layoutPlatform struct {
	*fakeUpdatePlatform
}

func (p *layoutPlatform) Units() ([]update.Unit, bool) {
	return []update.Unit{
		{Rel: "Notes.exe", Current: filepath.Join(p.dir, "Notes.exe")},
		{Rel: update.WebView2LoaderName, Current: filepath.Join(p.dir, update.WebView2LoaderName)},
	}, true
}

// Inspect reads the version out of the fake executable's body
// ("exe 1.2.0") after the same closed-set check the real one runs.
func (p *layoutPlatform) Inspect(extracted string) (update.BundleInfo, error) {
	if err := update.CheckWindowsBundle(extracted, "Notes"); err != nil {
		return update.BundleInfo{}, err
	}
	body, err := os.ReadFile(filepath.Join(extracted, "Notes.exe"))
	if err != nil {
		return update.BundleInfo{}, err
	}
	return update.BundleInfo{Identifier: installTestID, ShortVersion: strings.TrimPrefix(string(body), "exe ")}, nil
}

func windowsLayoutZip(t *testing.T, version string, extra ...string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	dir := &zip.FileHeader{Name: "Notes/"}
	dir.SetMode(0o755 | os.ModeDir)
	if _, err := zw.CreateHeader(dir); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{"Notes.exe": "exe " + version, update.WebView2LoaderName: "dll " + version}
	for _, name := range extra {
		files[name] = "stray"
	}
	for _, name := range []string{"Notes.exe", update.WebView2LoaderName, "README.txt"} {
		body, ok := files[name]
		if !ok {
			continue
		}
		hdr := &zip.FileHeader{Name: "Notes/" + name, Method: zip.Deflate}
		hdr.SetMode(0o755)
		w, err := zw.CreateHeader(hdr)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// layoutFixture runs 1.0.0 from a directory holding Notes.exe and the
// loader, with a feed offering archive as 1.2.0.
func layoutFixture(t *testing.T, archive []byte) (*Battery, *layoutPlatform, string) {
	t.Helper()
	b, fp, _ := installFixture(t, "1.2.0", archive)
	dir := t.TempDir()
	for name, body := range map[string]string{"Notes.exe": "exe 1.0.0", update.WebView2LoaderName: "dll 1.0.0"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	fp.name, fp.dir = "Notes", dir
	p := &layoutPlatform{fakeUpdatePlatform: fp}
	b.updaterFor().platform = p
	return b, p, dir
}

func readIn(t *testing.T, dir, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		return "<" + err.Error() + ">"
	}
	return string(b)
}

func TestFileLayoutInstallSwapsEveryFile(t *testing.T) {
	b, p, dir := layoutFixture(t, windowsLayoutZip(t, "1.2.0"))
	if err := b.InstallUpdate(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := readIn(t, dir, "Notes.exe"); got != "exe 1.2.0" {
		t.Fatalf("Notes.exe = %q", got)
	}
	if got := readIn(t, dir, update.WebView2LoaderName); got != "dll 1.2.0" {
		t.Fatalf("loader = %q", got)
	}
	if got := readIn(t, dir, "Notes.exe.old"); got != "exe 1.0.0" {
		t.Fatalf("Notes.exe.old = %q", got)
	}
	if len(p.relaunched) != 1 || p.relaunched[0] != filepath.Join(dir, "Notes.exe") {
		t.Fatalf("relaunched %q, want the executable", p.relaunched)
	}
	if len(p.verified) != 1 {
		t.Fatalf("verified %q, want one signature check", p.verified)
	}

	// The next launch clears both leftovers.
	b.updaterFor().removeStaleOld()
	for _, name := range []string{"Notes.exe.old", update.WebView2LoaderName + ".old"} {
		if _, err := os.Lstat(filepath.Join(dir, name)); !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("%s survived the cleanup: %v", name, err)
		}
	}
}

func TestFileLayoutInstallRefusesAStrayFile(t *testing.T) {
	b, p, dir := layoutFixture(t, windowsLayoutZip(t, "1.2.0", "README.txt"))
	if err := b.InstallUpdate(context.Background()); !errors.Is(err, errUpdateArchive) {
		t.Fatalf("install = %v, want %v", err, errUpdateArchive)
	}
	if got := readIn(t, dir, "Notes.exe"); got != "exe 1.0.0" {
		t.Fatalf("Notes.exe = %q after a refused install", got)
	}
	if len(p.verified) != 0 {
		t.Fatal("the signature check ran on a refused archive")
	}
}

// A rename failure on the loader, after the executable already swapped,
// must put the old executable back: a new exe beside the old loader is
// a build nobody shipped.
func TestFileLayoutInstallRollsBackEarlierFiles(t *testing.T) {
	for _, step := range []string{"aside", "into place"} {
		t.Run(step, func(t *testing.T) {
			b, p, dir := layoutFixture(t, windowsLayoutZip(t, "1.2.0"))
			loader := filepath.Join(dir, update.WebView2LoaderName)
			b.updaterFor().rename = func(oldpath, newpath string) error {
				fail := (step == "aside" && oldpath == loader && newpath == loader+".old") ||
					(step == "into place" && oldpath == loader+".new" && newpath == loader)
				if fail {
					return &os.LinkError{Op: "rename", Old: oldpath, New: newpath, Err: fs.ErrPermission}
				}
				return os.Rename(oldpath, newpath)
			}
			if err := b.InstallUpdate(context.Background()); !errors.Is(err, errUpdateApply) {
				t.Fatalf("install = %v, want %v", err, errUpdateApply)
			}
			got := fmt.Sprintf("%s / %s", readIn(t, dir, "Notes.exe"), readIn(t, dir, update.WebView2LoaderName))
			if got != "exe 1.0.0 / dll 1.0.0" {
				t.Fatalf("after the failed swap: %s, want the running build back", got)
			}
			if len(p.relaunched) != 0 {
				t.Fatalf("relaunched %q after a failed swap", p.relaunched)
			}
		})
	}
}
