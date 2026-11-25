package update

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/internal/linuxbundle/linuxbundletest"
)

type tarEntry struct {
	hdr  tar.Header
	body string
}

func tarGz(t *testing.T, entries ...tarEntry) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, e := range entries {
		hdr := e.hdr
		if hdr.Typeflag == tar.TypeReg && hdr.Size == 0 {
			hdr.Size = int64(len(e.body))
		}
		if err := tw.WriteHeader(&hdr); err != nil {
			t.Fatal(err)
		}
		if hdr.Size > int64(len(e.body)) {
			// A header declaring more than the body: the archive ends here,
			// as a truncated download would.
			_ = tw.Flush()
			break
		}
		if e.body != "" {
			if _, err := tw.Write([]byte(e.body)); err != nil {
				t.Fatal(err)
			}
		}
	}
	_ = tw.Close()
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func reg(name string, mode int64, body string) tarEntry {
	return tarEntry{tar.Header{Name: name, Typeflag: tar.TypeReg, Mode: mode}, body}
}

func TestExtractTarGzWritesOneRoot(t *testing.T) {
	dest := t.TempDir()
	root, err := ExtractTarGz(tarGz(t,
		reg("app-1.0.0/bin/app", 0o4777, "exe"),
		reg("app-1.0.0/README.txt", 0o666, "readme"),
		tarEntry{tar.Header{Name: "app-1.0.0/docs/", Typeflag: tar.TypeDir, Mode: 0o777}, ""},
	), dest)
	if err != nil {
		t.Fatal(err)
	}
	if root != filepath.Join(dest, "app-1.0.0") {
		t.Fatalf("root = %q", root)
	}
	for name, want := range map[string]os.FileMode{"bin/app": 0o755, "README.txt": 0o644, "docs": 0o755 | os.ModeDir} {
		st, err := os.Lstat(filepath.Join(root, name))
		if err != nil {
			t.Fatal(err)
		}
		if got := st.Mode() & (os.ModePerm | os.ModeDir | os.ModeSetuid | os.ModeSetgid | os.ModeSticky); got != want {
			t.Errorf("%s mode = %v, want %v", name, got, want)
		}
	}
}

func TestExtractTarGzRefusesUnsafeArchives(t *testing.T) {
	cases := map[string][]byte{
		"a symlink": tarGz(t, reg("app/a", 0o644, "x"),
			tarEntry{tar.Header{Name: "app/link", Typeflag: tar.TypeSymlink, Linkname: "/etc/passwd"}, ""}),
		"a hard link": tarGz(t, reg("app/a", 0o644, "x"),
			tarEntry{tar.Header{Name: "app/link", Typeflag: tar.TypeLink, Linkname: "app/a"}, ""}),
		"a FIFO":              tarGz(t, tarEntry{tar.Header{Name: "app/fifo", Typeflag: tar.TypeFifo}, ""}),
		"a parent segment":    tarGz(t, reg("app/../escape", 0o644, "x")),
		"an absolute name":    tarGz(t, reg("/app/a", 0o644, "x")),
		"a backslash":         tarGz(t, reg(`app\a`, 0o644, "x")),
		"two top-level names": tarGz(t, reg("app/a", 0o644, "x"), reg("other/b", 0o644, "y")),
		"a repeated entry":    tarGz(t, reg("app/a", 0o644, "x"), reg("app/a", 0o644, "y")),
		"a file as the root":  tarGz(t, reg("app", 0o644, "x")),
		"an empty archive":    tarGz(t),
		"not gzip":            []byte("plain bytes"),
		"an oversized entry":  tarGz(t, tarEntry{tar.Header{Name: "app/big", Typeflag: tar.TypeReg, Size: maxEntrySize + 1}, ""}),
	}
	for name, data := range cases {
		t.Run(name, func(t *testing.T) {
			dest := t.TempDir()
			if _, err := ExtractTarGz(data, dest); !errors.Is(err, ErrBadArchive) {
				t.Fatalf("ExtractTarGz = %v, want ErrBadArchive", err)
			}
			if _, err := os.Lstat(filepath.Join(filepath.Dir(dest), "escape")); err == nil {
				t.Fatal("an entry was written outside dest")
			}
		})
	}
}

func writeLinuxArchive(t *testing.T, dir string, spec linuxbundletest.Spec) string {
	t.Helper()
	p := filepath.Join(dir, "bundle.tar.gz")
	if err := os.WriteFile(p, linuxbundletest.TarGz(t, spec, linuxbundletest.Entries(spec)), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestWriteFeedChecksALinuxArchive(t *testing.T) {
	spec := linuxbundletest.Default("1.2.0")
	spec.Arch = "amd64"
	dir := t.TempDir()
	keyPath, _, _, _ := writeKeyFile(t, dir)
	archive := writeLinuxArchive(t, dir, spec)
	url := "https://example.com/notes.tar.gz"
	if err := WriteFeed(filepath.Join(dir, "ok"), keyPath, "1.2.0", "", "linux-amd64", archive, url, 0); err != nil {
		t.Fatalf("a matching Linux archive was refused: %v", err)
	}
	for name, run := range map[string]func() error{
		"another version": func() error {
			return WriteFeed(filepath.Join(dir, "v"), keyPath, "1.3.0", "", "linux-amd64", archive, url, 0)
		},
		"another platform": func() error {
			return WriteFeed(filepath.Join(dir, "p"), keyPath, "1.2.0", "", "linux-arm64", archive, url, 0)
		},
		"a zip": func() error {
			return WriteFeed(filepath.Join(dir, "z"), keyPath, "1.2.0", "", "linux-amd64", writeArchive(t, dir), url, 0)
		},
	} {
		t.Run(name, func(t *testing.T) {
			err := run()
			if err == nil {
				t.Fatal("WriteFeed accepted it")
			}
			if strings.Contains(err.Error(), dir) {
				t.Fatalf("the error names a path: %v", err)
			}
		})
	}
}
