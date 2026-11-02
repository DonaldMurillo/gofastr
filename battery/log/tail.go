package log

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os"

	"github.com/DonaldMurillo/gofastr/internal/jsonexact"
)

// tailFile returns up to maxLines decoded log entries from the end of
// the file at path, in chronological order (oldest first within the
// returned window). Each entry is parsed as JSON; non-JSON / partial
// lines are skipped.
//
// Used by the log MCP tools for historical queries that fall outside
// the in-memory RingSink's retention window. Caps and seek are
// approximate: we read the last maxLines*8 KiB by default, which
// covers ~typical access entries comfortably. Tune via the second
// parameter if you have unusually large entries.
//
// Returns (nil, nil) when the file doesn't exist, agents asking for
// historical logs against an app that hasn't enabled a file sink should
// get an empty list, not an error.
func tailFile(path string, maxLines int) ([]map[string]any, error) {
	if maxLines <= 0 {
		maxLines = 100
	}
	return tailFileWithWindow(path, maxLines, int64(maxLines)*8192)
}

func tailFileWithWindow(path string, maxLines int, window int64) ([]map[string]any, error) {
	// Refuse symlinks: a writable parent dir could let another user
	// swap the log path for a pointer to /etc/shadow or another
	// process's log and exfiltrate via the MCP historical reader.
	if li, err := os.Lstat(path); err == nil {
		if li.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("log: tail refuses symlink at %s", path)
		}
	}
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("log: tail open %s: %w", path, err)
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return nil, fmt.Errorf("log: tail stat %s: %w", path, err)
	}

	// Seek to (size - window), or 0 if the file is smaller.
	offset := max(info.Size()-window, 0)
	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		return nil, fmt.Errorf("log: tail seek %s: %w", path, err)
	}

	// Lines up to 1 MiB are read; the recovery middleware caps stacks at
	// 64 KiB and panic values at 4 KiB, so this is comfortable. A longer
	// line (an app can log a large attr; the fanout caps nothing) is
	// skipped, not fatal: bufio.Scanner ended the whole scan on one with
	// ErrTooLong, and the historical read returned nothing while that
	// line sat in the window.
	rd := bufio.NewReaderSize(f, 64*1024)
	// If we seeked into the middle of a line, discard the partial one.
	skipFirst := offset > 0

	// Accumulate. Drop oldest if we exceed maxLines.
	out := make([]map[string]any, 0, maxLines)
	for {
		line, err := readLineCapped(rd, 1<<20)
		if line != nil && !skipFirst {
			var m map[string]any
			if jsonexact.Unmarshal(line, &m) == nil { // ignore malformed lines
				if len(out) == maxLines {
					out = out[1:]
				}
				out = append(out, m)
			}
		}
		skipFirst = false
		if err == io.EOF {
			return out, nil
		}
		if err != nil {
			return nil, fmt.Errorf("log: tail scan %s: %w", path, err)
		}
	}
}

// readLineCapped returns the next '\n'-terminated line of rd without its
// terminator, or nil when the line is longer than limit (the line is
// consumed either way). At end of input it returns the final unterminated
// line, if any, with io.EOF.
func readLineCapped(rd *bufio.Reader, limit int) ([]byte, error) {
	var buf []byte
	over := false
	for {
		chunk, err := rd.ReadSlice('\n')
		if !over {
			if len(buf)+len(chunk) > limit+1 { // +1: the '\n' is not content
				over, buf = true, nil
			} else {
				buf = append(buf, chunk...)
			}
		}
		if err == bufio.ErrBufferFull {
			continue
		}
		if over {
			return nil, err
		}
		buf = bytes.TrimSuffix(buf, []byte{'\n'})
		if err == io.EOF && len(buf) == 0 {
			return nil, io.EOF
		}
		return buf, err
	}
}
