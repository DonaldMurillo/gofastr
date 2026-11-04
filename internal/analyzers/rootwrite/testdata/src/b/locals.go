// Package b, file 2: locals and declaration spellings the 2026-11-03
// gap round (R8-AZ7-01/06/07) found missing — a zero-argument Join
// that panicked the helper-hop walk, var-spec and local-flag bindings
// that resolved to nothing, and an Lstat leaf check credited on
// co-presence.
package b

import (
	"os"
	"path/filepath"
	"syscall"
)

// zeroArgJoin is legal Go returning "": reading call.Args[0] before
// the argument count panicked the helper-hop walk on any sink whose
// path routes through it.
func zeroArgJoin(baseDir string) string {
	return filepath.Join()
}

// writeViaZeroJoin routes a sink path through the zero-arg joiner: the
// walk must survive it and find nothing to report.
func writeViaZeroJoin(baseDir string) error {
	return os.WriteFile(zeroArgJoin(baseDir), nil, 0o600)
}

// varSpecPath binds the joined path with `var p = ...`: the
// declaration spelling must resolve like `p := ...` does.
func varSpecPath(baseDir, name string) error {
	var p = filepath.Join(baseDir, "spool", name)
	return os.WriteFile(p, nil, 0o600) // want `write under a root with lexical containment only`
}

// localFlags holds the write bits in a local: the flag expression must
// resolve through the binding, or the write-open reads as a plain
// read and the sink disappears.
func localFlags(spoolDir, id string) error {
	flags := os.O_WRONLY | os.O_CREATE
	f, err := os.OpenFile(filepath.Join(spoolDir, id), flags, 0o600) // want `write under a root with lexical containment only`
	if err != nil {
		return err
	}
	return f.Close()
}

// varFlags is the var-spec spelling of the same local.
func varFlags(spoolDir, id string) error {
	var flags = os.O_WRONLY | os.O_CREATE | os.O_TRUNC
	f, err := os.OpenFile(filepath.Join(spoolDir, id), flags, 0o600) // want `write under a root with lexical containment only`
	if err != nil {
		return err
	}
	return f.Close()
}

// localFlagsNoFollow keeps the write bits and O_NOFOLLOW in one local:
// the leaf-following posture stays quiet through a local too.
func localFlagsNoFollow(spoolDir, id string) error {
	flags := os.O_WRONLY | os.O_CREATE | syscall.O_NOFOLLOW
	f, err := os.OpenFile(filepath.Join(spoolDir, id), flags, 0o600)
	if err != nil {
		return err
	}
	return f.Close()
}

// localReadFlags opens read-only through a local: not a write, quiet.
func localReadFlags(spoolDir, id string) ([]byte, error) {
	flags := os.O_RDONLY
	f, err := os.OpenFile(filepath.Join(spoolDir, id), flags, 0)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return nil, f.Close()
}

// lstatOtherConsulted Lstats the sink's own target and throws the
// FileInfo away; the ModeSymlink test reads ANOTHER file's Lstat. The
// leaf posture was credited on co-presence — any Lstat touching the
// path plus any ModeSymlink selector in the body — though both root
// rules' docs say the Lstat result must be consulted.
func lstatOtherConsulted(workDir, id string, data []byte) error {
	p := filepath.Join(workDir, id)
	if _, err := os.Lstat(p); err != nil {
		return err
	}
	other, err := os.Lstat("manifest")
	if err == nil && other.Mode()&os.ModeSymlink != 0 {
		return nil
	}
	return os.WriteFile(p, data, 0o600) // want `write under a root with lexical containment only`
}

// fakeGuardDiscard Lstats the path it is handed and throws the
// FileInfo away; the ModeSymlink test below reads no Lstat result.
// Co-presence made this a "guard" whose call gated the write.
func fakeGuardDiscard(p string) error {
	if _, err := os.Lstat(p); err != nil {
		return err
	}
	if os.ModeSymlink&os.FileMode(0) != 0 {
		return nil
	}
	return nil
}

// writeViaFakeGuard hands the joined path to the fake guard: the write
// must still fire.
func writeViaFakeGuard(workDir, id string, data []byte) error {
	p := filepath.Join(workDir, id)
	if err := fakeGuardDiscard(p); err != nil {
		return err
	}
	return os.WriteFile(p, data, 0o600) // want `write under a root with lexical containment only`
}
