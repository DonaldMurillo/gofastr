package sdk

import (
	"archive/zip"
	"bytes"
	"strings"
	"testing"
)

// Property: every entry name PackZip emits stays inside the extraction
// directory. The per-file Path guard already rejects "", leading "/", and
// ".."; the PREFIX the caller contributes is composed into the same entry
// names ("prefix + "/" + path") but is not validated at all. The generator
// derives the prefix from the app name (spec.App + "-sdk"), which comes
// from --name or the project's gofastr.codegen.yml — a file an attacker
// can edit without touching either the generator or the app source — so a
// "../" there turns every entry of the published, publicly downloadable
// archive into zip-slip.
func TestPackZipPrefixCannotEscapeDir(t *testing.T) {
	files := []File{{Path: "go.mod", Data: []byte("module x\n")}}
	for _, prefix := range []string{"../evil-sdk", "a/../../evil-sdk", "/abs-root-sdk", "..", "app-sdk/.."} {
		raw, err := PackZip(prefix, files)
		if err != nil {
			// Rejecting the prefix outright is an acceptable fix shape.
			continue
		}
		for _, esc := range escapingZipEntries(t, raw) {
			t.Errorf("prefix %q produced zip entry %q that extracts outside the extraction directory", prefix, esc)
		}
	}

	// Happy path: a clean prefix must keep packing, the guard cannot
	// become a blanket refusal.
	raw, err := PackZip("app-sdk", files)
	if err != nil {
		t.Fatalf("clean prefix rejected: %v", err)
	}
	if esc := escapingZipEntries(t, raw); len(esc) != 0 {
		t.Fatalf("clean prefix produced escaping entries %v", esc)
	}
}

// escapingZipEntries lists entry names that resolve outside the directory
// the archive is extracted into: absolute names, parent traversal, or any
// ".." segment.
func escapingZipEntries(t *testing.T, raw []byte) []string {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		t.Fatalf("packed bytes are not a readable zip: %v", err)
	}
	var out []string
	for _, f := range zr.File {
		name := strings.TrimPrefix(f.Name, "./")
		if strings.HasPrefix(name, "/") {
			out = append(out, f.Name)
			continue
		}
		for _, seg := range strings.Split(name, "/") {
			if seg == ".." {
				out = append(out, f.Name)
				break
			}
		}
	}
	return out
}
