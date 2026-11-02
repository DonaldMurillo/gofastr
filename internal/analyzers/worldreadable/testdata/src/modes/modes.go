// Package modes holds the constant-mode spellings the 2026-11-02 guard
// bash found worldreadable blind to: the mode is constant in every case
// below, only not a bare integer literal — a FileMode conversion, a
// named constant, an expression of constants, and the os.ModePerm
// constant itself.
package modes

import (
	"io/fs"
	"os"
	"path/filepath"
)

const statePerm = 0o644

func Conversion(dir string, secret []byte) error {
	return os.WriteFile(filepath.Join(dir, "session.key"), secret, os.FileMode(0o644)) // want `created with mode`
}

func NamedConst(dir string, secret []byte) error {
	return os.WriteFile(filepath.Join(dir, "session.key"), secret, statePerm) // want `created with mode`
}

func ModePerm(dir string) error {
	return os.MkdirAll(filepath.Join(dir, "state"), fs.ModePerm) // want `created with mode`
}

func OrExpr(dir string, secret []byte) error {
	return os.WriteFile(filepath.Join(dir, "session.key"), secret, 0o600|0o044) // want `created with mode`
}

// Silent: owner-only constants in the same spellings.
const ownerOnly = 0o600

func QuietConst(dir string, secret []byte) error {
	f, err := os.OpenFile(filepath.Join(dir, "session.key"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, ownerOnly)
	if err != nil {
		return err
	}
	_, err = f.Write(secret)
	return err
}
