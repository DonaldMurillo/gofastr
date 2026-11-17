package static

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

// writeUnder writes data to dst, a path under outDir, through an
// os.Root opened on outDir: every component below outDir is resolved in
// the kernel, so a symlink planted inside the output tree fails the
// write instead of redirecting it outside (2026-10-27 I4). The export
// is public output, so files are 0644 and directories 0755.
func writeUnder(outDir, dst string, data []byte) error {
	rel, err := relUnder(outDir, dst)
	if err != nil {
		return err
	}
	root, err := os.OpenRoot(outDir)
	if err != nil {
		return err
	}
	defer root.Close()
	if err := root.MkdirAll(filepath.Dir(rel), 0o755); err != nil {
		return err
	}
	return root.WriteFile(rel, data, 0o644)
}

// removeUnder removes dst, a path under outDir, through an os.Root.
func removeUnder(outDir, dst string) error {
	rel, err := relUnder(outDir, dst)
	if err != nil {
		return err
	}
	root, err := os.OpenRoot(outDir)
	if err != nil {
		return err
	}
	defer root.Close()
	return root.Remove(rel)
}

// relUnder is dst relative to outDir, refused when it leaves outDir.
func relUnder(outDir, dst string) (string, error) {
	rel, err := filepath.Rel(outDir, dst)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", fmt.Errorf("static: refusing to write outside OutDir: %q", dst)
	}
	return rel, nil
}

// foldPath maps every rune of p to the smallest rune in its Unicode
// simple case-fold orbit, the relation strings.EqualFold uses, so two
// paths a case-insensitive filesystem treats as one fold to one key.
// p is decomposed first (NFD): APFS also treats a precomposed "café"
// and "cafe" with a combining accent as one name.
func foldPath(p string) string {
	return strings.Map(func(r rune) rune {
		least := r
		for f := unicode.SimpleFold(r); f != r; f = unicode.SimpleFold(f) {
			least = min(least, f)
		}
		return least
	}, norm.NFD.String(p))
}
