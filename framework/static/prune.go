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

// pageManifestName is the file in OutDir listing the page files the
// last build into it wrote, one slash path per line.
const pageManifestName = ".gofastr-pages"

// pruneStalePages removes the page files a previous build into this
// OutDir wrote that this build does not write: a route since removed or
// excluded, a page a policy now refuses, one that answers a 404 outlet
// or another status. A static host serves whatever file sits at a path
// as a 200, so the old file kept the page published, a private one
// among them. Every stale page goes, not only the policy and 404 skips:
// each is a URL the app no longer answers with that page, and keeping
// any of them publishes content the live server does not.
//
// Only a file the previous build's manifest names is a candidate, so a
// file the builder never wrote stays, and so does any file this build
// put at the same path (a page, or anything in res.Assets: a doc, an
// asset, a static-source or ExtraDirs copy). Removal goes through os.Root, so no manifest entry
// reaches outside OutDir. A build into an OutDir with no manifest
// prunes nothing. The manifest then records this build's page files.
func (b *Builder) pruneStalePages(res Result, rendered *fileSet, docs map[string]bool) error {
	root, err := os.OpenRoot(b.OutDir)
	if err != nil {
		return fmt.Errorf("static: open out dir: %w", err)
	}
	defer root.Close()
	written := map[string]bool{}
	for _, a := range res.Assets {
		written[strings.TrimPrefix(a, "/")] = true
	}
	prev, err := root.ReadFile(pageManifestName)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("static: read page manifest: %w", err)
	}
	for _, rel := range strings.Split(string(prev), "\n") {
		if rel == "" || rendered.hasExact(rel) || written[rel] {
			continue
		}
		// A page renamed by case only ("About" to "about") was rewritten
		// in place on a case-insensitive filesystem: the old spelling
		// names the new page, which stays. On a case-sensitive one the
		// two are different files and the old one goes.
		if cur, ok := rendered.spelling(rel); ok && sameFile(root, rel, cur) {
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
	if err := root.WriteFile(pageManifestName, []byte(body), 0o644); err != nil {
		return fmt.Errorf("static: write page manifest: %w", err)
	}
	return nil
}

// sameFile reports whether a and b, slash paths under root, name one
// existing file.
func sameFile(root *os.Root, a, b string) bool {
	fa, err := root.Stat(filepath.FromSlash(a))
	if err != nil {
		return false
	}
	fb, err := root.Stat(filepath.FromSlash(b))
	return err == nil && os.SameFile(fa, fb)
}
