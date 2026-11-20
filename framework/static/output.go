package static

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

func checkRouteOutput(route, p string) error {
	dir := strings.Trim(hostPath(p), "/")
	if dir == "" {
		return nil
	}
	for name := range strings.SplitSeq(dir, "/") {
		if !portableFilename(name) {
			return fmt.Errorf("static: route %q exports to %q: %q is not a portable filename", route, p, name)
		}
	}
	return nil
}

func portableFilename(name string) bool {
	if name == "" || len(name) > 255 || !utf8.ValidString(name) ||
		strings.ContainsAny(name, `<>:"\|?*`) || strings.HasSuffix(name, ".") || strings.HasSuffix(name, " ") ||
		strings.ContainsFunc(name, func(r rune) bool { return r < 32 }) {
		return false
	}
	stem, _, _ := strings.Cut(name, ".")
	stem = strings.ToUpper(strings.TrimRight(stem, " "))
	switch stem {
	case "CON", "PRN", "AUX", "NUL", "CONIN$", "CONOUT$":
		return false
	}
	if strings.HasPrefix(stem, "COM") || strings.HasPrefix(stem, "LPT") {
		switch stem[3:] {
		case "1", "2", "3", "4", "5", "6", "7", "8", "9", "¹", "²", "³":
			return false
		}
	}
	return true
}

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

func relUnder(outDir, dst string) (string, error) {
	rel, err := filepath.Rel(outDir, dst)
	if err != nil {
		return "", fmt.Errorf("static: refusing to write outside OutDir: %q under %q: %w", dst, outDir, err)
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", fmt.Errorf("static: refusing to write outside OutDir: %q escapes %q", dst, outDir)
	}
	return rel, nil
}

func foldPath(p string) string {
	return strings.Map(func(r rune) rune {
		least := r
		for f := unicode.SimpleFold(r); f != r; f = unicode.SimpleFold(f) {
			least = min(least, f)
		}
		return least
	}, norm.NFD.String(p))
}
