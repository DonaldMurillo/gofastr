package telemetry

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"os"
	"slices"
	"strconv"
	"time"
)

// A finished day is sealed: its lines are packed into gzip blocks and
// the plain file is removed. A sealed day is up to three files.
//
//	<kind>-YYYYMMDD.jsonl.gz  the lines, in blocks of about 1 MiB, each
//	                          block a gzip member of its own. The file
//	                          is one valid gzip stream: zcat reads it.
//	traces-YYYYMMDD.heads.gz  traces only: each trace's summary (the
//	                          line up to its spans), blocked the same
//	                          way. A list of requests reads this and
//	                          never unpacks a span.
//	<kind>-YYYYMMDD.idx       where each block is, the times it covers,
//	                          and for traces which block holds each
//	                          trace ID.
//
// The index is written last and is the commit: a day with no index is
// read from its plain file, and a day with one is read from its blocks
// whatever else is lying beside them. A crash at any point leaves a day
// readable, and the next pass finishes or redoes the job.
const (
	extPlain = ".jsonl"
	extData  = ".jsonl.gz"
	extHeads = ".heads.gz"
	extIndex = ".idx"
	extTmp   = ".tmp"

	// blockRaw is the uncompressed size at which a block is closed.
	blockRaw = 1 << 20
	// maxBlockRaw bounds what a reader will unpack for one block: a
	// block is closed at blockRaw, so it runs over by one line at most.
	maxBlockRaw = blockRaw + maxLine + 1
)

var indexMagic = [8]byte{'G', 'F', 'T', 'I', 'D', 'X', '1', '\n'}

// blockRef places one block in its file and in time.
type blockRef struct {
	off         uint64
	clen, rlen  uint32
	first, last int64 // unix nanoseconds of its first and last timed lines
}

const (
	blockRefSize = 8 + 4 + 4 + 8 + 8
	idRecSize    = 8 + 4
)

// sealedIndex is a day's index. The ID table stays on disk and is
// searched there: a day of traces is a megabyte of it.
type sealedIndex struct {
	data, heads []blockRef
	idsOff      int64
	ids         int
}

func (r blockRef) put(b []byte) {
	binary.LittleEndian.PutUint64(b[0:], r.off)
	binary.LittleEndian.PutUint32(b[8:], r.clen)
	binary.LittleEndian.PutUint32(b[12:], r.rlen)
	binary.LittleEndian.PutUint64(b[16:], uint64(r.first))
	binary.LittleEndian.PutUint64(b[24:], uint64(r.last))
}

func getBlockRef(b []byte) blockRef {
	return blockRef{
		off:   binary.LittleEndian.Uint64(b[0:]),
		clen:  binary.LittleEndian.Uint32(b[8:]),
		rlen:  binary.LittleEndian.Uint32(b[12:]),
		first: int64(binary.LittleEndian.Uint64(b[16:])),
		last:  int64(binary.LittleEndian.Uint64(b[24:])),
	}
}

type idRec struct {
	prefix uint64 // the first 16 hex digits of the trace ID
	block  uint32
}

// encodeIndex writes the index: magic, the two block tables, the ID
// table sorted by prefix, and a checksum of all of it.
func encodeIndex(data, heads []blockRef, ids []idRec) []byte {
	slices.SortFunc(ids, func(a, b idRec) int {
		if a.prefix != b.prefix {
			if a.prefix < b.prefix {
				return -1
			}
			return 1
		}
		return int(a.block) - int(b.block)
	})
	out := make([]byte, 0, 8+2*4+(len(data)+len(heads))*blockRefSize+4+len(ids)*idRecSize+4)
	out = append(out, indexMagic[:]...)
	for _, table := range [][]blockRef{data, heads} {
		out = binary.LittleEndian.AppendUint32(out, uint32(len(table)))
		for _, r := range table {
			var b [blockRefSize]byte
			r.put(b[:])
			out = append(out, b[:]...)
		}
	}
	out = binary.LittleEndian.AppendUint32(out, uint32(len(ids)))
	for _, id := range ids {
		out = binary.LittleEndian.AppendUint64(out, id.prefix)
		out = binary.LittleEndian.AppendUint32(out, id.block)
	}
	return binary.LittleEndian.AppendUint32(out, crc32.ChecksumIEEE(out))
}

var errBadIndex = errors.New("telemetry: index file is damaged")

// readIndex reads an index's block tables and finds its ID table. It
// checks the file's shape against its length, not its checksum: that
// is checked when a day is sealed and when an index is rebuilt, and a
// lookup must not read a megabyte to find twelve bytes.
func readIndex(f *os.File) (*sealedIndex, error) {
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	size := info.Size()
	rd := bufio.NewReaderSize(io.NewSectionReader(f, 0, size), 64<<10)
	var magic [8]byte
	if _, err := io.ReadFull(rd, magic[:]); err != nil || magic != indexMagic {
		return nil, errBadIndex
	}
	pos := int64(8)
	idx := &sealedIndex{}
	for _, table := range []*[]blockRef{&idx.data, &idx.heads} {
		var n [4]byte
		if _, err := io.ReadFull(rd, n[:]); err != nil {
			return nil, errBadIndex
		}
		count := int64(binary.LittleEndian.Uint32(n[:]))
		pos += 4
		if count > (size-pos)/blockRefSize {
			return nil, errBadIndex
		}
		*table = make([]blockRef, count)
		for i := range *table {
			var b [blockRefSize]byte
			if _, err := io.ReadFull(rd, b[:]); err != nil {
				return nil, errBadIndex
			}
			(*table)[i] = getBlockRef(b[:])
		}
		pos += count * blockRefSize
	}
	var n [4]byte
	if _, err := io.ReadFull(rd, n[:]); err != nil {
		return nil, errBadIndex
	}
	idx.ids = int(binary.LittleEndian.Uint32(n[:]))
	idx.idsOff = pos + 4
	if idx.idsOff+int64(idx.ids)*idRecSize+4 != size {
		return nil, errBadIndex
	}
	return idx, nil
}

// blocksFor returns the blocks of id's prefix, searching the ID table
// where it lies on disk. More than one block is a prefix shared by two
// traces, which the caller tells apart by reading them.
func (idx *sealedIndex) blocksFor(f *os.File, prefix uint64) ([]uint32, error) {
	rec := func(i int) (idRec, error) {
		var b [idRecSize]byte
		if _, err := f.ReadAt(b[:], idx.idsOff+int64(i)*idRecSize); err != nil {
			return idRec{}, err
		}
		return idRec{binary.LittleEndian.Uint64(b[0:]), binary.LittleEndian.Uint32(b[8:])}, nil
	}
	lo, hi := 0, idx.ids
	for lo < hi {
		mid := lo + (hi-lo)/2
		r, err := rec(mid)
		if err != nil {
			return nil, err
		}
		if r.prefix < prefix {
			lo = mid + 1
		} else {
			hi = mid
		}
	}
	var blocks []uint32
	for i := lo; i < idx.ids; i++ {
		r, err := rec(i)
		if err != nil {
			return nil, err
		}
		if r.prefix != prefix {
			break
		}
		if int(r.block) < len(idx.data) && !slices.Contains(blocks, r.block) {
			blocks = append(blocks, r.block)
		}
	}
	return blocks, nil
}

// blockReader unpacks blocks one after another, keeping its gzip state
// and its buffers between them: a search unpacks a thousand blocks a
// day, and setting up a reader for each cost more than reading them.
type blockReader struct {
	src *bufio.Reader
	zr  *gzip.Reader
	buf []byte
}

// read unpacks one block and returns its lines, valid until the next
// read.
func (b *blockReader) read(f io.ReaderAt, r blockRef) ([]byte, error) {
	if r.rlen > maxBlockRaw {
		return nil, errBadIndex
	}
	section := io.NewSectionReader(f, int64(r.off), int64(r.clen))
	if b.src == nil {
		b.src = bufio.NewReaderSize(section, 64<<10)
	} else {
		b.src.Reset(section)
	}
	var err error
	if b.zr == nil {
		b.zr, err = gzip.NewReader(b.src)
	} else {
		err = b.zr.Reset(b.src)
	}
	if err != nil {
		return nil, fmt.Errorf("telemetry: block: %w", err)
	}
	b.zr.Multistream(false)
	if cap(b.buf) < int(r.rlen) {
		b.buf = make([]byte, r.rlen)
	}
	b.buf = b.buf[:r.rlen]
	if _, err := io.ReadFull(b.zr, b.buf); err != nil {
		return nil, fmt.Errorf("telemetry: block: %w", err)
	}
	// One read past the end: it is the read that reaches the block's
	// checksum, and without it a damaged block that still unpacks was
	// served as what the app wrote.
	var past [1]byte
	if n, err := b.zr.Read(past[:]); n != 0 || err != io.EOF {
		if err == nil || err == io.EOF {
			err = errors.New("longer than its index says")
		}
		return nil, fmt.Errorf("telemetry: block: %w", err)
	}
	return b.buf, nil
}

// blockWriter packs lines into gzip members and keeps the table of them.
type blockWriter struct {
	f      *os.File
	zw     *gzip.Writer
	raw    bytes.Buffer
	packed bytes.Buffer
	table  []blockRef
	off    uint64
	first  int64
	last   int64
	// took is how long packing the last block took, set when add or
	// flush put one on disk and zero otherwise: what the sealer rests
	// in proportion to.
	took time.Duration
}

func newBlockWriter(f *os.File) *blockWriter {
	return &blockWriter{f: f, zw: gzip.NewWriter(io.Discard)}
}

// add appends one line and its newline, stamped t (zero for a line with
// no time, which takes the time of the line before it).
func (w *blockWriter) add(line []byte, t time.Time) error {
	w.took = 0
	if !t.IsZero() {
		ns := t.UnixNano()
		if w.raw.Len() == 0 || w.first == 0 {
			w.first = ns
		}
		w.last = max(w.last, ns)
		w.first = min(w.first, ns)
	}
	w.raw.Write(line)
	w.raw.WriteByte('\n')
	if w.raw.Len() >= blockRaw {
		return w.flush()
	}
	return nil
}

func (w *blockWriter) flush() error {
	if w.raw.Len() == 0 {
		return nil
	}
	start := time.Now()
	w.packed.Reset()
	w.zw.Reset(&w.packed)
	if _, err := w.zw.Write(w.raw.Bytes()); err != nil {
		return err
	}
	if err := w.zw.Close(); err != nil {
		return err
	}
	if _, err := w.f.Write(w.packed.Bytes()); err != nil {
		return err
	}
	// A block of lines with no time at all borrows the block before it.
	if w.first == 0 && len(w.table) > 0 {
		w.first, w.last = w.table[len(w.table)-1].last, w.table[len(w.table)-1].last
	}
	w.table = append(w.table, blockRef{off: w.off, clen: uint32(w.packed.Len()), rlen: uint32(w.raw.Len()), first: w.first, last: w.last})
	w.off += uint64(w.packed.Len())
	w.raw.Reset()
	w.first, w.last = 0, 0
	w.took = time.Since(start)
	return nil
}

// tracePrefix reads the ID a trace line carries as the 64 bits the
// index files it under.
func tracePrefix(line []byte) (uint64, bool) {
	id, ok := stringField(head(line), `"trace_id":`)
	if !ok || len(id) < 16 {
		return 0, false
	}
	v, err := strconv.ParseUint(id[:16], 16, 64)
	return v, err == nil
}

func idPrefix(traceID string) uint64 {
	v, _ := strconv.ParseUint(traceID[:16], 16, 64)
	return v
}

func dayBase(kind string, day time.Time) string { return kind + "-" + day.UTC().Format(dayLayout) }

// sealDay packs kind's plain file for day into blocks, writes its index
// and removes the plain file. It does nothing when there is no plain
// file, and finishes the job when an earlier pass was cut short. after
// runs between blocks; an error from it stops the pass and leaves the
// plain file as it was.
func sealDay(root *os.Root, kind string, day time.Time, after func(time.Duration) error) (err error) {
	base := dayBase(kind, day)
	if _, err := root.Stat(base + extIndex); err == nil {
		// Sealed already; a plain file still here is a leftover.
		if rmErr := root.Remove(base + extPlain); rmErr != nil && !errors.Is(rmErr, os.ErrNotExist) {
			return rmErr
		}
		return nil
	}
	src, err := root.Open(base + extPlain)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer src.Close()

	create := func(name string) (*os.File, error) {
		return root.OpenFile(name+extTmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	}
	tmp := []string{base + extData, base + extIndex}
	dataF, err := create(base + extData)
	if err != nil {
		return err
	}
	defer dataF.Close()
	data := newBlockWriter(dataF)
	var heads *blockWriter
	if kind == KindTraces {
		tmp = append(tmp, base+extHeads)
		headsF, err := create(base + extHeads)
		if err != nil {
			return err
		}
		defer headsF.Close()
		heads = newBlockWriter(headsF)
	}
	defer func() {
		if err != nil {
			for _, name := range tmp {
				_ = root.Remove(name + extTmp)
			}
		}
	}()

	var ids []idRec
	lr := newLineReader(src, scanBuf)
	var headLine []byte
	for {
		line, _, rerr := lr.next()
		if line != nil && len(line) > 0 {
			t, _ := lineTime(line)
			if heads != nil {
				if prefix, ok := tracePrefix(line); ok {
					ids = append(ids, idRec{prefix, uint32(len(data.table))})
				}
				headLine = append(append(headLine[:0], head(line)...), '}')
				if err := heads.add(headLine, t); err != nil {
					return err
				}
			}
			if err := data.add(line, t); err != nil {
				return err
			}
			// A block went to disk: step aside for the app, or stop.
			if took := data.took; took > 0 && after != nil {
				if err := after(took); err != nil {
					return err
				}
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			return rerr
		}
	}
	writers := []*blockWriter{data}
	var headTable []blockRef
	if heads != nil {
		writers = append(writers, heads)
	}
	for _, w := range writers {
		if err := w.flush(); err != nil {
			return err
		}
		if err := w.f.Sync(); err != nil {
			return err
		}
	}
	if heads != nil {
		headTable = heads.table
	}
	idxF, err := create(base + extIndex)
	if err != nil {
		return err
	}
	_, werr := idxF.Write(encodeIndex(data.table, headTable, ids))
	if err := errors.Join(werr, idxF.Sync(), idxF.Close()); err != nil {
		return err
	}
	// The index goes last: it is what says the day is sealed.
	for _, ext := range []string{extData, extHeads, extIndex} {
		if ext == extHeads && heads == nil {
			continue
		}
		if err := root.Rename(base+ext+extTmp, base+ext); err != nil {
			return err
		}
	}
	return root.Remove(base + extPlain)
}

// SealDay seals one day of one kind in dir: see sealDay. The writer
// does this for every finished day on its own; this is for a tool that
// works on a directory with no writer open.
func SealDay(dir, kind string, day time.Time) error {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return err
	}
	defer root.Close()
	return sealDay(root, kind, day, nil)
}

// checkIndex reports whether an index file is whole: its checksum
// matches and its tables fit its length.
func checkIndex(root *os.Root, name string) bool {
	b, err := root.ReadFile(name)
	if err != nil || len(b) < len(indexMagic)+4 || !bytes.Equal(b[:len(indexMagic)], indexMagic[:]) {
		return false
	}
	body, sum := b[:len(b)-4], binary.LittleEndian.Uint32(b[len(b)-4:])
	if crc32.ChecksumIEEE(body) != sum {
		return false
	}
	f, err := root.Open(name)
	if err != nil {
		return false
	}
	defer f.Close()
	_, err = readIndex(f)
	return err == nil
}

// countingReader counts the bytes handed out, one at a time when asked
// for one: the gzip reader then takes exactly a member's bytes, and the
// count is where the next member starts.
type countingReader struct {
	rd *bufio.Reader
	n  uint64
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.rd.Read(p)
	c.n += uint64(n)
	return n, err
}

func (c *countingReader) ReadByte() (byte, error) {
	b, err := c.rd.ReadByte()
	if err == nil {
		c.n++
	}
	return b, err
}

// rebuildIndex writes a sealed day's index again from its block file,
// and for traces its summaries too: for a day whose index was lost or
// damaged after its plain file was removed. The blocks are the record;
// everything else is derived from them.
func rebuildIndex(root *os.Root, kind string, day time.Time) (err error) {
	base := dayBase(kind, day)
	f, err := root.Open(base + extData)
	if err != nil {
		return err
	}
	defer f.Close()
	var heads *blockWriter
	tmp := []string{base + extIndex}
	if kind == KindTraces {
		hf, err := root.OpenFile(base+extHeads+extTmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
		if err != nil {
			return err
		}
		defer hf.Close()
		heads = newBlockWriter(hf)
		tmp = append(tmp, base+extHeads)
	}
	defer func() {
		if err != nil {
			for _, name := range tmp {
				_ = root.Remove(name + extTmp)
			}
		}
	}()

	cr := &countingReader{rd: bufio.NewReaderSize(f, 64<<10)}
	var table []blockRef
	var ids []idRec
	var headLine []byte
	raw := make([]byte, 0, blockRaw)
	for {
		start := cr.n
		if _, err := cr.rd.Peek(1); err == io.EOF {
			break
		}
		zr, err := gzip.NewReader(cr)
		if err != nil {
			return fmt.Errorf("telemetry: %s block %d: %w", base, len(table), err)
		}
		zr.Multistream(false)
		buf := bytes.NewBuffer(raw[:0])
		if _, err := io.Copy(buf, io.LimitReader(zr, maxBlockRaw+1)); err != nil {
			return fmt.Errorf("telemetry: %s block %d: %w", base, len(table), err)
		}
		if buf.Len() > maxBlockRaw {
			return fmt.Errorf("telemetry: %s block %d is larger than a block can be", base, len(table))
		}
		raw = buf.Bytes()
		ref := blockRef{off: start, clen: uint32(cr.n - start), rlen: uint32(len(raw))}
		for block := raw; len(block) > 0; {
			i := bytes.IndexByte(block, '\n')
			if i < 0 {
				i = len(block)
			}
			line := block[:i]
			block = block[min(i+1, len(block)):]
			t, timed := lineTime(line)
			if timed {
				ns := t.UnixNano()
				if ref.first == 0 {
					ref.first = ns
				}
				ref.first, ref.last = min(ref.first, ns), max(ref.last, ns)
			}
			if heads != nil && len(line) > 0 {
				if prefix, ok := tracePrefix(line); ok {
					ids = append(ids, idRec{prefix, uint32(len(table))})
				}
				headLine = append(append(headLine[:0], head(line)...), '}')
				if err := heads.add(headLine, t); err != nil {
					return err
				}
			}
		}
		if ref.first == 0 && len(table) > 0 {
			ref.first, ref.last = table[len(table)-1].last, table[len(table)-1].last
		}
		table = append(table, ref)
	}
	var headTable []blockRef
	if heads != nil {
		if err := errors.Join(heads.flush(), heads.f.Sync()); err != nil {
			return err
		}
		headTable = heads.table
	}
	idxF, err := root.OpenFile(base+extIndex+extTmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	_, werr := idxF.Write(encodeIndex(table, headTable, ids))
	if err := errors.Join(werr, idxF.Sync(), idxF.Close()); err != nil {
		return err
	}
	if heads != nil {
		if err := root.Rename(base+extHeads+extTmp, base+extHeads); err != nil {
			return err
		}
	}
	return root.Rename(base+extIndex+extTmp, base+extIndex)
}

// RepairDay makes a sealed day readable again when its index is missing
// or damaged and its plain file is gone, by rebuilding the index from
// the blocks. It does nothing for a day that is whole, plain, or absent.
func RepairDay(dir, kind string, day time.Time) error {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return err
	}
	defer root.Close()
	return repairDay(root, kind, day)
}

func repairDay(root *os.Root, kind string, day time.Time) error {
	base := dayBase(kind, day)
	if _, err := root.Stat(base + extPlain); err == nil {
		return nil // not sealed yet, or about to be sealed again
	}
	if _, err := root.Stat(base + extData); err != nil {
		return nil
	}
	if checkIndex(root, base+extIndex) {
		return nil
	}
	return rebuildIndex(root, kind, day)
}
