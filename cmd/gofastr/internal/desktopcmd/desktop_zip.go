package desktopcmd

import (
	"archive/zip"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"
)

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
		hdr := &zip.FileHeader{Name: name}
		hdr.SetMode(fi.Mode())
		if runtime.GOOS == "windows" {
			mode := fs.FileMode(0o644)
			if d.IsDir() {
				mode = fs.ModeDir | 0o755
			} else if strings.HasPrefix(filepath.ToSlash(rel), "Contents/MacOS/") {
				mode = 0o755
			}
			hdr.SetMode(mode)
		}
		if !d.IsDir() {
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
		_ = f.Close()
		_ = os.Remove(destZip)
		return err
	}
	if err := zw.Close(); err != nil {
		_ = f.Close()
		_ = os.Remove(destZip)
		return err
	}
	return f.Close()
}
