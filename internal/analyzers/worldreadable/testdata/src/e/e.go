// Package e pins atomicfile.Rewrite (2026-10-31): moving an
// os.WriteFile to the atomic rewrite must not take it out of either arm.
package e

import (
	"os"
	"path/filepath"

	"atomicfile"
)

// WriteNDJSON is a.writeNDJSON through the atomic rewrite.
func WriteNDJSON(path string) error {
	return atomicfile.Rewrite(path, []byte("row"), 0o644) // want `file created with mode 0o644 under path`
}

// WriteToken claims owner-only, which Rewrite gives a new file only.
func WriteToken(dir string) error {
	return atomicfile.Rewrite(filepath.Join(dir, "token"), []byte("t"), 0o600) // want `atomicfile\.Rewrite mode 0o600 applies only at create`
}

// ThemeStarter is b's theme starter: the 0755 dir holds one public
// 0644 source file, now written through Rewrite.
func ThemeStarter(dest string) error {
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	return atomicfile.Rewrite(dest, []byte("package theme\n"), 0o644)
}
