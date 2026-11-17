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

// staticSourceFile reports whether the host's static source holds a
// regular file at rel, a valid fs path (a directory named like a page
// does not count).
func (b *Builder) staticSourceFile(rel string) bool {
	if dir := b.Host.StaticDir(); dir != "" {
		return isFile(os.DirFS(dir), rel)
	}
	if fsys := b.Host.StaticFS(); fsys != nil {
		return isFile(fsys, rel)
	}
	return false
}

// isFile reports whether fsys holds a regular file at name.
func isFile(fsys fs.FS, name string) bool {
	if !fs.ValidPath(name) {
		return false
	}
	info, err := fs.Stat(fsys, name)
	return err == nil && info.Mode().IsRegular()
}

// staticSourceHas reports whether the host's static asset source (dir or
// embedded FS) provides rel, meaning the user shipped their own copy.
//
// The spelling must match: on a case-insensitive filesystem a static
// MANIFEST.webmanifest opened as manifest.webmanifest, the generated
// file was skipped, and a case-sensitive host had no manifest. The other
// spelling is copied as itself and refused as a case twin.
func (b *Builder) staticSourceHas(rel string) bool {
	if dir := b.Host.StaticDir(); dir != "" {
		return spelledIn(os.DirFS(dir), filepath.ToSlash(rel))
	}
	if fsys := b.Host.StaticFS(); fsys != nil {
		return spelledIn(fsys, filepath.ToSlash(rel))
	}
	return false
}

// spelledIn reports whether fsys holds rel, a slash path, under exactly
// that spelling: each directory listing names each segment.
func spelledIn(fsys fs.FS, rel string) bool {
	// Absent in any spelling is the common answer, and one Stat gives it.
	// Any other Stat error (an adapter whose files read but do not stat)
	// leaves the answer to the listings.
	if _, err := fs.Stat(fsys, rel); errors.Is(err, fs.ErrNotExist) {
		return false
	}
	dir := "."
	for _, name := range strings.Split(rel, "/") {
		ents, err := fs.ReadDir(fsys, dir)
		if err != nil || !slices.ContainsFunc(ents, func(e fs.DirEntry) bool { return e.Name() == name }) {
			return false
		}
		dir = path.Join(dir, name)
	}
	return true
}

// dumpExtraDirs copies every Builder.ExtraDirs filesystem under its mount
// path. Deterministic (sorted mounts, sorted walk) and non-clobbering:
// a file this build already wrote (a page, a framework asset, an earlier
// mount's file) or one the static SOURCE provides wins. What decides is
// this build and the source, never what sits in OutDir: Watch rebuilds
// into the same directory, and the previous build's copy of an extra
// file must not keep the new one out.
func (b *Builder) dumpExtraDirs(res *Result, rendered *fileSet) error {
	// Keyed folded, like every lookup below.
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
			if skipSymlink(fsys, p, d, b.log) {
				return nil
			}
			dst := filepath.Join(b.OutDir, filepath.FromSlash(rel))
			if err := b.ensureContained(dst); err != nil {
				return err
			}
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

// keepRendered reports whether the static source's file at rel (a slash
// path) is refused because the build wrote a file there, in any case,
// and logs the refusal. Any other file, an index.html under a path no
// page takes included, is copied.
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

// publishable reports whether rel, a slash path in the export, is copied
// into the public tree: not a path core/static refuses (a dot segment
// such as .env or .git/config, or a server config file), the same set
// the UI host refuses to serve. A leading .well-known/ is the public
// namespace RFC 8615 reserves, and the root .nojekyll is the marker
// GitHub Pages reads; both are copied.
func publishable(rel string) bool {
	if rel == ".nojekyll" {
		return true
	}
	return !corestatic.Refuses(strings.TrimPrefix(rel, ".well-known/"))
}

// generatedHostRules reports whether rel (a slash path) is a host rule
// file the build wrote: the root _headers, always, and the root
// _redirects when the app has redirects. A static source's copy of one is
// appended to it rather than refused, so the app's rules (Cache-Control,
// HSTS, legacy redirects) apply and the generated ones, the header-only
// frame-ancestors guard among them, stay first.
func generatedHostRules(rel string, rendered *fileSet) bool {
	return (rel == "_headers" || rel == "_redirects") && rendered.hasExact(rel)
}

// appendHostRules appends the static source's rules to the generated
// file at rel. Both generated files end in a newline, so the first
// appended line starts a line of its own.
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
	// Every read goes through an os.Root over src, the way the UI host
	// reads the static dir: a symlink pointing out of the tree is refused
	// in the kernel, so the export never publishes a file the live host
	// answers 404 for. os.Open followed it.
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

// walkStatic walks a static tree the way the UI host serves it, calling
// fn for every entry that is not a directory, slash-separated and
// relative to the tree's root. In an os.Root view a symlink resolves
// only inside the tree; any other FS (an embed.FS holds no links) gets
// no directory link walked, since nothing holds that walk inside it.
// fs.WalkDir does not descend a symlinked directory, and the live host
// serves static/images/logo.svg through static/images -> img all the
// same: the export dropped every asset under such a link and the
// published site 404'd on them. A directory link that resolves inside
// the tree is walked under its own name; one that escapes, or names
// nothing, reaches fn like any other link, which refuses it. A link to
// a directory the walk is already inside (loop -> .) is skipped: the
// host would serve loop/loop/... forever, and no export holds that.
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
				// Only an os.Root view holds a linked directory's walk
				// inside the root. Elsewhere the link goes to fn, whose
				// skipSymlink drops it.
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

// openSourceFile opens rel, a walked static-dir entry, through srcRoot.
// A non-empty reason means the entry is not published, the way the UI
// host answers 404 for it: a symlink whose target leaves the tree (or
// no longer exists), and anything that is not a regular file.
func openSourceFile(srcRoot *os.Root, rel string, d fs.DirEntry) (*os.File, string, error) {
	f, err := srcRoot.Open(rel)
	if err != nil {
		if d.Type()&fs.ModeSymlink != 0 {
			return nil, "symlink out of the static dir or to nothing", nil
		}
		return nil, "", err
	}
	st, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return nil, "", err
	}
	if !st.Mode().IsRegular() {
		_ = f.Close()
		return nil, "not a regular file", nil
	}
	return f, "", nil
}

// skipSymlink reports whether the walked entry at rel is a symlink the
// export does not publish, and logs why. A symlink in a disk-backed FS
// (SetStaticFS and Builder.ExtraDirs re-root os.DirFS through os.Root)
// is published only when it resolves to a regular file inside the tree,
// as openSourceFile decides for the static dir; a read of one that
// escapes failed the whole build. One that resolves to a directory in
// the tree never reaches here: walkStatic walks it.
func skipSymlink(fsys fs.FS, rel string, d fs.DirEntry, log func(string, ...any)) bool {
	if d.Type()&fs.ModeSymlink == 0 {
		return false
	}
	// Only an os.Root view keeps the link's target inside the directory.
	// Any other FS, fs.Sub(os.DirFS(dir), ".") among them, follows it
	// anywhere on disk, and nothing here can tell where it lands.
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
		if skipSymlink(fsys, path, d, log) {
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
