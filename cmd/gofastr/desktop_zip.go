package main

// The pure-Go zipper for a built .app bundle: archive/zip only, no
// ditto or zip on PATH. Entry names are relative to the bundle's
// parent (Notes.app/...), directories are included as their own
// entries, file modes are preserved (the executable bit under
// Contents/MacOS is what LaunchServices launches), mtimes are zeroed
// so two builds of the same tree zip to identical bytes, and symlinks
// are refused: one inside a bundle is either a mistake or an attempt
// to make a reader resolve outside the tree.

import (
	"archive/zip"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
)

// zipAppBundle writes the bundle at appDir to destZip with entry names
// rooted at the bundle's own base name.
func zipAppBundle(appDir, destZip string) error {
	appDir = filepath.Clean(appDir)
	root := filepath.Base(appDir)
	//gofastr:allow(worldreadable) a distributed archive is a public artifact: it is the notary submission and the download, holds no state or secret
	f, err := os.OpenFile(destZip, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()

	zw := zip.NewWriter(f)
	// WalkDir visits entries in lexical order within each directory, so
	// the archive layout only depends on the tree, never on readdir
	// order.
	err = filepath.WalkDir(appDir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		fi, err := d.Info()
		if err != nil {
			return err
		}
		if fi.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("refusing to zip symlink %s: a bundle must not contain symlinks", p)
		}
		rel, err := filepath.Rel(appDir, p)
		if err != nil {
			return err
		}
		name := path.Clean(path.Join(root, filepath.ToSlash(rel)))
		if d.IsDir() {
			name += "/"
		} else if !fi.Mode().IsRegular() {
			return fmt.Errorf("refusing to zip %s: not a regular file (mode %s)", p, fi.Mode())
		}
		// Zero Modified: the archive carries no timestamps, which is
		// what makes two zips of the same tree byte-identical.
		hdr := &zip.FileHeader{Name: name}
		hdr.SetMode(fi.Mode())
		if !d.IsDir() {
			// Deflate: the bundle is mostly one 20 MB binary, and a
			// stored archive is the download the updater ships.
			hdr.Method = zip.Deflate
		}
		w, err := zw.CreateHeader(hdr)
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil // a directory entry carries no data
		}
		src, err := os.Open(p)
		if err != nil {
			return err
		}
		defer src.Close()
		_, err = io.Copy(w, src)
		return err
	})
	if err != nil {
		_ = zw.Close()
		_ = os.Remove(destZip)
		return err
	}
	if err := zw.Close(); err != nil {
		_ = os.Remove(destZip)
		return err
	}
	return f.Close()
}
