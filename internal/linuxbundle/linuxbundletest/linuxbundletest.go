// Package linuxbundletest builds Linux desktop bundles for tests: the
// source form `gofastr desktop build --format=tar.gz` writes, its
// archive, and the installed form `gofastr desktop install` makes.
package linuxbundletest

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"sort"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/internal/linuxbundle"
)

// Spec is one bundle's identity; Marker varies the executable's bytes.
type Spec struct {
	ID, Name, Version, Scheme, Arch, Marker string
}

// Default is a bundle for this host's architecture.
func Default(version string) Spec {
	return Spec{ID: "dev.gofastr.notes", Name: "Notes", Version: version, Arch: runtime.GOARCH, Marker: version}
}

// Entry is one archive member.
type Entry struct {
	Name string
	Mode int64
	Data []byte
}

// ELF is the smallest executable Open accepts for arch: one PT_LOAD R+X
// segment holding the entry point, then marker.
func ELF(arch, marker string) []byte {
	machine := uint16(62) // EM_X86_64
	if arch == "arm64" {
		machine = 183 // EM_AARCH64
	}
	const base, size = 0x400000, 0x80
	b := make([]byte, size, size+len(marker))
	copy(b, []byte{0x7f, 'E', 'L', 'F', 2, 1, 1, 0})
	le := binary.LittleEndian
	le.PutUint16(b[16:], 2) // ET_EXEC
	le.PutUint16(b[18:], machine)
	le.PutUint32(b[20:], 1)
	le.PutUint64(b[24:], base+0x78) // entry
	le.PutUint64(b[32:], 64)        // phoff
	le.PutUint16(b[52:], 64)        // ehsize
	le.PutUint16(b[54:], 56)        // phentsize
	le.PutUint16(b[56:], 1)         // phnum
	ph := b[64:]
	le.PutUint32(ph[0:], 1) // PT_LOAD
	le.PutUint32(ph[4:], 5) // PF_R|PF_X
	le.PutUint64(ph[16:], base)
	le.PutUint64(ph[24:], base)
	le.PutUint64(ph[32:], size)
	le.PutUint64(ph[40:], size)
	le.PutUint64(ph[48:], 0x1000)
	return append(b, marker...)
}

// PNG is a 1x1 PNG.
func PNG() []byte {
	var buf bytes.Buffer
	_ = png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 1, 1)))
	return buf.Bytes()
}

// Entries is the bundle's payload in archive order, manifest.json last.
func Entries(s Spec) []Entry {
	return EntriesWithExecutable(s, ELF(s.Arch, s.Marker))
}

// EntriesWithExecutable is Entries with exe as bin/<id>.
func EntriesWithExecutable(s Spec, exe []byte) []Entry {
	entries := []Entry{
		{"README.txt", 0o644, []byte("fixture " + s.Version + "\n")},
		{"bin/" + s.ID, 0o755, exe},
		{s.ID + ".desktop.in", 0o644, []byte("# TEMPLATE ONLY\n")},
		{"icon.png", 0o644, PNG()},
		{linuxbundle.EntryPoint, 0o755, []byte("#!/bin/sh\nexec \"${0%/*}/bin/" + s.ID + "\" \"$@\"\n")},
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })
	m := linuxbundle.Manifest{SchemaVersion: 1, ID: s.ID, Name: s.Name, Version: s.Version, Platform: "linux-" + s.Arch, EntryPoint: linuxbundle.EntryPoint, EmbeddedScheme: s.Scheme, Runtime: []string{"GTK 3"}}
	for _, e := range entries {
		sum := sha256.Sum256(e.Data)
		m.Files = append(m.Files, linuxbundle.File{Path: e.Name, Mode: fmt.Sprintf("%04o", e.Mode), Size: int64(len(e.Data)), SHA256: hex.EncodeToString(sum[:])})
	}
	raw, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		panic(err)
	}
	return append(entries, Entry{linuxbundle.ManifestName, 0o644, append(raw, '\n')})
}

// TarGz is the release archive of entries under the root directory build writes for s.
func TarGz(t testing.TB, s Spec, entries []Entry) []byte {
	t.Helper()
	root, err := linuxbundle.RootName(s.ID, s.Version, s.Arch)
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, e := range entries {
		hdr := &tar.Header{Name: path.Join(root, e.Name), Typeflag: tar.TypeReg, Mode: e.Mode, Size: int64(len(e.Data)), ModTime: time.Unix(0, 0), Format: tar.FormatPAX}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write(e.Data); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// Install writes s's installed form at apps/<id> with a receipt for
// launcher and desktopEntry, as `gofastr desktop install` lays it out,
// and returns that directory.
func Install(t testing.TB, apps string, s Spec, launcher, desktopEntry []byte) string {
	t.Helper()
	dir := filepath.Join(apps, s.ID)
	if err := os.MkdirAll(filepath.Join(dir, "bin"), 0o700); err != nil {
		t.Fatal(err)
	}
	for _, d := range []string{apps, dir, filepath.Join(dir, "bin")} {
		if err := os.Chmod(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	var raw []byte
	for _, e := range Entries(s) {
		mode := linuxbundle.InstalledMode(fmt.Sprintf("%04o", e.Mode))
		if e.Name == linuxbundle.ManifestName {
			raw, mode = e.Data, 0o600
		}
		name := filepath.Join(dir, filepath.FromSlash(e.Name))
		if err := os.WriteFile(name, e.Data, mode); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(name, mode); err != nil {
			t.Fatal(err)
		}
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	if err := linuxbundle.WriteReceipt(root, linuxbundle.NewReceipt(s.ID, raw, launcher, desktopEntry)); err != nil {
		t.Fatal(err)
	}
	return dir
}
