package telemetry

import (
	"bufio"
	"bytes"
	"io"
	"time"
)

const (
	// maxLine is the longest line a reader returns; a longer one is
	// skipped, never an error.
	maxLine = 1 << 20
	// seekGranule is where the time binary search stops halving and
	// leaves the rest to the scan.
	seekGranule = 64 << 10
	// timeSlop widens a time window on both sides: goroutines stamp a
	// line before they take the write lock, so a file is ordered only
	// to within the time one write waits.
	timeSlop = 2 * time.Second
	// boundEvery is how many lines a windowed scan reads between checks
	// of whether it has left the window. Parsing a time on every line
	// would cost more than the scan itself.
	boundEvery = 512
)

var timePrefix = []byte(`{"time":"`)

// lineTime parses the leading "time" field both file kinds start with.
func lineTime(line []byte) (time.Time, bool) {
	if !bytes.HasPrefix(line, timePrefix) {
		return time.Time{}, false
	}
	rest := line[len(timePrefix):]
	end := bytes.IndexByte(rest, '"')
	if end < 0 {
		return time.Time{}, false
	}
	t, err := time.Parse(time.RFC3339Nano, string(rest[:end]))
	return t, err == nil
}

// lineReader reads newline-terminated lines without copying the ones
// that fit its buffer.
type lineReader struct {
	rd  *bufio.Reader
	buf []byte
}

// A scan reads through a large buffer; a seek probe reads one or two
// lines and would waste it.
const (
	scanBuf  = 256 << 10
	probeBuf = 4 << 10
)

func newLineReader(r io.Reader, size int) *lineReader {
	return &lineReader{rd: bufio.NewReaderSize(r, size)}
}

// next returns the next line without its newline, valid until the next
// call, and the bytes consumed. line is nil for one that is longer than
// maxLine or has no newline yet (a write in progress); n still counts
// it. err is io.EOF at end of input.
func (r *lineReader) next() (line []byte, n int, err error) {
	chunk, err := r.rd.ReadSlice('\n')
	n = len(chunk)
	if err == nil {
		return chunk[:len(chunk)-1], n, nil
	}
	if err != bufio.ErrBufferFull {
		return nil, n, err
	}
	r.buf = append(r.buf[:0], chunk...)
	over := false
	for {
		chunk, err = r.rd.ReadSlice('\n')
		n += len(chunk)
		if !over {
			if len(r.buf)+len(chunk) > maxLine+1 {
				over = true
			} else {
				r.buf = append(r.buf, chunk...)
			}
		}
		if err == bufio.ErrBufferFull {
			continue
		}
		if over || err != nil {
			return nil, n, err
		}
		return r.buf[:len(r.buf)-1], n, nil
	}
}

// scanForward calls fn with each line of f in [from, to), oldest first,
// until fn returns false. from must be a line start.
func scanForward(f io.ReaderAt, from, to int64, fn func(line []byte) bool) error {
	if to <= from {
		return nil
	}
	lr := newLineReader(io.NewSectionReader(f, from, to-from), scanBuf)
	for {
		line, _, err := lr.next()
		if line != nil && !fn(line) {
			return nil
		}
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
	}
}

// scanBackward calls fn with each line of f in [0, end), newest first,
// until fn returns false. A line is valid only during the call.
func scanBackward(f io.ReaderAt, end int64, fn func(line []byte) bool) error {
	const chunk = 1 << 20
	buf := make([]byte, chunk)
	var tail []byte   // end of a line whose start lies before pos
	var joined []byte // b followed by tail, reused across chunks
	skip := true      // dropping bytes until a newline: a torn or over-long line
	for pos := end; pos > 0; {
		n := min(int64(chunk), pos)
		pos -= n
		b := buf[:n]
		if _, err := f.ReadAt(b, pos); err != nil && err != io.EOF {
			return err
		}
		data := b
		if skip {
			i := bytes.LastIndexByte(b, '\n')
			if i < 0 {
				continue
			}
			data, skip = b[:i+1], false
		} else if len(tail) > 0 {
			joined = append(append(joined[:0], b...), tail...)
			data = joined
		}
		tail = tail[:0]
		// data ends with a newline; walk its lines from the last one.
		for stop := len(data) - 1; ; {
			i := bytes.LastIndexByte(data[:stop], '\n')
			if i < 0 && pos > 0 {
				if stop+1 > maxLine {
					skip = true
				} else {
					tail = append(tail, data[:stop+1]...) // data is b or joined, never tail
				}
				break
			}
			if line := data[i+1 : stop]; len(line) <= maxLine && !fn(line) {
				return nil
			}
			if i < 0 {
				break
			}
			stop = i
		}
	}
	return nil
}

// seekTime returns the start of a line at or before the first line of f
// stamped t or later, within seekGranule bytes of it. It assumes lines
// are in time order; callers widen t by timeSlop for the lines that are
// not quite.
func seekTime(f io.ReaderAt, size int64, t time.Time) (int64, error) {
	lo, hi := int64(0), size
	for hi-lo > seekGranule {
		mid := lo + (hi-lo)/2
		start, lt, ok, err := firstTimedLine(f, mid, hi)
		if err != nil {
			return 0, err
		}
		if ok && lt.Before(t) {
			lo = start
		} else {
			hi = mid
		}
	}
	return lo, nil
}

// firstLineAfter returns where the first line stamped after t starts,
// reading on from at, which is the start of a line; size when there is
// none. A line with no time, or too long to read, is passed over.
func firstLineAfter(f io.ReaderAt, size, at int64, t time.Time) (int64, error) {
	lr := newLineReader(io.NewSectionReader(f, at, size-at), probeBuf)
	for off := at; ; {
		line, n, err := lr.next()
		if line != nil {
			if lt, ok := lineTime(line); ok && lt.After(t) {
				return off, nil
			}
		}
		off += int64(n)
		if err == io.EOF {
			return size, nil
		}
		if err != nil {
			return 0, err
		}
	}
}

// firstTimedLine finds the first whole line starting after from, and
// before to, that carries a time.
func firstTimedLine(f io.ReaderAt, from, to int64) (start int64, t time.Time, ok bool, err error) {
	lr := newLineReader(io.NewSectionReader(f, from, to-from), probeBuf)
	off := from
	for first := true; ; first = false {
		line, n, err := lr.next()
		lineStart := off
		off += int64(n)
		// The first read lands mid-line; what it returns is a fragment.
		if !first && line != nil {
			if lt, ok := lineTime(line); ok {
				return lineStart, lt, true, nil
			}
		}
		if err == io.EOF {
			return 0, time.Time{}, false, nil
		}
		if err != nil {
			return 0, time.Time{}, false, err
		}
	}
}
