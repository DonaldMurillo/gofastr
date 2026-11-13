package telemetry

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/DonaldMurillo/gofastr/internal/fileperm"
)

// ErrClosed is returned by a write after Close.
var ErrClosed = errors.New("telemetry: writer closed")

// ErrDiskFull is returned while the current day's files alone fill
// MaxDiskBytes: there is nothing older left to remove, so new lines are
// dropped and counted until the day turns or the cap is raised.
var ErrDiskFull = errors.New("telemetry: disk cap reached")

// pruneEvery is how many bytes are written between checks of the disk
// cap, which is how far past the cap the directory can get.
const pruneEvery = 8 << 20

// Writer appends log lines and traces to the day's files and keeps the
// directory inside Retention and MaxDiskBytes. Each line is one write
// call on a file opened for append, with no buffer in between: a line
// WriteLog has returned from survives the process being killed.
type Writer struct {
	cfg  Config
	root *os.Root
	now  func() time.Time

	logs, traces dayFileWriter

	sinceCheck atomic.Int64
	pruning    sync.Mutex
	full       atomic.Bool
	closed     atomic.Bool

	logLines, logBytes     atomic.Int64
	traceLines, traceBytes atomic.Int64
	dropped, removed       atomic.Int64
}

// WriterStats counts what a Writer has done since it was opened.
type WriterStats struct {
	LogLines, LogBytes int64
	Traces, TraceBytes int64
	// Dropped is the lines refused because the disk cap was reached or
	// a write failed.
	Dropped int64
	// FilesRemoved is the day files deleted for age or for the cap.
	FilesRemoved int64
}

type dayFileWriter struct {
	kind string
	mu   sync.Mutex
	day  int64 // days since the epoch, UTC
	f    *os.File
}

// NewWriter opens cfg.Dir for writing, creating it owner-only if it is
// missing, and removes what Retention and MaxDiskBytes no longer allow.
func NewWriter(cfg Config) (*Writer, error) {
	return newWriter(cfg, time.Now)
}

func newWriter(cfg Config, now func() time.Time) (*Writer, error) {
	cfg, err := cfg.Resolve()
	if err != nil {
		return nil, err
	}
	if err := fileperm.MkdirPrivate(cfg.Dir); err != nil {
		return nil, fmt.Errorf("telemetry: mkdir %s: %w", cfg.Dir, err)
	}
	root, err := os.OpenRoot(cfg.Dir)
	if err != nil {
		return nil, fmt.Errorf("telemetry: open %s: %w", cfg.Dir, err)
	}
	w := &Writer{cfg: cfg, root: root, now: now}
	w.logs.kind, w.traces.kind = KindLogs, KindTraces
	if err := w.prune(); err != nil {
		_ = root.Close()
		return nil, err
	}
	return w, nil
}

// Stats returns the counters.
func (w *Writer) Stats() WriterStats {
	return WriterStats{
		LogLines: w.logLines.Load(), LogBytes: w.logBytes.Load(),
		Traces: w.traceLines.Load(), TraceBytes: w.traceBytes.Load(),
		Dropped: w.dropped.Load(), FilesRemoved: w.removed.Load(),
	}
}

// WriteLog appends one log entry: a JSON object with "time" first and
// no newline, which is what battery/log hands a sink. It keeps entry.
func (w *Writer) WriteLog(entry []byte) error {
	n, err := w.write(&w.logs, append(entry, '\n'))
	if err == nil {
		w.logLines.Add(1)
		w.logBytes.Add(int64(n))
	}
	return err
}

var lineBufs = sync.Pool{New: func() any { return new([]byte) }}

// WriteTrace appends one complete trace.
func (w *Writer) WriteTrace(t Trace) error {
	bp := lineBufs.Get().(*[]byte)
	*bp = AppendTrace((*bp)[:0], t)
	n, err := w.write(&w.traces, *bp)
	if cap(*bp) <= 64<<10 {
		lineBufs.Put(bp)
	}
	if err == nil {
		w.traceLines.Add(1)
		w.traceBytes.Add(int64(n))
	}
	return err
}

func (w *Writer) write(d *dayFileWriter, line []byte) (int, error) {
	if w.closed.Load() {
		return 0, ErrClosed
	}
	now := w.now().UTC()
	day := now.Unix() / 86400
	d.mu.Lock()
	turned := d.f != nil && d.day != day
	if w.full.Load() && !turned {
		d.mu.Unlock()
		w.dropped.Add(1)
		return 0, ErrDiskFull
	}
	if d.f == nil || turned {
		if d.f != nil {
			_ = d.f.Close()
			d.f = nil
		}
		// os.Root refuses a name that leaves the directory, a planted
		// symlink included.
		f, err := w.root.OpenFile(FileName(d.kind, now), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
		if err != nil {
			d.mu.Unlock()
			w.dropped.Add(1)
			return 0, fmt.Errorf("telemetry: open day file: %w", err)
		}
		d.f, d.day = f, day
	}
	n, err := d.f.Write(line)
	d.mu.Unlock()
	if err != nil {
		w.dropped.Add(1)
		return n, fmt.Errorf("telemetry: write: %w", err)
	}
	if turned || w.sinceCheck.Add(int64(n)) >= pruneEvery {
		w.sinceCheck.Store(0)
		// A failed prune leaves the directory as it was; the next
		// check tries again, and the write itself succeeded.
		_ = w.prune()
	}
	return n, nil
}

// prune removes day files past Retention, then the oldest remaining
// ones while the directory is over MaxDiskBytes. Today's files are
// never removed; if they alone are over the cap, writes stop.
func (w *Writer) prune() error {
	w.pruning.Lock()
	defer w.pruning.Unlock()
	entries, err := fs.ReadDir(w.root.FS(), ".")
	if err != nil {
		return fmt.Errorf("telemetry: list %s: %w", w.cfg.Dir, err)
	}
	now := w.now().UTC()
	today := now.Truncate(24 * time.Hour)
	type file struct {
		name string
		day  time.Time
		size int64
	}
	var files []file
	var total int64
	for _, e := range entries {
		kind, rest, ok := strings.Cut(e.Name(), "-")
		stamp, isDay := strings.CutSuffix(rest, fileExt)
		if !ok || !isDay || (kind != KindLogs && kind != KindTraces) || !e.Type().IsRegular() {
			continue
		}
		day, err := time.Parse(dayLayout, stamp)
		if err != nil {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue // removed since the listing
		}
		files = append(files, file{e.Name(), day, info.Size()})
		total += info.Size()
	}
	slices.SortFunc(files, func(a, b file) int { return a.day.Compare(b.day) })
	for _, f := range files {
		expired := !f.day.Add(24 * time.Hour).After(now.Add(-w.cfg.Retention))
		if f.day.Before(today) && (expired || total > w.cfg.MaxDiskBytes) {
			if err := w.root.Remove(f.name); err != nil {
				return fmt.Errorf("telemetry: remove %s: %w", f.name, err)
			}
			total -= f.size
			w.removed.Add(1)
		}
	}
	w.full.Store(total > w.cfg.MaxDiskBytes)
	return nil
}

// Close closes the day files. Writes after it return ErrClosed.
func (w *Writer) Close() error {
	if w.closed.Swap(true) {
		return nil
	}
	var errs []error
	for _, d := range []*dayFileWriter{&w.logs, &w.traces} {
		d.mu.Lock()
		if d.f != nil {
			errs = append(errs, d.f.Close())
			d.f = nil
		}
		d.mu.Unlock()
	}
	return errors.Join(append(errs, w.root.Close())...)
}
