package telemetry

import (
	"fmt"
	"io/fs"
	"os"
	"slices"
	"strings"
	"time"
)

// Store reads one directory of log and trace files. It holds no state
// beyond the directory handle, so it is safe for concurrent use and
// sees whatever the writer has appended by the time a query runs.
type Store struct {
	cfg  Config
	root *os.Root
}

// Open opens the store at cfg.Dir, which must exist.
func Open(cfg Config) (*Store, error) {
	cfg, err := cfg.Resolve()
	if err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(cfg.Dir)
	if err != nil {
		return nil, fmt.Errorf("telemetry: open %s: %w", cfg.Dir, err)
	}
	return &Store{cfg: cfg, root: root}, nil
}

// Dir returns the directory the store reads.
func (s *Store) Dir() string { return s.cfg.Dir }

// Close releases the directory handle.
func (s *Store) Close() error { return s.root.Close() }

// dayFile is one day's file of one kind.
type dayFile struct {
	name string
	day  time.Time // 00:00 UTC
}

// files lists kind's day files overlapping [from, to], newest first. A
// zero from or to leaves that side open.
func (s *Store) files(kind string, from, to time.Time) ([]dayFile, error) {
	entries, err := fs.ReadDir(s.root.FS(), ".")
	if err != nil {
		return nil, fmt.Errorf("telemetry: list %s: %w", s.cfg.Dir, err)
	}
	var out []dayFile
	for _, e := range entries {
		stamp, ok := strings.CutPrefix(e.Name(), kind+"-")
		if !ok || !e.Type().IsRegular() {
			continue
		}
		stamp, ok = strings.CutSuffix(stamp, fileExt)
		if !ok {
			continue
		}
		day, err := time.Parse(dayLayout, stamp)
		if err != nil {
			continue
		}
		if !to.IsZero() && day.After(to) {
			continue
		}
		if !from.IsZero() && !day.Add(24*time.Hour).After(from) {
			continue
		}
		out = append(out, dayFile{name: e.Name(), day: day})
	}
	slices.SortFunc(out, func(a, b dayFile) int { return b.day.Compare(a.day) })
	return out, nil
}

// window is a file opened and narrowed to the bytes that can hold lines
// stamped in [from, to].
type window struct {
	f          *os.File
	start, end int64
}

func (s *Store) open(df dayFile, from, to time.Time) (window, error) {
	f, err := s.root.Open(df.name)
	if err != nil {
		return window{}, fmt.Errorf("telemetry: open %s: %w", df.name, err)
	}
	info, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return window{}, fmt.Errorf("telemetry: stat %s: %w", df.name, err)
	}
	w := window{f: f, end: info.Size()}
	// A side of the window that lies outside this file's day needs no
	// search: the whole file is on the inside of it.
	if !from.IsZero() && from.Add(-timeSlop).After(df.day) {
		if w.start, err = seekTime(f, info.Size(), from.Add(-timeSlop)); err != nil {
			_ = f.Close()
			return window{}, fmt.Errorf("telemetry: seek %s: %w", df.name, err)
		}
	}
	if !to.IsZero() && to.Add(timeSlop).Before(df.day.Add(24*time.Hour)) {
		// seekTime stops short of its target by up to a granule; the
		// scan's own bound check trims the overshoot this adds.
		at, err := seekTime(f, info.Size(), to.Add(timeSlop))
		if err != nil {
			_ = f.Close()
			return window{}, fmt.Errorf("telemetry: seek %s: %w", df.name, err)
		}
		w.end = min(info.Size(), at+2*seekGranule)
	}
	return w, nil
}
