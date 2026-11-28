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

type archiveFile struct {
	name string
	src  string
	mode fs.FileMode
}

var epoch = time.Unix(0, 0).UTC()

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
	hdr := &zip.FileHeader{Name: f.name, Method: zip.Deflate}
	hdr.SetMode(f.mode)
	dst, err := zw.CreateHeader(hdr)
	if err != nil {
		return err
	}
	_, err = io.Copy(dst, src)
	return err
}
