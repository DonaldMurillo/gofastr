package telemetry

import (
	"bytes"
	"errors"
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

// dayRef is one day of one kind: sealed into blocks, or still a plain
// file.
type dayRef struct {
	base   string // "<kind>-YYYYMMDD"
	day    time.Time
	sealed bool
}

// splitName reads "<kind>-YYYYMMDD<ext>" for one of the known
// extensions.
func splitName(name string) (kind string, day time.Time, ext string, ok bool) {
	kind, rest, found := strings.Cut(name, "-")
	if !found || (kind != KindLogs && kind != KindTraces && kind != KindStats) || len(rest) < len(dayLayout) {
		return "", time.Time{}, "", false
	}
	day, err := time.Parse(dayLayout, rest[:len(dayLayout)])
	if err != nil {
		return "", time.Time{}, "", false
	}
	ext = rest[len(dayLayout):]
	switch strings.TrimSuffix(ext, extTmp) {
	case extPlain, extData, extHeads, extIndex:
		return kind, day, ext, true
	}
	return "", time.Time{}, "", false
}

// days lists kind's days overlapping [from, to], newest first. A zero
// from or to leaves that side open. A day with an index is sealed and
// is read from its blocks; any other day is read from its plain file.
func (s *Store) days(kind string, from, to time.Time) ([]dayRef, error) {
	entries, err := fs.ReadDir(s.root.FS(), ".")
	if err != nil {
		return nil, fmt.Errorf("telemetry: list %s: %w", s.cfg.Dir, err)
	}
	found := map[time.Time]*dayRef{}
	for _, e := range entries {
		k, day, ext, ok := splitName(e.Name())
		if !ok || k != kind || !e.Type().IsRegular() || (ext != extPlain && ext != extIndex) {
			continue
		}
		if !to.IsZero() && day.After(to) {
			continue
		}
		if !from.IsZero() && !day.Add(24*time.Hour).After(from) {
			continue
		}
		d := found[day]
		if d == nil {
			d = &dayRef{base: dayBase(kind, day), day: day}
			found[day] = d
		}
		d.sealed = d.sealed || ext == extIndex
	}
	out := make([]dayRef, 0, len(found))
	for _, d := range found {
		out = append(out, *d)
	}
	slices.SortFunc(out, func(a, b dayRef) int { return b.day.Compare(a.day) })
	return out, nil
}

// scan walks kind's lines stamped in [from, to], day by day, until fn
// returns false: newest first or oldest first. With summaries, a sealed
// day of traces gives each trace's summary line in place of the whole
// trace; a plain day gives the whole line either way, and the two read
// the same through summary. A line is valid only during the call. Lines
// a little outside the window can be passed: fn checks the time it
// cares about.
func (s *Store) scan(kind string, from, to time.Time, newestFirst, summaries bool, fn func(line []byte) bool) error {
	days, err := s.days(kind, from, to)
	if err != nil {
		return err
	}
	if !newestFirst {
		slices.Reverse(days)
	}
	for _, d := range days {
		more := true
		each := func(line []byte) bool {
			more = fn(line)
			return more
		}
		if d.sealed {
			err = s.scanSealed(d, kind == KindTraces && summaries, from, to, newestFirst, each)
		} else {
			err = s.scanPlain(d, from, to, newestFirst, each)
		}
		if err != nil || !more {
			return err
		}
	}
	return nil
}

func (s *Store) scanPlain(d dayRef, from, to time.Time, newestFirst bool, fn func(line []byte) bool) error {
	f, err := s.root.Open(d.base + extPlain)
	if errors.Is(err, os.ErrNotExist) {
		return nil // sealed or pruned since the listing
	}
	if err != nil {
		return fmt.Errorf("telemetry: open %s: %w", d.base, err)
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return fmt.Errorf("telemetry: stat %s: %w", d.base, err)
	}
	start, end := int64(0), info.Size()
	// A side of the window that lies outside this file's day needs no
	// search: the whole file is on the inside of it.
	if !from.IsZero() && from.Add(-timeSlop).After(d.day) {
		if start, err = seekTime(f, info.Size(), from.Add(-timeSlop)); err != nil {
			return fmt.Errorf("telemetry: seek %s: %w", d.base, err)
		}
	}
	if !to.IsZero() && to.Add(timeSlop).Before(d.day.Add(24*time.Hour)) {
		// seekTime stops short of its target by up to a granule; the
		// bound check below trims the overshoot this adds.
		at, err := seekTime(f, info.Size(), to.Add(timeSlop))
		if err != nil {
			return fmt.Errorf("telemetry: seek %s: %w", d.base, err)
		}
		end = min(info.Size(), at+2*seekGranule)
	}
	seen := 0
	if newestFirst {
		return scanBackward(f, end, func(line []byte) bool {
			if seen++; seen%boundEvery == 0 && !from.IsZero() {
				if t, ok := lineTime(line); ok && t.Before(from.Add(-timeSlop)) {
					return false
				}
			}
			return fn(line)
		})
	}
	return scanForward(f, start, end, func(line []byte) bool {
		if seen++; seen%boundEvery == 0 && !to.IsZero() {
			if t, ok := lineTime(line); ok && t.After(to.Add(timeSlop)) {
				return false
			}
		}
		return fn(line)
	})
}

// openSealed opens a sealed day's index and the block file to read.
func (s *Store) openSealed(d dayRef, heads bool) (idx *sealedIndex, idxF, blocks *os.File, table []blockRef, err error) {
	idxF, err = s.root.Open(d.base + extIndex)
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("telemetry: open %s index: %w", d.base, err)
	}
	if idx, err = readIndex(idxF); err != nil {
		_ = idxF.Close()
		return nil, nil, nil, nil, fmt.Errorf("telemetry: %s: %w", d.base, err)
	}
	name, table := d.base+extData, idx.data
	if heads {
		name, table = d.base+extHeads, idx.heads
	}
	if blocks, err = s.root.Open(name); err != nil {
		_ = idxF.Close()
		return nil, nil, nil, nil, fmt.Errorf("telemetry: open %s blocks: %w", d.base, err)
	}
	return idx, idxF, blocks, table, nil
}

func (s *Store) scanSealed(d dayRef, heads bool, from, to time.Time, newestFirst bool, fn func(line []byte) bool) error {
	_, idxF, f, table, err := s.openSealed(d, heads)
	if err != nil {
		return err
	}
	defer idxF.Close()
	defer f.Close()
	lo, hi := int64(0), int64(1<<63-1)
	if !from.IsZero() {
		lo = from.Add(-timeSlop).UnixNano()
	}
	if !to.IsZero() {
		hi = to.Add(timeSlop).UnixNano()
	}
	var br blockReader
	visit := func(r blockRef) (bool, error) {
		if r.last < lo || r.first > hi {
			return true, nil
		}
		block, err := br.read(f, r)
		if err != nil {
			return false, fmt.Errorf("%s: %w", d.base, err)
		}
		if !newestFirst {
			for len(block) > 0 {
				i := bytes.IndexByte(block, '\n')
				if i < 0 {
					i = len(block)
				}
				if !fn(block[:i]) {
					return false, nil
				}
				block = block[min(i+1, len(block)):]
			}
			return true, nil
		}
		block = bytes.TrimSuffix(block, []byte{'\n'})
		for len(block) > 0 {
			i := bytes.LastIndexByte(block, '\n')
			if !fn(block[i+1:]) {
				return false, nil
			}
			block = block[:max(i, 0)]
		}
		return true, nil
	}
	if newestFirst {
		for _, r := range slices.Backward(table) {
			if more, err := visit(r); err != nil || !more {
				return err
			}
		}
		return nil
	}
	for _, r := range table {
		if more, err := visit(r); err != nil || !more {
			return err
		}
	}
	return nil
}

// findSealed looks one trace up in a sealed day through its ID table.
func (s *Store) findSealed(d dayRef, traceID string, needle []byte) ([]byte, error) {
	idx, idxF, f, table, err := s.openSealed(d, false)
	if err != nil {
		return nil, err
	}
	defer idxF.Close()
	defer f.Close()
	blocks, err := idx.blocksFor(idxF, idPrefix(traceID))
	if err != nil {
		return nil, fmt.Errorf("telemetry: %s index: %w", d.base, err)
	}
	var br blockReader
	for _, b := range blocks {
		block, err := br.read(f, table[b])
		if err != nil {
			return nil, fmt.Errorf("%s: %w", d.base, err)
		}
		for len(block) > 0 {
			i := bytes.IndexByte(block, '\n')
			if i < 0 {
				i = len(block)
			}
			if bytes.Contains(head(block[:i]), needle) {
				return bytes.Clone(block[:i]), nil
			}
			block = block[min(i+1, len(block)):]
		}
	}
	return nil, nil
}
