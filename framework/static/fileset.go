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

func (s *fileSet) has(rel string) bool {
	_, ok := s.folded[foldPath(rel)]
	return ok
}

func (s *fileSet) hasExact(rel string) bool { return s.spelled[rel] }

func (s *fileSet) spelling(rel string) (string, bool) {
	p, ok := s.folded[foldPath(rel)]
	return p, ok
}

func (s *fileSet) paths() []string { return slices.Sorted(maps.Keys(s.spelled)) }

func caseTwinError(a, b string) error {
	return fmt.Errorf("static: %s and %s differ only in case and are one file on a case-insensitive filesystem; give one of them another name", a, b)
}

func refuseSeparatorName(name string) error {
	if strings.ContainsAny(name, "\\\x00") {
		return fmt.Errorf("static: file %q holds a backslash or NUL; name it with / only", name)
	}
	return nil
}

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

func writtenPaths(res *Result, rendered *fileSet, docs map[string]bool) []string {
	out := rendered.paths()
	out = append(out, slices.Sorted(maps.Keys(docs))...)
	for _, a := range res.Assets {
		out = append(out, strings.TrimPrefix(a, "/"))
	}
	return out
}

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
	var behindLink [][2]string // parent, name: judged once every rename ran
	var links []string         // symlinks on the paths, their targets judged likewise
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
				if linked[parent] {
					behindLink = append(behindLink, [2]string{parent, name})
				} else if err := d.respell(root, parent, name); err != nil {
					return err
				}
				fi, err := root.Lstat(filepath.FromSlash(cur))
				stop[cur] = err != nil
				isLink := err == nil && fi.Mode()&fs.ModeSymlink != 0
				if isLink {
					links = append(links, cur)
				}
				linked[cur] = linked[parent] || isLink
			}
			if stop[cur] {
				break
			}
			parent = cur
		}
	}
	fresh := map[string]*dirNames{}
	listing := func(dir string) *dirNames {
		if fresh[dir] == nil {
			fresh[dir] = listNames(root, dir)
		}
		return fresh[dir]
	}
	for _, e := range behindLink {
		parent, name := e[0], e[1]
		if old, ok := listing(parent).sameFileAs(root, parent, name); ok {
			return fmt.Errorf("static: /%s is spelled /%s on disk, behind a symlink the export does not rename through; remove the stale entry", path.Join(parent, name), path.Join(parent, old))
		}
	}
	for _, link := range links {
		target, err := root.Readlink(filepath.FromSlash(link))
		if err != nil || filepath.IsAbs(target) {
			continue
		}
		t := path.Join(path.Dir(link), filepath.ToSlash(target))
		if t == ".." || strings.HasPrefix(t, "../") {
			continue
		}
		parent := "."
		for _, name := range strings.Split(t, "/") {
			if old, ok := listing(parent).sameFileAs(root, parent, name); ok {
				return fmt.Errorf("static: the symlink /%s names /%s, which is spelled /%s on disk; point it at that spelling", link, path.Join(parent, name), path.Join(parent, old))
			}
			parent = path.Join(parent, name)
		}
	}
	return nil
}

type dirNames struct {
	exact  map[string]bool
	folded map[string]string
}

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

func (d *dirNames) sameFileAs(root *os.Root, parent, name string) (string, bool) {
	if d.exact[name] {
		return "", false
	}
	old, ok := d.folded[foldPath(name)]
	if !ok {
		return "", false
	}
	a, errA := root.Lstat(filepath.FromSlash(path.Join(parent, old)))
	b, errB := root.Lstat(filepath.FromSlash(path.Join(parent, name)))
	if errA != nil || errB != nil || !os.SameFile(a, b) {
		return "", false
	}
	return old, true
}

func (d *dirNames) respell(root *os.Root, parent, name string) error {
	old, ok := d.sameFileAs(root, parent, name)
	if !ok {
		return nil
	}
	from, to := path.Join(parent, old), path.Join(parent, name)
	if err := root.Rename(filepath.FromSlash(from), filepath.FromSlash(to)); err != nil {
		return fmt.Errorf("static: respell /%s as /%s: %w", from, to, err)
	}
	delete(d.exact, old)
	d.exact[name] = true
	d.folded[foldPath(name)] = name
	return nil
}
