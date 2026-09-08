// Package c holds the owner-only-create arm of worldreadable: the
// 2026-09-07 inverse finding — os.WriteFile's mode argument applies
// only when the file is created, so overwriting a pre-existing looser
// file leaves it group/world-readable with the new content.
package c

import (
	"fmt"
	"os"
	"path/filepath"
)

// memoryStore reduces harness/memory/memory.go: entries and the index
// written 0o600 at caller-chosen paths under a host root.
func memoryStore(root, name, content string) error {
	if err := os.WriteFile(filepath.Join(root, name+".md"), []byte(content), 0o600); err != nil { // want `os\.WriteFile mode 0o600 applies only at create`
		return err
	}
	return os.WriteFile(filepath.Join(root, "MEMORY.md"), []byte(content), 0o600) // want `os\.WriteFile mode 0o600 applies only at create`
}

// dekExport reduces session/sqlite/dek.go: ExportDEK writes the wrapped
// key 0o600 — the pre-existing-loose overwrite is the finding.
func dekExport(outPath string, data []byte) error {
	return os.WriteFile(outPath, data, 0o600) // want `os\.WriteFile mode 0o600 applies only at create`
}

// ownerOnlyVariants: 0o400 and 0o700 are the same shape — any constant
// mode with no group/other bits.
func ownerOnlyVariants(path string, data []byte) error {
	if err := os.WriteFile(path+".ro", data, 0o400); err != nil { // want `os\.WriteFile mode 0o400 applies only at create`
		return err
	}
	return os.WriteFile(path+".bin", data, 0o700) // want `os\.WriteFile mode 0o700 applies only at create`
}

// restrictAfterStillFires: a fileperm.Restrict AFTER the write does not
// quiet the arm — the content is exposed in the window between write
// and tighten (kiln/freeze world.json's real shape).
func restrictAfterStillFires(dir string, buf []byte) error {
	path := filepath.Join(dir, "world.json")
	if err := os.WriteFile(path, append(buf, '\n'), 0o600); err != nil { // want `os\.WriteFile mode 0o600 applies only at create`
		return err
	}
	return restrict(path)
}

func restrict(path string) error {
	return os.Chmod(path, 0o600)
}

// ---------- quiet: the fresh-path postures --------------------------------

// underMkdirTemp: a path inside a dir this function minted with
// MkdirTemp cannot pre-exist.
func underMkdirTemp(name string, data []byte) (string, error) {
	dir, err := os.MkdirTemp("", "c-*")
	if err != nil {
		return "", err
	}
	return dir, os.WriteFile(filepath.Join(dir, name), data, 0o600)
}

// createTempNamed: writing through the Name() of a file this function
// minted with CreateTemp — fresh by construction.
func createTempNamed(data []byte) error {
	f, err := os.CreateTemp("", "c-*")
	if err != nil {
		return err
	}
	f.Close()
	return os.WriteFile(f.Name(), data, 0o600)
}

// ---------- quiet: the fix posture ----------------------------------------

// writeSecretFileFixed is the canonical fix (cmd/gofastr/pack.go):
// open O_CREATE|O_TRUNC 0o600, Chmod the HANDLE before the write — the
// mode travels with the write, and a pre-existing looser file is
// tightened before any content lands. No os.WriteFile site exists.
func writeSecretFileFixed(path, content string) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	if err := f.Chmod(0o600); err != nil {
		f.Close()
		return err
	}
	_, werr := f.Write([]byte(content))
	f.Close()
	return werr
}

// throughHelper: a caller of the owner-only helper — quiet.
func throughHelper(root, name, content string) error {
	return writeSecretFileFixed(filepath.Join(root, name), content)
}

// writeOwnerOnlyNamed stands in for internal/fileperm.WriteOwnerOnly.
func writeOwnerOnlyNamed(path string, data []byte) error {
	return writeSecretFileFixed(path, string(data))
}

func callerOfWriteOwnerOnly(path string, data []byte) error {
	return writeOwnerOnlyNamed(path, data)
}

var _ = fmt.Sprintf
