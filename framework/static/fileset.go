package static

import (
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
)

// fileSet is a set of slash paths under OutDir that the build writes
// itself. has matches case-folded: two paths that differ only in case
// are one file on a case-insensitive filesystem (APFS, NTFS by
// default), so a copy that would land on one of these files in another
// case is refused like an exact match, and the export is the same
// whichever filesystem built it.
type fileSet struct {
	spelled map[string]bool
	folded  map[string]string // foldPath(p) -> p as written
}

func newFileSet() *fileSet {
	return &fileSet{spelled: map[string]bool{}, folded: map[string]string{}}
}

func (s *fileSet) add(rel string) {
	s.spelled[rel] = true
	s.folded[foldPath(rel)] = rel
}

// has reports whether rel, in any case, is in the set.
func (s *fileSet) has(rel string) bool {
	_, ok := s.folded[foldPath(rel)]
	return ok
}

// hasExact reports whether rel, spelled exactly, is in the set.
func (s *fileSet) hasExact(rel string) bool { return s.spelled[rel] }

// spelling returns the set's spelling of rel's file, if it has one.
func (s *fileSet) spelling(rel string) (string, bool) {
	p, ok := s.folded[foldPath(rel)]
	return p, ok
}

// paths returns the set's paths as written, sorted.
func (s *fileSet) paths() []string { return slices.Sorted(maps.Keys(s.spelled)) }

// caseTwinError refuses two spellings of one file. A case-insensitive
// filesystem keeps whichever spelling was written first and a
// case-sensitive host serves only that one, so a page asking for the
// other gets a 404.
func caseTwinError(a, b string) error {
	return fmt.Errorf("static: %s and %s differ only in case and are one file on a case-insensitive filesystem; give one of them another name", a, b)
}

// refuseSeparatorName refuses an fs.FS name holding a backslash, which
// fs.ValidPath allows as a literal character but Windows reads as a
// separator: about\index.html would land on about/index.html there,
// past every check keyed on the slash path. A NUL names no file.
func refuseSeparatorName(name string) error {
	if strings.ContainsAny(name, "\\\x00") {
		return fmt.Errorf("static: file %q holds a backslash or NUL; name it with / only", name)
	}
	return nil
}

// checkCaseTwins fails the build when two files it wrote, or two
// directories on their paths, differ only in case: a contributor's
// _HEADERS rewritten later as _headers kept the contributor's spelling
// on APFS, and a static OFFLINE/ held the worker's offline/index.html.
// A case-sensitive host then serves neither generated name.
func checkCaseTwins(res *Result, rendered *fileSet, docs map[string]bool) error {
	seen := map[string]string{}
	check := func(rel string) error {
		f := foldPath(rel)
		if prev, ok := seen[f]; ok && prev != rel {
			return caseTwinError("/"+prev, "/"+rel)
		}
		seen[f] = rel
		return nil
	}
	for _, p := range writtenPaths(res, rendered, docs) {
		// A directory is keyed with its slash, so it never meets a file.
		for i := range len(p) {
			if p[i] == '/' {
				if err := check(p[:i+1]); err != nil {
					return err
				}
			}
		}
		if err := check(p); err != nil {
			return err
		}
	}
	return nil
}

// writtenPaths lists every slash path the build wrote: pages and the
// host-rule files, docs, and assets.
func writtenPaths(res *Result, rendered *fileSet, docs map[string]bool) []string {
	out := rendered.paths()
	out = append(out, slices.Sorted(maps.Keys(docs))...)
	for _, a := range res.Assets {
		out = append(out, strings.TrimPrefix(a, "/"))
	}
	return out
}

// respell renames each directory entry on the build's paths to the
// build's spelling where the filesystem kept another: a case-insensitive
// one keeps the name an entry was created with, so a rebuild after a
// case-only route rename, or over a leftover _HEADERS, rewrote the
// content under the old name. An entry is renamed only when it is the
// build's file under another name (os.SameFile): on a case-sensitive
// disk a stale _REDIRECTS is another file and stays. Nothing is renamed
// past a symlink, so nothing outside the build's own directories moves,
// but the walk goes on through it (os.Root keeps it inside outDir): an
// entry there in another spelling fails the build, since the export
// would not serve that path on a case-sensitive host.
func respell(outDir string, paths []string) error {
	root, err := os.OpenRoot(outDir)
	if err != nil {
		return fmt.Errorf("static: open out dir: %w", err)
	}
	defer root.Close()
	listed := map[string]*dirNames{}
	stop := map[string]bool{}   // a path at which the walk ends: nothing there
	linked := map[string]bool{} // a path at or below a symlink
	done := map[string]bool{}
	for _, p := range paths {
		parent := "."
		for _, name := range strings.Split(p, "/") {
			cur := path.Join(parent, name)
			if !done[cur] {
				done[cur] = true
				d := listed[parent]
				if d == nil {
					d = listNames(root, parent)
					listed[parent] = d
				}
				if err := d.respell(root, parent, name, linked[parent]); err != nil {
					return err
				}
				fi, err := root.Lstat(filepath.FromSlash(cur))
				stop[cur] = err != nil
				linked[cur] = linked[parent] || (err == nil && fi.Mode()&fs.ModeSymlink != 0)
			}
			if stop[cur] {
				break
			}
			parent = cur
		}
	}
	return nil
}

// dirNames is one directory's listing, by exact and folded name.
type dirNames struct {
	exact  map[string]bool
	folded map[string]string
}

// listNames lists dir under root. A directory that cannot be read in
// full lists nothing: a partial listing could miss the entry to rename,
// and renaming none leaves the build's other checks to judge the tree.
func listNames(root *os.Root, dir string) *dirNames {
	d := &dirNames{exact: map[string]bool{}, folded: map[string]string{}}
	f, err := root.Open(filepath.FromSlash(dir))
	if err != nil {
		return d
	}
	defer f.Close()
	names, err := f.Readdirnames(-1)
	if err != nil {
		return d
	}
	for _, n := range names {
		d.exact[n] = true
		d.folded[foldPath(n)] = n
	}
	return d
}

// respell renames parent's entry for name to name when the directory
// holds it only under another spelling of the same file. Under a
// symlink (linked) it refuses instead of renaming.
func (d *dirNames) respell(root *os.Root, parent, name string, linked bool) error {
	if d.exact[name] {
		return nil
	}
	old, ok := d.folded[foldPath(name)]
	if !ok {
		return nil
	}
	from, to := path.Join(parent, old), path.Join(parent, name)
	a, errA := root.Lstat(filepath.FromSlash(from))
	b, errB := root.Lstat(filepath.FromSlash(to))
	if errA != nil || errB != nil || !os.SameFile(a, b) {
		return nil
	}
	if linked {
		return fmt.Errorf("static: /%s is spelled /%s on disk, behind a symlink the export does not rename through; remove the stale entry", to, from)
	}
	if err := root.Rename(filepath.FromSlash(from), filepath.FromSlash(to)); err != nil {
		return fmt.Errorf("static: respell /%s as /%s: %w", from, to, err)
	}
	delete(d.exact, old)
	d.exact[name] = true
	d.folded[foldPath(name)] = name
	return nil
}
