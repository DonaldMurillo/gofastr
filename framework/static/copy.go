package static

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	corestatic "github.com/DonaldMurillo/gofastr/core/static"
	"github.com/DonaldMurillo/gofastr/internal/rootfs"
)

func (b *Builder) staticSourceFile(rel string) bool {
	fsys := b.staticSource()
	return fsys != nil && isFile(fsys, rel)
}

func isFile(fsys fs.FS, name string) bool {
	if !fs.ValidPath(name) {
		return false
	}
	info, err := fs.Stat(fsys, name)
	return err == nil && info.Mode().IsRegular()
}

func (b *Builder) staticSourceHas(rel string) bool {
	fsys := b.staticSource()
	return fsys != nil && spelledIn(fsys, filepath.ToSlash(rel))
}

// staticSource is the host's static source as the copy reads it: a
// static dir through os.Root, so a symlink out of it is not there.
func (b *Builder) staticSource() fs.FS {
	if dir := b.Host.StaticDir(); dir != "" {
		return rootfs.ConfineDirFS(os.DirFS(dir))
	}
	return b.Host.StaticFS()
}

func spelledIn(fsys fs.FS, rel string) bool {
	fi, err := fs.Stat(fsys, rel)
	if errors.Is(err, fs.ErrNotExist) || (err == nil && !fi.Mode().IsRegular()) {
		return false
	}
	names := strings.Split(rel, "/")
	dir := "."
	for i, name := range names {
		ents, err := fs.ReadDir(fsys, dir)
		if err != nil {
			return false
		}
		j := slices.IndexFunc(ents, func(e fs.DirEntry) bool { return e.Name() == name })
		if j < 0 || (i == len(names)-1 && fi == nil && !ents[j].Type().IsRegular()) {
			return false
		}
		dir = path.Join(dir, name)
	}
	return true
}

func (b *Builder) dumpExtraDirs(res *Result, rendered *fileSet) error {
	written := newFileSet()
	for _, p := range res.Assets {
		written.add(p)
	}
	mounts := make([]string, 0, len(b.extraDirs))
	for m := range b.extraDirs {
		mounts = append(mounts, m)
	}
	sort.Strings(mounts)
	for _, mount := range mounts {
		fsys := b.extraDirs[mount]
		if fsys == nil {
			continue
		}
		if err := refuseSeparatorName(mount); err != nil {
			return fmt.Errorf("static: extra dir: %w", err)
		}
		cleanMount := "/" + strings.Trim(mount, "/")
		err := walkStatic(fsys, b.log, func(p string, d fs.DirEntry) error {
			if err := refuseSeparatorName(p); err != nil {
				return err
			}
			urlPath := path.Join(cleanMount, p)
			rel := strings.TrimPrefix(urlPath, "/")
			if !publishable(rel) {
				b.log("skipped %s (not published)", urlPath)
				return nil
			}
			if skipIrregular(fsys, p, d, b.log) {
				return nil
			}
			dst := filepath.Join(b.OutDir, filepath.FromSlash(rel))
			if own, ok := written.spelling(urlPath); ok && own != urlPath {
				return caseTwinError(own, urlPath)
			}
			if written.has(urlPath) || rendered.has(rel) || b.staticSourceHas(filepath.FromSlash(rel)) {
				b.log("kept %s (extra dir %s skipped)", urlPath, mount)
				return nil
			}
			data, err := fs.ReadFile(fsys, p)
			if err != nil {
				return err
			}
			if err := writeUnder(b.OutDir, dst, data); err != nil {
				return err
			}
			res.Assets = append(res.Assets, urlPath)
			written.add(urlPath)
			b.log("copied extra %s", urlPath)
			return nil
		})
		if err != nil {
			return fmt.Errorf("static: extra dir %q: %w", mount, err)
		}
	}
	return nil
}

func keepRendered(rel string, rendered *fileSet, log func(string, ...any)) bool {
	own, ok := rendered.spelling(rel)
	if !ok {
		return false
	}
	if own == rel {
		log("kept the rendered page /%s (static source copy refused)", rel)
	} else {
		log("kept the rendered page /%s (static source copy /%s refused)", own, rel)
	}
	return true
}

func publishable(rel string) bool {
	if rel == ".nojekyll" {
		return true
	}
	return !corestatic.Refuses(strings.TrimPrefix(rel, ".well-known/"))
}

func generatedHostRules(rel string, rendered *fileSet) bool {
	return (rel == "_headers" || rel == "_redirects") && rendered.hasExact(rel)
}

func appendHostRules(root *os.Root, rel string, data []byte, log func(string, ...any)) error {
	f, err := root.OpenFile(rel, os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	log("appended the static source's %s to the generated one", rel)
	return nil
}

func copyDir(src, dst string, rendered *fileSet, res *Result, log func(string, ...any)) error {
	root, err := os.OpenRoot(dst)
	if err != nil {
		return err
	}
	defer root.Close()
	srcRoot, err := os.OpenRoot(src)
	if err != nil {
		return err
	}
	defer srcRoot.Close()
	return walkStatic(srcRoot.FS(), log, func(slashRel string, d fs.DirEntry) error {
		rel := filepath.FromSlash(slashRel)
		if err := refuseSeparatorName(slashRel); err != nil {
			return err
		}
		if !publishable(filepath.ToSlash(rel)) {
			log("skipped %s (not published)", rel)
			return nil
		}
		in, skip, err := openSourceFile(srcRoot, rel, d)
		if err != nil {
			return err
		}
		if skip != "" {
			log("skipped %s (%s)", rel, skip)
			return nil
		}
		defer in.Close()
		if generatedHostRules(filepath.ToSlash(rel), rendered) {
			data, err := io.ReadAll(in)
			if err != nil {
				return err
			}
			return appendHostRules(root, rel, data, log)
		}
		if keepRendered(filepath.ToSlash(rel), rendered, log) {
			return nil
		}
		out := filepath.Join(dst, rel)
		if err := root.MkdirAll(filepath.Dir(rel), 0o755); err != nil {
			return err
		}
		w, err := root.OpenFile(rel, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
		if err != nil {
			return err
		}
		defer w.Close()
		if _, err := io.Copy(w, in); err != nil {
			return err
		}
		res.Assets = append(res.Assets, "/"+filepath.ToSlash(rel))
		log("copied %s -> %s", rel, out)
		return nil
	})
}

func walkStatic(fsys fs.FS, log func(string, ...any), fn func(rel string, d fs.DirEntry) error) error {
	rootInfo, err := fs.Stat(fsys, ".")
	if err != nil {
		return err
	}
	var walk func(dir string, chain []fs.FileInfo) error
	walk = func(dir string, chain []fs.FileInfo) error {
		entries, err := fs.ReadDir(fsys, dir)
		if err != nil {
			return err
		}
		for _, e := range entries {
			rel := e.Name()
			if dir != "." {
				rel = path.Join(dir, e.Name())
			}
			var sub fs.FileInfo
			switch {
			case e.IsDir():
				if sub, err = e.Info(); err != nil {
					return err
				}
			case e.Type()&fs.ModeSymlink != 0 && rootfs.Confined(fsys):
				if fi, err := fs.Stat(fsys, rel); err == nil && fi.IsDir() {
					sub = fi
				}
			}
			if sub == nil {
				if err := fn(rel, e); err != nil {
					return err
				}
				continue
			}
			if slices.ContainsFunc(chain, func(a fs.FileInfo) bool { return os.SameFile(a, sub) }) {
				log("skipped %s (a link back to a directory it is in)", rel)
				continue
			}
			if err := walk(rel, append(slices.Clip(chain), sub)); err != nil {
				return err
			}
		}
		return nil
	}
	return walk(".", []fs.FileInfo{rootInfo})
}

func openSourceFile(srcRoot *os.Root, rel string, d fs.DirEntry) (*os.File, string, error) {
	st, err := srcRoot.Stat(rel)
	if err != nil {
		if d.Type()&fs.ModeSymlink != 0 {
			return nil, "symlink out of the static dir or to nothing", nil
		}
		return nil, "", err
	}
	if !st.Mode().IsRegular() {
		return nil, "not a regular file", nil
	}
	f, err := srcRoot.Open(rel)
	return f, "", err
}

func skipIrregular(fsys fs.FS, rel string, d fs.DirEntry, log func(string, ...any)) bool {
	switch t := d.Type(); {
	case t.IsRegular():
		return false
	case t&fs.ModeSymlink == 0:
		log("skipped %s (not a regular file)", rel)
		return true
	}
	if !rootfs.Confined(fsys) {
		log("skipped %s (symlink in a filesystem os.Root does not confine)", rel)
		return true
	}
	fi, err := fs.Stat(fsys, rel)
	if err != nil {
		log("skipped %s (symlink out of its directory or to nothing)", rel)
		return true
	}
	if !fi.Mode().IsRegular() {
		log("skipped %s (not a regular file)", rel)
		return true
	}
	return false
}

func copyFS(fsys fs.FS, dst string, rendered *fileSet, res *Result, log func(string, ...any)) error {
	root, err := os.OpenRoot(dst)
	if err != nil {
		return err
	}
	defer root.Close()
	return walkStatic(fsys, log, func(path string, d fs.DirEntry) error {
		if err := refuseSeparatorName(path); err != nil {
			return err
		}
		if !publishable(path) {
			log("skipped %s (not published)", path)
			return nil
		}
		if skipIrregular(fsys, path, d, log) {
			return nil
		}
		if generatedHostRules(path, rendered) {
			data, err := fs.ReadFile(fsys, path)
			if err != nil {
				return err
			}
			return appendHostRules(root, filepath.FromSlash(path), data, log)
		}
		if keepRendered(path, rendered, log) {
			return nil
		}
		data, err := fs.ReadFile(fsys, path)
		if err != nil {
			return err
		}
		out := filepath.Join(dst, path)
		rel := filepath.FromSlash(path)
		if err := root.MkdirAll(filepath.Dir(rel), 0o755); err != nil {
			return err
		}
		if err := root.WriteFile(rel, data, 0o644); err != nil {
			return err
		}
		res.Assets = append(res.Assets, "/"+filepath.ToSlash(path))
		log("copied %s -> %s", path, out)
		return nil
	})
}
