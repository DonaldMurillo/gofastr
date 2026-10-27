package desktopcmd

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"debug/elf"
	"encoding/hex"
	"encoding/json"
	"image"
	"image/png"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
)

func TestDesktopBuildRejectsUnsupportedFormat(t *testing.T) {
	for _, test := range []struct{ target, format string }{{"linux", "deb"}, {"linux", ""}, {"darwin", "tar.gz"}, {"windows", "tar.gz"}, {"darwin", "executable"}} {
		t.Run(test.target, func(t *testing.T) {
			output := filepath.Join(t.TempDir(), "not-created")
			var code int
			text := clitest.CovT_capStdout(t, func() {
				code = clitest.CovT_capExit(t, func() {
					runDesktopBuildForOS([]string{"--id=dev.example.notes", "--pkg=./missing", "--format=" + test.format, "-o=" + output}, test.target)
				})
			})
			if code != 1 || !strings.Contains(text, "--format") {
				t.Fatalf("unsupported format not refused: exit=%d output=%s", code, text)
			}
			if _, err := os.Stat(output); !os.IsNotExist(err) {
				t.Fatalf("output changed before format validation: %v", err)
			}
		})
	}
}

func linuxArchiveFixture(t *testing.T) (desktopBuildFlags, string, []linuxArchiveEntry) {
	t.Helper()
	dir := t.TempDir()
	exe := filepath.Join(dir, "app")
	if err := os.WriteFile(exe, []byte("#!/bin/sh\nprintf '%s\\000' \"$@\"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	// The id names bin/<id>, which the launcher test executes. macOS kills
	// an exec of a file named *.app outside a bundle, so the id must not
	// end in .app.
	f := desktopBuildFlags{id: "dev.example.notes", version: "1.2.3", format: "tar.gz"}
	icon := testLinuxPNG(t)
	entries, err := linuxBundleEntries(f, ` Notes 100% "$HOME" & Co `, "amd64", exe, icon)
	if err != nil {
		t.Fatal(err)
	}
	return f, exe, entries
}
func testLinuxPNG(t *testing.T) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := png.Encode(&b, image.NewNRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}
func decodeLinuxArchive(t *testing.T, data []byte) ([]*tar.Header, map[string][]byte) {
	t.Helper()
	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	defer gz.Close()
	if gz.Name != "" || gz.Comment != "" || !gz.ModTime.IsZero() {
		t.Fatalf("gzip carries unstable metadata: %+v", gz.Header)
	}
	tr := tar.NewReader(gz)
	var headers []*tar.Header
	contents := map[string][]byte{}
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(tr)
		if err != nil {
			t.Fatal(err)
		}
		headers = append(headers, h)
		contents[h.Name] = data
	}
	return headers, contents
}
func TestLinuxArchiveDeterministic(t *testing.T) {
	f, exe, entries := linuxArchiveFixture(t)
	root, err := linuxBundleRoot(f, "Notes", "amd64")
	if err != nil {
		t.Fatal(err)
	}
	var first, second bytes.Buffer
	if err = writeLinuxArchive(&first, root, entries); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-24 * time.Hour)
	if err = os.Chtimes(exe, old, old); err != nil {
		t.Fatal(err)
	}
	slices.Reverse(entries)
	if err = writeLinuxArchive(&second, root, entries); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first.Bytes(), second.Bytes()) {
		t.Fatal("same payload differs by source timestamp or input order")
	}
	headers, contents := decodeLinuxArchive(t, first.Bytes())
	want := []string{"README.txt", "bin/dev.example.notes", "dev.example.notes.desktop.in", "icon.png", "launch.sh", "manifest.json"}
	var got []string
	for _, h := range headers {
		rel := strings.TrimPrefix(h.Name, root+"/")
		got = append(got, rel)
		if h.Typeflag != tar.TypeReg || h.Uid != 0 || h.Gid != 0 || h.Uname != "" || h.Gname != "" || !h.ModTime.Equal(time.Unix(0, 0)) || !h.AccessTime.IsZero() || !h.ChangeTime.IsZero() {
			t.Errorf("non-deterministic or unexpected metadata: %+v", h)
		}
		mode := int64(0644)
		if rel == "launch.sh" || rel == "bin/dev.example.notes" {
			mode = 0755
		}
		if h.Mode != mode {
			t.Errorf("%s mode %o, want %o", rel, h.Mode, mode)
		}
	}
	if !slices.Equal(got, want) {
		t.Fatalf("archive payload %v, want exactly %v (no shared libraries)", got, want)
	}
	var manifest linuxBundleManifest
	if err = json.Unmarshal(contents[root+"/manifest.json"], &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.SchemaVersion != 1 || manifest.Platform != "linux-amd64" || manifest.EntryPoint != "launch.sh" || manifest.ID != f.id || manifest.Version != f.version || len(manifest.Files) != 5 {
		t.Fatalf("manifest: %+v", manifest)
	}
	for _, entry := range manifest.Files {
		data, ok := contents[root+"/"+entry.Path]
		sum := sha256.Sum256(data)
		if !ok || entry.Size != int64(len(data)) || entry.SHA256 != hex.EncodeToString(sum[:]) {
			t.Errorf("manifest mismatch: %+v", entry)
		}
	}
	desktop := string(contents[root+"/dev.example.notes.desktop.in"])
	if !strings.Contains(desktop, `Name=\sNotes\s100%\s"$HOME"\s&\sCo\s`+"\n") || !strings.Contains(desktop, "Exec=\"@BUNDLE_ROOT@/launch.sh\"\n") {
		t.Fatalf("desktop escaping: %s", desktop)
	}
	for _, forbidden := range []string{"MimeType=", "DBusActivatable=", "%u", "%U"} {
		if strings.Contains(desktop, forbidden) {
			t.Errorf("template claims unsupported activation: %q", forbidden)
		}
	}
}
func TestLinuxArchiveRejectsPaths(t *testing.T) {
	for _, name := range []string{"../escape", "/absolute", "a/../../escape", "a\\b", "a//b", ".", "a/./b"} {
		t.Run(name, func(t *testing.T) {
			var b bytes.Buffer
			if err := writeLinuxArchive(&b, "bundle", []linuxArchiveEntry{{name: name, mode: 0644, data: []byte("x")}}); err == nil {
				t.Fatal("unsafe entry accepted")
			}
			if b.Len() != 0 {
				t.Fatal("unsafe path wrote archive bytes")
			}
		})
	}
	for _, root := range []string{"../escape", "/absolute", "a/b", "a\\b", "."} {
		var b bytes.Buffer
		if err := writeLinuxArchive(&b, root, nil); err == nil {
			t.Errorf("unsafe root %q accepted", root)
		}
	}
	for _, entries := range [][]linuxArchiveEntry{{{name: "file", mode: 0777}}, {{name: "same", mode: 0644}, {name: "same", mode: 0644}}} {
		var b bytes.Buffer
		if err := writeLinuxArchive(&b, "bundle", entries); err == nil {
			t.Fatal("duplicate/mode accepted")
		}
	}
}
func TestLinuxArchiveRejectsSymlinkSource(t *testing.T) {
	_, exe, entries := linuxArchiveFixture(t)
	link := filepath.Join(filepath.Dir(exe), "link")
	if err := os.Symlink(exe, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	for i := range entries {
		if entries[i].source != "" {
			entries[i].source = link
		}
	}
	var b bytes.Buffer
	if err := writeLinuxArchive(&b, "bundle", entries); err == nil {
		t.Fatal("symlink source archived")
	}
}
func TestLinuxArchiveMetadataValidation(t *testing.T) {
	f := desktopBuildFlags{id: "dev.example.notes", version: "1.2.3"}
	for _, version := range []string{"", "../../escape", "1\nExec=bad", "1/2", "-1", strings.Repeat("1", 65)} {
		bad := f
		bad.version = version
		if _, err := linuxBundleRoot(bad, "Notes", "amd64"); err == nil {
			t.Errorf("version %q accepted", version)
		}
	}
	for _, id := range []string{"../escape", "a/b", "a\\b", "...", strings.Repeat("a", 250) + ".b"} {
		bad := f
		bad.id = id
		if _, err := linuxBundleRoot(bad, "Notes", "amd64"); err == nil {
			t.Errorf("ID %q accepted", id)
		}
	}
	for _, name := range []string{"a/b", "a\\b", "line\nbreak", "bad\xff"} {
		if _, err := linuxBundleRoot(f, name, "amd64"); err == nil {
			t.Errorf("name %q accepted", name)
		}
	}
	if got := escapeDesktopEntryString(" a\\b\t\n\r "); got != `\sa\\b\t\n\r\s` {
		t.Fatalf("desktop string escaping %q", got)
	}
}
func TestLinuxArchiveLauncherQuotes(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX launcher executes on Linux, not Windows")
	}
	_, _, entries := linuxArchiveFixture(t)
	// Percent, shell metacharacters, quotes, backslashes and a newline in the
	// extraction directory cannot become shell syntax or corrupt the path.
	root := filepath.Join(t.TempDir(), "space % '$HOME' `false` \\\n")
	for _, entry := range entries {
		name := filepath.Join(root, filepath.FromSlash(entry.name))
		if err := os.MkdirAll(filepath.Dir(name), 0700); err != nil {
			t.Fatal(err)
		}
		var b bytes.Buffer
		if _, err := copyLinuxArchiveEntry(&b, entry); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(name, b.Bytes(), os.FileMode(entry.mode)); err != nil {
			t.Fatal(err)
		}
	}
	args := []string{"hello world", `$(exit 7); "$HOME" %u`, "line\nbreak", ""}
	want := []byte(strings.Join(args, "\x00") + "\x00")
	for _, invoke := range []string{filepath.Join(root, "launch.sh"), "./launch.sh", "launch.sh"} {
		command := invoke
		if invoke == "launch.sh" {
			command = "/bin/sh"
		}
		var cmd *exec.Cmd
		if invoke == "launch.sh" {
			cmd = exec.Command(command, append([]string{"-c", `exec launch.sh "$@"`, "probe"}, args...)...)
			cmd.Env = append(os.Environ(), "PATH="+root+":"+os.Getenv("PATH"))
		} else {
			cmd = exec.Command(command, args...)
		}
		cmd.Dir = root
		got, err := cmd.Output()
		if err != nil {
			t.Fatalf("launcher %q: %v", invoke, err)
		}
		if !bytes.Equal(got, want) {
			t.Fatalf("launcher %q arguments: got %q, want %q", invoke, got, want)
		}
	}
}
func TestLinuxArchiveIconValidation(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "icon.png")
	if err := os.WriteFile(file, []byte("not png"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := linuxBundleIcon(file); err == nil {
		t.Fatal("non-PNG icon accepted")
	}
	data := testLinuxPNG(t)
	if err := os.WriteFile(file, data, 0600); err != nil {
		t.Fatal(err)
	}
	got, err := linuxBundleIcon(file)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, data) {
		t.Fatal("valid PNG unexpectedly changed")
	}
	if _, err = linuxBundleIcon(dir); err == nil {
		t.Fatal("directory accepted as icon")
	}
}
func TestLinuxArchiveInvalidInputBeforeOutput(t *testing.T) {
	t.Setenv("GOARCH", "amd64")
	for _, test := range []struct {
		name   string
		mutate func(*desktopBuildFlags)
	}{
		{"version", func(f *desktopBuildFlags) { f.version = "../../escape" }},
		{"icon", func(f *desktopBuildFlags) { f.icon = "missing.png" }},
		{"invalid scheme", func(f *desktopBuildFlags) { f.scheme = "Invalid Scheme" }},
		{"sign", func(f *desktopBuildFlags) { f.sign = "identity" }},
	} {
		t.Run(test.name, func(t *testing.T) {
			f := desktopBuildFlags{format: "tar.gz", id: "dev.example.notes", version: "1.0", pkg: "./missing", out: filepath.Join(t.TempDir(), "not-created")}
			test.mutate(&f)
			if err := buildLinuxDesktop(f, "Notes"); err == nil {
				t.Fatal("invalid input accepted")
			}
			if _, err := os.Stat(f.out); !os.IsNotExist(err) {
				t.Fatalf("invalid input mutated output: %v", err)
			}
		})
	}
}
func TestDesktopBuildLinuxArchive(t *testing.T) {
	fixture := t.TempDir()
	desktopFixtureApp(t, fixture)
	t.Chdir(fixture)
	t.Setenv("GOARCH", "amd64")
	out := filepath.Join(fixture, "dist")
	var code int
	clitest.CovT_capStdout(t, func() {
		code = clitest.CovT_capExit(t, func() {
			runDesktopBuildForOS([]string{"--id=dev.example.archive", "--name=Quotes \"$HOME\" 100%", "--version=1.2.3", "--format=tar.gz", "-o=" + out}, "linux")
		})
	})
	if code != -1 {
		t.Fatalf("build exit %d", code)
	}
	root := "dev.example.archive-1.2.3-linux-amd64"
	data, err := os.ReadFile(filepath.Join(out, root+".tar.gz"))
	if err != nil {
		t.Fatal(err)
	}
	headers, contents := decodeLinuxArchive(t, data)
	binary, err := elf.NewFile(bytes.NewReader(contents[root+"/bin/dev.example.archive"]))
	if err != nil {
		t.Fatal(err)
	}
	defer binary.Close()
	if binary.Machine != elf.EM_X86_64 {
		t.Fatalf("machine %s", binary.Machine)
	}
	for _, p := range binary.Progs {
		if p.Type == elf.PT_INTERP {
			t.Fatal("fixture binary unexpectedly links a C runtime")
		}
	}
	files, err := os.ReadDir(out)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 {
		t.Fatalf("staging artifacts remain: %v", files)
	}
	if runtime.GOOS == "linux" && runtime.GOARCH == "amd64" {
		extracted := filepath.Join(fixture, "unpacked ' $ %")
		for _, header := range headers {
			dest := filepath.Join(extracted, filepath.FromSlash(header.Name))
			if err = os.MkdirAll(filepath.Dir(dest), 0700); err != nil {
				t.Fatal(err)
			}
			if err = os.WriteFile(dest, contents[header.Name], os.FileMode(header.Mode)); err != nil {
				t.Fatal(err)
			}
		}
		got, err := exec.Command(filepath.Join(extracted, root, "launch.sh")).Output()
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != "desktop fixture\n" {
			t.Fatalf("launcher output %q", got)
		}
	}
	// No output executable name is derived from the display name.
	for _, h := range headers {
		if path.Clean(h.Name) != h.Name || strings.Contains(h.Name, "$HOME") {
			t.Fatalf("unsafe archive name %q", h.Name)
		}
	}
}

func TestLinuxArchiveDestinationSymlink(t *testing.T) {
	t.Setenv("GOARCH", "amd64")
	f := desktopBuildFlags{format: "tar.gz", id: "dev.example.notes", version: "1.0", pkg: "./missing", out: t.TempDir()}
	target := filepath.Join(t.TempDir(), "unrelated")
	if err := os.WriteFile(target, []byte("unchanged"), 0600); err != nil {
		t.Fatal(err)
	}
	root, err := linuxBundleRoot(f, "Notes", "amd64")
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Symlink(target, filepath.Join(f.out, root+".tar.gz")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if err = buildLinuxDesktop(f, "Notes"); err == nil || !strings.Contains(err.Error(), "non-regular") {
		t.Fatalf("symlink destination accepted: %v", err)
	}
	got, err := os.ReadFile(target)
	if err != nil || string(got) != "unchanged" {
		t.Fatalf("symlink target changed: %q %v", got, err)
	}
	files, err := os.ReadDir(f.out)
	if err != nil || len(files) != 1 {
		t.Fatalf("staging files remain: %v %v", files, err)
	}
}

func TestLinuxArchiveBuildFailurePreservesRelease(t *testing.T) {
	t.Setenv("GOARCH", "amd64")
	f := desktopBuildFlags{format: "tar.gz", id: "dev.example.notes", version: "1.0", pkg: "./missing", out: t.TempDir()}
	root, err := linuxBundleRoot(f, "Notes", "amd64")
	if err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(f.out, root+".tar.gz")
	if err = os.WriteFile(dest, []byte("previous release"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = buildLinuxDesktop(f, "Notes"); err == nil {
		t.Fatal("missing source compiled")
	}
	got, err := os.ReadFile(dest)
	if err != nil || string(got) != "previous release" {
		t.Fatalf("release changed after failed build: %q %v", got, err)
	}
	files, err := os.ReadDir(f.out)
	if err != nil || len(files) != 1 {
		t.Fatalf("staging files remain: %v %v", files, err)
	}
}
