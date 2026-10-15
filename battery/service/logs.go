package service

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/DonaldMurillo/gofastr/internal/fileperm"
)

// maxLogSize is where the daemon trims its own log at start. launchd
// and the Windows path append to one file forever; journald rotates on
// its own.
const maxLogSize = 10 << 20

// trimLog keeps one generation: when path is over maxLogSize it is
// copied to path.1 and truncated in place. Copy-then-truncate, not
// rename, because launchd opened the file before the daemon ran and
// keeps writing through that descriptor (O_APPEND lands at the new
// end).
func trimLog(path string) error {
	fi, err := os.Stat(path)
	if err != nil || fi.Size() <= maxLogSize {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if err := fileperm.WriteOwnerOnly(path+".1", data); err != nil {
		return err
	}
	return os.Truncate(path, 0)
}

// tailBytes bounds how much of the log `logs` reads to find the last
// lines.
const tailBytes = 1 << 20

// tailFile prints the last n lines of path to w and, with follow,
// keeps printing what is appended (starting over after a trim).
// Control bytes are stripped: a log line is data from requests and
// must not drive the terminal.
func tailFile(w io.Writer, path string, n int, follow bool) error {
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		fmt.Fprintf(w, "no log yet at %s\n", path)
		return nil
	}
	if err != nil {
		return fmt.Errorf("service: open log: %w", err)
	}
	defer f.Close()
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
		lines = lines[1:] // first line is cut mid-way
	}
	if n > 0 && len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	if len(lines) > 0 && !(len(lines) == 1 && lines[0] == "") {
		fmt.Fprintln(w, scrubControl(strings.Join(lines, "\n")))
	}
	if !follow {
		return nil
	}
	off := fi.Size()
	for {
		time.Sleep(500 * time.Millisecond)
		fi, err := os.Stat(path)
		if err != nil {
			continue
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
		_, _ = w.Write([]byte(scrubControl(string(bytes.TrimRight(chunk[:nr], "\x00")))))
	}
}
