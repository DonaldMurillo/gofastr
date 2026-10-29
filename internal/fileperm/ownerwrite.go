package fileperm

import "os"

// WriteOwnerOnly writes data to path as an owner-only (0600) file, on
// CREATE and on OVERWRITE alike.
// Windows uses a protected owner-only DACL; Unix uses mode 0600.
//
// os.WriteFile(path, data, 0o600) applies its mode only when it creates
// the file: a pre-existing 0644 file (an operator chmod, a restored
// backup, a dotfiles manager, umask drift) is truncated and refilled
// with the secret while it is still world-readable. The mode is fixed
// first, on the open handle, before a byte lands. This is the one
// spelling for every secret- or state-bearing write in the tree; the
// 2026-09-06 probes found seven sites still on os.WriteFile.
func WriteOwnerOnly(path string, data []byte) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	return fillOwnerOnly(f, data)
}

// fillOwnerOnly tightens the open handle to 0600, writes data, and
// closes it. Split from WriteOwnerOnly so the chmod and write failure
// arms are reachable from a test through a handle in the wrong state,
// without fault injection.
func fillOwnerOnly(f *os.File, data []byte) error {
	if err := RestrictOpenFile(f); err != nil {
		f.Close()
		return err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// CreateOwnerOnly is WriteOwnerOnly for a write too large to hold in
// memory first: it opens path for writing as an owner-only (0600) file,
// truncating one that exists, and tightens the open handle before the
// caller writes a byte. The caller closes it.
func CreateOwnerOnly(path string) (*os.File, error) {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return nil, err
	}
	return restrictedHandle(f)
}

// restrictedHandle is CreateOwnerOnly's handle half, split out for the
// same reason as fillOwnerOnly.
func restrictedHandle(f *os.File) (*os.File, error) {
	if err := RestrictOpenFile(f); err != nil {
		f.Close()
		return nil, err
	}
	return f, nil
}

// SeedOwnerOnly creates path as an empty owner-only (0600) file if it
// does not exist and tightens it to 0600 if it does, for a file some
// other library will open next. database/sql's SQLite driver creates a
// missing database (and its -wal sidecar) 0666&~umask; seeding first
// makes both inherit 0600.
func SeedOwnerOnly(path string) error {
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return err
	}
	return tightenAndClose(f)
}

// tightenAndClose is SeedOwnerOnly's handle half, split out for the
// same reason as fillOwnerOnly.
func tightenAndClose(f *os.File) error {
	if err := RestrictOpenFile(f); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}
