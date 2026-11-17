package static

import (
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

const pageManifestSuffix = ".gofastr-pages"

func (b *Builder) pageManifest() (dir, name string, err error) {
	abs, err := filepath.Abs(b.OutDir)
	if err != nil {
		return "", "", fmt.Errorf("static: resolve out dir: %w", err)
	}
	dir = filepath.Dir(abs)
	if dir == abs {
		return "", "", fmt.Errorf("static: out dir %s is a filesystem root, with no parent to keep the page manifest in", abs)
	}
	return dir, "." + filepath.Base(abs) + pageManifestSuffix, nil
}

func (b *Builder) pruneStalePages(res Result, rendered *fileSet, docs map[string]bool) error {
	root, err := os.OpenRoot(b.OutDir)
	if err != nil {
		return fmt.Errorf("static: open out dir: %w", err)
	}
	defer root.Close()
	mdir, mname, err := b.pageManifest()
	if err != nil {
		return err
	}
	mroot, err := os.OpenRoot(mdir)
	if err != nil {
		return fmt.Errorf("static: open page manifest dir: %w", err)
	}
	defer mroot.Close()
	written := map[string]bool{}
	current := newFileSet()
	for _, p := range rendered.paths() {
		current.add(p)
	}
	for _, a := range res.Assets {
		written[strings.TrimPrefix(a, "/")] = true
		current.add(strings.TrimPrefix(a, "/"))
	}
	for p := range docs {
		current.add(p)
	}
	prev, err := mroot.ReadFile(mname)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("static: read page manifest: %w", err)
	}
	for _, rel := range strings.Split(string(prev), "\n") {
		if rel == "" || rendered.hasExact(rel) || written[rel] {
			continue
		}
		if cur, ok := current.spelling(rel); ok && sameFile(root, rel, cur) {
			continue
		}
		if err := root.Remove(filepath.FromSlash(rel)); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("static: remove stale page %q: %w", rel, err)
		}
		b.log("removed stale page /%s", rel)
	}
	pages := maps.Clone(docs)
	for _, p := range rendered.paths() {
		pages[p] = true
	}
	body := strings.Join(slices.Sorted(maps.Keys(pages)), "\n") + "\n"
	if err := mroot.WriteFile(mname, []byte(body), 0o600); err != nil {
		return fmt.Errorf("static: write page manifest: %w", err)
	}
	return nil
}

func sameFile(root *os.Root, a, b string) bool {
	fa, err := root.Stat(filepath.FromSlash(a))
	if err != nil {
		return false
	}
	fb, err := root.Stat(filepath.FromSlash(b))
	return err == nil && os.SameFile(fa, fb)
}
