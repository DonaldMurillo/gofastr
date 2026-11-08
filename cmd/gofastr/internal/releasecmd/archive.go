package releasecmd

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"fmt"
	"io"
	"io/fs"
	"os"
	"time"
)

// archiveFile is one entry of a release archive. Entries sit at the
// archive root, so the install scripts find the binary by its name.
type archiveFile struct {
	name string // entry name: a base name, never a path
	src  string // file whose bytes the entry holds
	mode fs.FileMode
}

// epoch is every entry's modification time. Fixed times, owners and
// order make two archives of the same build byte-identical.
var epoch = time.Unix(0, 0).UTC()

// writeTarGz writes files as a gzip-compressed tar to w.
func writeTarGz(w io.Writer, files []archiveFile) error {
	gz, err := gzip.NewWriterLevel(w, gzip.BestCompression)
	if err != nil {
		return err
	}
	tw := tar.NewWriter(gz)
	for _, f := range files {
		if err := addTarEntry(tw, f); err != nil {
			return err
		}
	}
	if err := tw.Close(); err != nil {
		return err
	}
	return gz.Close()
}

func addTarEntry(tw *tar.Writer, f archiveFile) error {
	src, err := os.Open(f.src)
	if err != nil {
		return err
	}
	defer func() { _ = src.Close() }()
	fi, err := src.Stat()
	if err != nil {
		return err
	}
	if !fi.Mode().IsRegular() {
		return fmt.Errorf("%s is not a regular file", f.src)
	}
	hdr := &tar.Header{
		Typeflag: tar.TypeReg,
		Name:     f.name,
		Mode:     int64(f.mode.Perm()),
		Size:     fi.Size(),
		ModTime:  epoch,
		Format:   tar.FormatPAX,
	}
	if err := tw.WriteHeader(hdr); err != nil {
		return err
	}
	n, err := io.Copy(tw, src)
	if err != nil {
		return err
	}
	if n != fi.Size() {
		return fmt.Errorf("%s changed size while it was archived", f.src)
	}
	return nil
}

// writeZip writes files as a deflated zip to w.
func writeZip(w io.Writer, files []archiveFile) error {
	zw := zip.NewWriter(w)
	for _, f := range files {
		if err := addZipEntry(zw, f); err != nil {
			return err
		}
	}
	return zw.Close()
}

func addZipEntry(zw *zip.Writer, f archiveFile) error {
	src, err := os.Open(f.src)
	if err != nil {
		return err
	}
	defer func() { _ = src.Close() }()
	fi, err := src.Stat()
	if err != nil {
		return err
	}
	if !fi.Mode().IsRegular() {
		return fmt.Errorf("%s is not a regular file", f.src)
	}
	// A zero Modified keeps timestamps out of the archive.
	hdr := &zip.FileHeader{Name: f.name, Method: zip.Deflate}
	hdr.SetMode(f.mode)
	dst, err := zw.CreateHeader(hdr)
	if err != nil {
		return err
	}
	_, err = io.Copy(dst, src)
	return err
}
