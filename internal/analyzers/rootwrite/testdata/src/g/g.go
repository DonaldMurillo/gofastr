// Package g pins bug bash 2026-10-27 I13 on the write side:
// pathflow.CallsSymlinkNamed (pathflow.go:633-655) accepted any call
// whose NAME contains "symlink" as a guard, with no look at the body
// and no tie to the path being written. A guard counts now only when
// its body really resolves or refuses symlinks (EvalSymlinks, an
// Lstat consulted with ModeSymlink, O_NOFOLLOW, or a call to such a
// guard) AND its argument is the sink's own path, its Dir,
// or a local bound to either.
package g

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"gsafe"
)

type store struct{ baseDir string }

// checkSymlink is named for the posture; its body refuses nothing.
func checkSymlink(string) error { return nil }

// fakeGuard: the name alone used to gate.
func (s *store) fakeGuard(name string, data []byte) error {
	p := filepath.Join(s.baseDir, name)
	if err := checkSymlink(p); err != nil {
		return err
	}
	return os.WriteFile(p, data, 0o600) // want `write under a root with lexical containment only`
}

// refuseSymlinkLeaf really refuses a symlinked leaf.
func refuseSymlinkLeaf(p string) error {
	fi, err := os.Lstat(p)
	if err != nil {
		return nil
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		return errors.New("symlink")
	}
	return nil
}

// guardOther: a real guard on an unrelated path resolves nothing for
// this write.
func (s *store) guardOther(name string, data []byte) error {
	if err := refuseSymlinkLeaf(filepath.Join(s.baseDir, "audit.log")); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(s.baseDir, name), data, 0o600) // want `write under a root with lexical containment only`
}

// guardRootOnly: a real guard on the root leaves every component under
// it unchecked.
func (s *store) guardRootOnly(name string, data []byte) error {
	if err := gsafe.EnsureNoSymlinkPath(s.baseDir); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(s.baseDir, name), data, 0o600) // want `write under a root with lexical containment only`
}

// guardOnPath is the codegen fileset posture: the real guard on the
// write's own parent chain and leaf. Quiet.
func (s *store) guardOnPath(name string, data []byte) error {
	p := filepath.Join(s.baseDir, name)
	if err := gsafe.EnsureNoSymlinkPath(filepath.Dir(p)); err != nil {
		return err
	}
	if err := refuseSymlinkLeaf(p); err != nil {
		return err
	}
	return os.WriteFile(p, data, 0o600)
}

// guardParentLocal: the guard reads a local bound to the Dir. Quiet.
func (s *store) guardParentLocal(name string, data []byte) error {
	p := filepath.Join(s.baseDir, name)
	parent := filepath.Dir(p)
	if err := gsafe.EnsureNoSymlinkPath(parent); err != nil {
		return err
	}
	if err := os.MkdirAll(parent, 0o700); err != nil {
		return err
	}
	return os.WriteFile(p, data, 0o600)
}

// guardWrapped: a wrapper with a neutral name whose body calls the
// real guard is a guard too. Quiet.
func (s *store) guardWrapped(name string, data []byte) error {
	p := filepath.Join(s.baseDir, name)
	if err := gsafe.RefuseLinks(p); err != nil {
		return err
	}
	return os.WriteFile(p, data, 0o600)
}

// guardFakeCrossPackage: a cross-package callee named for the posture
// whose body does none of it.
func (s *store) guardFakeCrossPackage(name string, data []byte) error {
	p := filepath.Join(s.baseDir, name)
	if err := gsafe.NoSymlinkFake(p); err != nil {
		return err
	}
	return os.WriteFile(p, data, 0o600) // want `write under a root with lexical containment only`
}

// pathFor joins under the receiver's root and calls a name-only
// "guard": the helper hop no longer reads that as resolution.
func (s *store) pathFor(name string) string {
	p := filepath.Join(s.baseDir, name)
	_ = checkSymlink(p)
	return p
}

func (s *store) viaFakeHelper(name string, data []byte) error {
	return os.WriteFile(s.pathFor(name), data, 0o600) // want `write under a root with lexical containment only`
}

// pathForGuarded is the helper-hop fix: its body calls a real guard.
func (s *store) pathForGuarded(name string) (string, error) {
	p := filepath.Join(s.baseDir, name)
	if err := gsafe.EnsureNoSymlinkPath(p); err != nil {
		return "", err
	}
	return p, nil
}

func (s *store) viaGuardedHelper(name string, data []byte) error {
	p, err := s.pathForGuarded(name)
	if err != nil {
		return err
	}
	return os.WriteFile(p, data, 0o600)
}

// lstatSymlinkExists Lstats but never consults ModeSymlink: an
// existence check, not a refusal.
func lstatSymlinkExists(p string) error {
	_, err := os.Lstat(p)
	return err
}

func (s *store) lstatOnly(name string, data []byte) error {
	p := filepath.Join(s.baseDir, name)
	if err := lstatSymlinkExists(p); err != nil {
		return err
	}
	return os.WriteFile(p, data, 0o600) // want `write under a root with lexical containment only`
}

// confirmInside resolves the parent chain and refuses an escape: a
// guard by its body, whatever its name says. Quiet.
func confirmInside(root, p string) error {
	real, err := filepath.EvalSymlinks(filepath.Dir(p))
	if err != nil {
		return err
	}
	if !strings.HasPrefix(real, root) {
		return errors.New("escapes")
	}
	return nil
}

func (s *store) evalGuard(name string, data []byte) error {
	p := filepath.Join(s.baseDir, name)
	if err := confirmInside(s.baseDir, p); err != nil {
		return err
	}
	return os.WriteFile(p, data, 0o600)
}

// chmodFirst: os.Chmod carries O_NOFOLLOW-shaped code inside the
// standard library; a stdlib call on the path is no guard.
func (s *store) chmodFirst(name string, data []byte) error {
	p := filepath.Join(s.baseDir, name)
	if err := os.Chmod(p, 0o600); err != nil {
		return err
	}
	return os.WriteFile(p, data, 0o600) // want `write under a root with lexical containment only`
}
