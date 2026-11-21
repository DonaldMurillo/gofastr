package log

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os"

	"github.com/DonaldMurillo/gofastr/internal/jsonexact"
)

func tailFile(path string, maxLines int) ([]map[string]any, error) {
	if maxLines <= 0 {
		maxLines = 100
	}
	return tailFileWithWindow(path, maxLines, int64(maxLines)*8192)
}

func tailFileWithWindow(path string, maxLines int, window int64) ([]map[string]any, error) {
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

	offset := max(info.Size()-window, 0)
	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		return nil, fmt.Errorf("log: tail seek %s: %w", path, err)
	}

	rd := bufio.NewReaderSize(f, 64*1024)
	skipFirst := offset > 0

	out := make([]map[string]any, 0, maxLines)
	for {
		line, err := readLineCapped(rd, 1<<20)
		if line != nil && !skipFirst {
			var m map[string]any
			if jsonexact.Unmarshal(line, &m) == nil {
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

func readLineCapped(rd *bufio.Reader, limit int) ([]byte, error) {
	var buf []byte
	over := false
	for {
		chunk, err := rd.ReadSlice('\n')
		if !over {
			if len(buf)+len(chunk) > limit+1 {
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
		if len(buf) > limit {
			return nil, err
		}
		if err == io.EOF && len(buf) == 0 {
			return nil, io.EOF
		}
		return buf, err
	}
}
