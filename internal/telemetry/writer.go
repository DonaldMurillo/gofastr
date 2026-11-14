package telemetry

import (
	"bytes"
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

	logs, traces, stats, host dayFileWriter

	sinceCheck atomic.Int64
	pruning    sync.Mutex
	full       atomic.Bool
	closed     atomic.Bool
	trimmed    atomic.Int64
	// lastPrune is when the directory was last measured, in Unix
	// seconds: a full directory is measured again once a minute.
	lastPrune atomic.Int64
	// bgMu orders a sealing pass starting against Close waiting for
	// the passes that have started.
	bgMu sync.Mutex

	// sealing is held by the one pass that packs finished days; bg
	// counts it, so Close can wait for it to step out.
	sealing sync.Mutex
	// flushing orders FlushStats calls: lines reach the file in time
	// order.
	flushing sync.Mutex
	bg       sync.WaitGroup
	sealed   atomic.Int64
	noSeal   bool

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
	// DaysSealed is the finished days of one kind packed into blocks.
	DaysSealed int64
	// Trimmed is the traces written without their attributes, and
	// without their later steps where that was not enough, because
	// whole they were longer than a reader returns.
	Trimmed int64
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
	return newWriter(cfg, time.Now, true)
}

// newWriter is NewWriter with the clock and the sealing pass in the
// caller's hands: a test of pruning counts plain files, which a pass
// in the background would be packing while it counts.
func newWriter(cfg Config, now func() time.Time, seal bool) (*Writer, error) {
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
	w := &Writer{cfg: cfg, root: root, now: now, noSeal: !seal}
	w.logs.kind, w.traces.kind, w.stats.kind, w.host.kind = KindLogs, KindTraces, KindStats, KindHost
	if err := w.prune(); err != nil {
		_ = root.Close()
		return nil, err
	}
	w.sealPast()
	return w, nil
}

// Stats returns the counters.
func (w *Writer) Stats() WriterStats {
	return WriterStats{
		LogLines: w.logLines.Load(), LogBytes: w.logBytes.Load(),
		Traces: w.traceLines.Load(), TraceBytes: w.traceBytes.Load(),
		Dropped: w.dropped.Load(), FilesRemoved: w.removed.Load(), DaysSealed: w.sealed.Load(),
		Trimmed: w.trimmed.Load(),
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
	// A reader skips a line longer than maxLine, and sealing drops it:
	// a trace written whole at that size was counted as kept and found
	// by nothing. Its attributes go first (the steps and their times
	// are what the page draws), then the later half of its steps, until
	// the line fits.
	if len(*bp) > maxLine {
		w.trimmed.Add(1)
		t.Spans = slices.Clone(t.Spans)
		for i := range t.Spans {
			t.Spans[i].Attrs = nil
		}
		*bp = AppendTrace((*bp)[:0], t)
		for len(*bp) > maxLine && len(t.Spans) > 1 {
			t.Spans = t.Spans[:(len(t.Spans)+1)/2]
			*bp = AppendTrace((*bp)[:0], t)
		}
	}
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

// WriteStats appends the lines Rollup.Take returned: whole lines, in
// time order.
func (w *Writer) WriteStats(lines []byte) error {
	if len(lines) == 0 {
		return nil
	}
	_, err := w.write(&w.stats, lines)
	return err
}

// FlushStats writes r's finished minutes, or every minute when all is
// set (the app is stopping), to the day's stats file.
func (w *Writer) FlushStats(r *Rollup, all bool) error {
	w.flushing.Lock()
	defer w.flushing.Unlock()
	// Take empties the minutes it returns. A writer that would refuse
	// them leaves them where they are, to be written when it can: the
	// counts are exact, and a refused write lost them for good. While
	// the directory stays full they are kept for an hour, then let go,
	// so the map does not grow all day.
	if w.closed.Load() {
		return ErrClosed
	}
	if w.full.Load() && !all {
		if stale := r.Take(w.now().Add(-time.Hour), false); len(stale) > 0 {
			w.dropped.Add(int64(bytes.Count(stale, []byte{'\n'})))
		}
		if w.remeasure(w.now()); w.full.Load() {
			return ErrDiskFull
		}
	}
	return w.WriteStats(r.Take(w.now(), all))
}

func (w *Writer) write(d *dayFileWriter, line []byte) (int, error) {
	d.mu.Lock()
	// Asked under the lock: Close sets closed and then takes this lock
	// to close the file, so a write that got here first finishes, and
	// one that gets here after is refused instead of opening the file
	// again behind a closed writer.
	if w.closed.Load() {
		d.mu.Unlock()
		return 0, ErrClosed
	}
	// The clock is read under the lock too. Read before it, a write
	// stamped a millisecond before midnight could take the lock after
	// another had turned the day, and open yesterday's file again: a
	// day already packed, whose plain file no reader looks at and the
	// next pass deletes. A day never goes backwards for the same
	// reason when the clock itself steps back: the line goes in the
	// file that is open.
	now := w.now().UTC()
	day := now.Unix() / 86400
	if d.f != nil && day < d.day {
		day = d.day
	}
	turned := d.f != nil && d.day != day
	if w.full.Load() && !turned {
		d.mu.Unlock()
		w.dropped.Add(1)
		w.remeasure(now)
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
	if turned {
		w.sealPast()
	}
	return n, nil
}

// remeasure measures a full directory again, at most once a minute:
// room made since (a day removed by hand, a pass that packed one) is
// found without waiting for midnight.
func (w *Writer) remeasure(now time.Time) {
	if last := w.lastPrune.Load(); now.Unix()-last >= 60 && w.lastPrune.CompareAndSwap(last, now.Unix()) {
		_ = w.prune()
	}
}

// errSealStopped ends a sealing pass early: the writer is closing.
var errSealStopped = errors.New("telemetry: sealing stopped")

// sealPast packs every finished day's plain files into blocks, and
// rebuilds the index of a sealed day that has lost its own, in the
// background, one pass at a time. It runs when the writer opens and
// when the day turns. Between blocks it rests for twice as long as the
// block took, so on one core it leaves two thirds of the time to the
// app, and it stops at the next block when the writer closes. A day it
// cannot seal stays plain and readable, and the next pass tries again.
func (w *Writer) sealPast() {
	if w.noSeal {
		return
	}
	// Close sets closed, then takes bgMu before it waits: a pass either
	// started before that, and is waited for, or sees closed here.
	w.bgMu.Lock()
	if w.closed.Load() || !w.sealing.TryLock() {
		w.bgMu.Unlock()
		return
	}
	w.bg.Add(1)
	w.bgMu.Unlock()
	go func() {
		defer w.bg.Done()
		defer w.sealing.Unlock()
		entries, err := fs.ReadDir(w.root.FS(), ".")
		if err != nil {
			return
		}
		today := w.now().UTC().Truncate(24 * time.Hour)
		rest := func(took time.Duration) error {
			if w.closed.Load() {
				return errSealStopped
			}
			time.Sleep(min(2*took, 200*time.Millisecond))
			return nil
		}
		for _, e := range entries {
			kind, day, ext, ok := splitName(e.Name())
			if !ok || !day.Before(today) || w.closed.Load() {
				continue
			}
			switch ext {
			case extPlain:
				if err := sealDay(w.root, kind, day, rest); err == nil {
					w.sealed.Add(1)
				}
			case extData:
				// A sealed day whose index was lost or damaged
				// is rebuilt from its blocks. A failure leaves
				// it as it was for the next pass.
				_ = repairDay(w.root, kind, day)
			}
		}
		// Sealing shrinks the directory: the cap may have room again.
		_ = w.prune()
	}()
}

// prune removes days past Retention, then the oldest remaining days
// while the directory is over MaxDiskBytes. A day goes whole: its plain
// file or its blocks, summaries and index. Today's files are never
// removed; if they alone are over the cap, writes stop.
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
		_, day, _, ok := splitName(e.Name())
		if !ok || !e.Type().IsRegular() {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue // removed since the listing
		}
		files = append(files, file{e.Name(), day, info.Size()})
		// A file being packed is the day counted a second time, for as
		// long as the pass runs: with the directory near its cap that
		// removed a day more than the cap asked, or refused new lines.
		if !strings.HasSuffix(e.Name(), extTmp) {
			total += info.Size()
		}
	}
	slices.SortStableFunc(files, func(a, b file) int { return a.day.Compare(b.day) })
	for i := 0; i < len(files); {
		// One day's files, of every kind, go or stay together.
		j, daySize := i, int64(0)
		for j < len(files) && files[j].day.Equal(files[i].day) {
			daySize += files[j].size
			j++
		}
		day := files[i].day
		expired := !day.Add(24 * time.Hour).After(now.Add(-w.cfg.Retention))
		if day.Before(today) && (expired || total > w.cfg.MaxDiskBytes) {
			for _, f := range files[i:j] {
				if err := w.root.Remove(f.name); err != nil && !errors.Is(err, os.ErrNotExist) {
					return fmt.Errorf("telemetry: remove %s: %w", f.name, err)
				}
				w.removed.Add(1)
			}
			total -= daySize
		}
		i = j
	}
	w.full.Store(total > w.cfg.MaxDiskBytes)
	return nil
}

// Close stops any sealing pass and closes the day files. Writes after
// it return ErrClosed.
func (w *Writer) Close() error {
	if w.closed.Swap(true) {
		return nil
	}
	w.bgMu.Lock() // no pass starts from here on
	w.bgMu.Unlock()
	w.bg.Wait() // a sealing pass stops at its next block
	var errs []error
	for _, d := range []*dayFileWriter{&w.logs, &w.traces, &w.stats, &w.host} {
		d.mu.Lock()
		if d.f != nil {
			errs = append(errs, d.f.Close())
			d.f = nil
		}
		d.mu.Unlock()
	}
	return errors.Join(append(errs, w.root.Close())...)
}
