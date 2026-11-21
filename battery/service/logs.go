package service

import (
	"bytes"
	"fmt"
	"github.com/DonaldMurillo/gofastr/core/textsafe"
	"io"
	"os"
	"strings"
	"time"

	"github.com/DonaldMurillo/gofastr/internal/fileperm"
)

const maxLogSize = 10 << 20

func trimLog(path string) error {
	fi, err := os.Stat(path)
	if err != nil || fi.Size() <= maxLogSize {
		return nil
	}
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	data := make([]byte, maxLogSize)
	if _, err := io.ReadFull(io.NewSectionReader(file, fi.Size()-maxLogSize, maxLogSize), data); err != nil {
		return err
	}
	if err := fileperm.WriteOwnerOnly(path+".1", data); err != nil {
		return err
	}
	return os.Truncate(path, 0)
}

const tailBytes = 1 << 20

func tailFile(w io.Writer, path string, n int, follow bool) error {
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		fmt.Fprintf(w, "no log yet at %s\n", path)
		return nil
	}
	if err != nil {
		return fmt.Errorf("service: open log: %w", err)
	}
	defer func() { f.Close() }()
	fi, err := f.Stat()
	if err != nil {
		return err
	}
	start := max(fi.Size()-tailBytes, 0)
	buf := make([]byte, fi.Size()-start)
	if _, err := f.ReadAt(buf, start); err != nil && err != io.EOF {
		return err
	}
	lines := strings.Split(strings.TrimRight(string(buf), "\n"), "\n")
	if start > 0 && len(lines) > 0 {
		lines = lines[1:]
	}
	if n > 0 && len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	if len(lines) > 0 && !(len(lines) == 1 && lines[0] == "") {
		fmt.Fprintln(w, textsafe.ScrubTerminal(strings.Join(lines, "\n")))
	}
	if !follow {
		return nil
	}
	off := fi.Size()
	cur := fi
	for {
		time.Sleep(500 * time.Millisecond)
		fi, err := os.Stat(path)
		if err != nil {
			continue
		}
		if !os.SameFile(cur, fi) {
			nf, err := os.Open(path)
			if err != nil {
				continue
			}
			if nfi, err := nf.Stat(); err != nil || !os.SameFile(nfi, fi) {
				nf.Close()
				continue
			}
			f.Close()
			f, cur, off = nf, fi, 0
		}
		if fi.Size() < off {
			off = 0
		}
		if fi.Size() == off {
			continue
		}
		chunk := make([]byte, fi.Size()-off)
		nr, err := f.ReadAt(chunk, off)
		if err != nil && err != io.EOF {
			return err
		}
		off += int64(nr)
		_, _ = w.Write([]byte(textsafe.ScrubTerminal(string(bytes.TrimRight(chunk[:nr], "\x00")))))
	}
}
