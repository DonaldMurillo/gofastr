package sqlite

import (
	"encoding/binary"
	"errors"
	"os"
	"sync"
)

// FileBackend is the interface for database storage backends.
type FileBackend interface {
	ReadAt(p []byte, off int64) (int, error)
	WriteAt(p []byte, off int64) (int, error)
	Truncate(size int64) error
	Len() int64
	Sync() error
	Close() error
}

// ============================================================================
// MemFile — in-memory file for database storage
// ============================================================================

// MemFile is an in-memory byte buffer that simulates a file.
type MemFile struct {
	data []byte
}

// NewMemFile creates a new empty MemFile.
func NewMemFile() *MemFile {
	return &MemFile{data: make([]byte, 0)}
}

// ReadAt reads len(p) bytes from offset off.
func (m *MemFile) ReadAt(p []byte, off int64) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	if off >= int64(len(m.data)) {
		return 0, errors.New("offset beyond end of file")
	}
	n := copy(p, m.data[off:])
	if n < len(p) {
		return n, errors.New("short read")
	}
	return n, nil
}

// WriteAt writes len(p) bytes at offset off, extending the file if needed.
func (m *MemFile) WriteAt(p []byte, off int64) (int, error) {
	end := off + int64(len(p))
	if end > int64(len(m.data)) {
		newData := make([]byte, end)
		copy(newData, m.data)
		m.data = newData
	}
	copy(m.data[off:], p)
	return len(p), nil
}

// Truncate resizes the file.
func (m *MemFile) Truncate(size int64) error {
	if size < 0 {
		return errors.New("negative size")
	}
	if size <= int64(len(m.data)) {
		m.data = m.data[:size]
	} else {
		newData := make([]byte, size)
		copy(newData, m.data)
		m.data = newData
	}
	return nil
}

// Len returns the length of the file.
func (m *MemFile) Len() int64 {
	return int64(len(m.data))
}

// Bytes returns a copy of the file data.
func (m *MemFile) Bytes() []byte {
	cp := make([]byte, len(m.data))
	copy(cp, m.data)
	return cp
}

// Sync is a no-op for in-memory files.
func (m *MemFile) Sync() error { return nil }

// Close is a no-op for in-memory files.
func (m *MemFile) Close() error { return nil }

// ============================================================================
// DiskFile — os.File-backed storage
// ============================================================================

// DiskFile wraps an *os.File to implement FileBackend.
type DiskFile struct {
	mu   sync.Mutex
	file *os.File
	size int64
}

// OpenDiskFile opens or creates a database file at the given path.
func OpenDiskFile(path string) (*DiskFile, error) {
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0666)
	if err != nil {
		return nil, err
	}
	fi, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, err
	}
	return &DiskFile{file: f, size: fi.Size()}, nil
}

// ReadAt reads from the underlying file.
func (d *DiskFile) ReadAt(p []byte, off int64) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.file.ReadAt(p, off)
}

// WriteAt writes to the underlying file, extending if needed.
func (d *DiskFile) WriteAt(p []byte, off int64) (int, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	n, err := d.file.WriteAt(p, off)
	if err != nil {
		return n, err
	}
	end := off + int64(len(p))
	if end > d.size {
		d.size = end
	}
	return n, nil
}

// Truncate resizes the file.
func (d *DiskFile) Truncate(size int64) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if err := d.file.Truncate(size); err != nil {
		return err
	}
	d.size = size
	return nil
}

// Len returns the file size.
func (d *DiskFile) Len() int64 {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.size
}

// Sync flushes the file to disk.
func (d *DiskFile) Sync() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.file.Sync()
}

// Close closes the underlying file.
func (d *DiskFile) Close() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.file.Close()
}

// ============================================================================
// Pager — page management
// ============================================================================

// Pager manages pages in a database file.
type Pager struct {
	file      FileBackend
	pageSize  int
	pageCount int
	header    *DatabaseHeader
	pages     [][]byte // page cache (0=unused, 1-indexed)
	dirty     []bool   // tracks which pages need flushing
	pool      *pagePool

	// Transaction COW: maps page number -> original page data (before first mutation in txn)
	txnOrigPages map[int][]byte
	inTxn        bool
}

// pagePool manages reusable page-sized buffers.
type pagePool struct {
	bufs [][]byte
	sz   int
}

func newPagePool(pageSize int) *pagePool {
	return &pagePool{sz: pageSize}
}

func (pp *pagePool) Get() []byte {
	if len(pp.bufs) > 0 {
		b := pp.bufs[len(pp.bufs)-1]
		pp.bufs = pp.bufs[:len(pp.bufs)-1]
		for i := range b {
			b[i] = 0
		}
		return b
	}
	return make([]byte, pp.sz)
}

func (pp *pagePool) Put(b []byte) {
	if cap(b) >= pp.sz {
		pp.bufs = append(pp.bufs, b[:pp.sz])
	}
}

// NewPager creates a new Pager.
func NewPager(file FileBackend, pageSize int) (*Pager, error) {
	if !validPageSize(pageSize) {
		return nil, errors.New("invalid page size")
	}

	p := &Pager{
		file:     file,
		pageSize: pageSize,
		pages:    make([][]byte, 0, 64), // 0=unused, pages are 1-indexed
		dirty:    make([]bool, 0, 64),
		pool:     newPagePool(pageSize),
	}

	// Calculate page count from file size
	fileSize := file.Len()
	if fileSize > 0 {
		if fileSize < int64(headerSize) {
			return nil, errors.New("file too small for database header")
		}
		p.pageCount = int((fileSize + int64(pageSize) - 1) / int64(pageSize))
		if fileSize == int64(pageSize) {
			p.pageCount = 1
		}
	}

	return p, nil
}

// cachedPage returns the cached page data or nil if not cached.
func (p *Pager) cachedPage(num int) []byte {
	if num < len(p.pages) {
		return p.pages[num]
	}
	return nil
}

// GetPageDataReadOnly returns the page data without copying.
// Caller must not modify the returned slice.
func (p *Pager) GetPageDataReadOnly(num int) ([]byte, error) {
	if num < 1 || num > p.pageCount {
		return nil, errors.New("page number out of range")
	}

	if data := p.cachedPage(num); data != nil {
		return data, nil
	}

	offset := int64(num-1) * int64(p.pageSize)
	data := p.pool.Get()
	_, err := p.file.ReadAt(data, offset)
	if err != nil {
		p.pool.Put(data)
		return nil, err
	}

	p.ensureCapacity(num)
	p.pages[num] = data
	return data, nil
}

// GetPageBuffer returns a zeroed page-sized buffer from the pool.
func (p *Pager) GetPageBuffer() []byte {
	return p.pool.Get()
}

// PutPageBuffer returns a buffer to the pool.
func (p *Pager) PutPageBuffer(b []byte) {
	p.pool.Put(b)
}

// GetPageSize returns the page size.
func (p *Pager) GetPageSize() int {
	return p.pageSize
}

// PageCount returns the number of pages.
func (p *Pager) PageCount() int {
	return p.pageCount
}

func (p *Pager) ensureCapacity(need int) {
	if need < len(p.pages) {
		return
	}
	newCap := cap(p.pages)
	if newCap < need+1 {
		for newCap < need+1 {
			newCap = newCap*2 + 1
		}
		newPages := make([][]byte, len(p.pages), newCap)
		copy(newPages, p.pages)
		p.pages = newPages
		newDirty := make([]bool, len(p.dirty), newCap)
		copy(newDirty, p.dirty)
		p.dirty = newDirty
	}
	p.pages = p.pages[:need+1]
	p.dirty = p.dirty[:need+1]
}

// AllocatePage allocates a new page and returns its page number (1-indexed).
func (p *Pager) AllocatePage() (int, error) {
	p.pageCount++
	if p.header != nil {
		p.header.DatabaseSizePages = uint32(p.pageCount)
	}
	pageNum := p.pageCount

	data := p.pool.Get()
	p.ensureCapacity(pageNum)
	p.pages[pageNum] = data
	p.dirty[pageNum] = true

	return pageNum, nil
}

// GetPageData returns a copy of the page data for the given page number.
func (p *Pager) GetPageData(num int) ([]byte, error) {
	if num < 1 || num > p.pageCount {
		return nil, errors.New("page number out of range")
	}

	if data := p.cachedPage(num); data != nil {
		cp := p.pool.Get()
		copy(cp, data)
		return cp, nil
	}

	offset := int64(num-1) * int64(p.pageSize)
	data := p.pool.Get()
	_, err := p.file.ReadAt(data, offset)
	if err != nil {
		p.pool.Put(data)
		return nil, err
	}

	p.ensureCapacity(num)
	p.pages[num] = data
	cp := p.pool.Get()
	copy(cp, data)
	return cp, nil
}

// SetPageData sets the page data for the given page number.
// GetPageDataMutable returns a direct reference to the page buffer for in-place modification.
// The caller MUST NOT hold the reference after calling any other pager methods.
// The page is automatically marked dirty.
func (p *Pager) GetPageDataMutable(num int) ([]byte, error) {
	if num < 1 || num > p.pageCount {
		return nil, errors.New("page number out of range")
	}

	if data := p.cachedPage(num); data != nil {
		if num < len(p.dirty) {
			p.dirty[num] = true
		}
		// COW: save original on first mutation in transaction
		if p.inTxn && p.txnOrigPages != nil {
			if _, saved := p.txnOrigPages[num]; !saved {
				orig := make([]byte, p.pageSize)
				copy(orig, data)
				p.txnOrigPages[num] = orig
			}
		}
		return data, nil
	}

	offset := int64(num-1) * int64(p.pageSize)
	data := p.pool.Get()
	_, err := p.file.ReadAt(data, offset)
	if err != nil {
		p.pool.Put(data)
		return nil, err
	}

	// COW: save original before we mutate
	if p.inTxn && p.txnOrigPages != nil {
		if _, saved := p.txnOrigPages[num]; !saved {
			orig := make([]byte, p.pageSize)
			copy(orig, data)
			p.txnOrigPages[num] = orig
		}
	}

	p.ensureCapacity(num)
	p.pages[num] = data
	p.dirty[num] = true
	return data, nil
}

func (p *Pager) SetPageData(num int, data []byte) error {
	if num < 1 || num > p.pageCount {
		return errors.New("page number out of range")
	}
	if len(data) != p.pageSize {
		return errors.New("page data size mismatch")
	}

	cp := p.pool.Get()
	copy(cp, data)
	p.ensureCapacity(num)
	p.pages[num] = cp
	p.dirty[num] = true
	return nil
}

// Flush writes all dirty pages back to the file.
func (p *Pager) Flush() error {
	if len(p.dirty) == 0 {
		return nil
	}
	for num := 1; num < len(p.dirty); num++ {
		if !p.dirty[num] {
			continue
		}
		if num >= len(p.pages) || p.pages[num] == nil {
			continue
		}
		data := p.pages[num]

		offset := int64(num-1) * int64(p.pageSize)
		end := offset + int64(len(data))
		if end > p.file.Len() {
			if err := p.file.Truncate(end); err != nil {
				return err
			}
		}

		if _, err := p.file.WriteAt(data, offset); err != nil {
			return err
		}
	}

	// Reset dirty flags
	for i := range p.dirty {
		p.dirty[i] = false
	}
	return nil
}

// Close flushes and clears the cache.
func (p *Pager) Close() error {
	if err := p.Flush(); err != nil {
		return err
	}
	p.pages = nil
	p.dirty = nil
	return nil
}

// pagerStatementSnapshot captures in-memory pager state at a statement
// boundary so a failed statement can be rolled back WITHOUT disturbing
// the enclosing transaction's COW rollback journal.
//
// This is intentionally separate from the transaction's own COW
// rollback state (txnOrigPages). The transaction journal stores the
// pre-BEGIN page images so ROLLBACK can restore the table to its
// state before BEGIN. The statement snapshot stores the state at the
// statement boundary so a failed statement inside the transaction
// does not leak its partial writes into either the transaction's
// eventual commit or its rollback.
//
// Critically, this snapshot NEVER flushes the page cache to disk.
// Inside a transaction the on-disk file is unchanged (only the
// in-memory cache is mutated); flushing here would race with the
// outer transaction's eventual rollback, which assumes the file still
// reflects the pre-BEGIN state. The old Snapshot()/Restore() pair
// flushed and then replaced the file contents, which silently
// discarded txnOrigPages' ability to recover pre-BEGIN images —
// see TestRollbackAfterFailedStatement.
type pagerStatementSnapshot struct {
	pages     [][]byte
	dirty     []bool
	pageCount int
	origPages map[int][]byte
}

// StatementSnapshot captures pager state for statement-level rollback.
// It does not flush.
func (p *Pager) StatementSnapshot() *pagerStatementSnapshot {
	pages := make([][]byte, len(p.pages))
	for i, pg := range p.pages {
		if pg != nil {
			cp := make([]byte, p.pageSize)
			copy(cp, pg)
			pages[i] = cp
		}
	}
	dirty := append([]bool(nil), p.dirty...)
	origPages := make(map[int][]byte, len(p.txnOrigPages))
	for k, v := range p.txnOrigPages {
		cp := make([]byte, len(v))
		copy(cp, v)
		origPages[k] = cp
	}
	return &pagerStatementSnapshot{
		pages:     pages,
		dirty:     dirty,
		pageCount: p.pageCount,
		origPages: origPages,
	}
}

// StatementRestore restores pager state captured by StatementSnapshot,
// including the transaction's pre-BEGIN page images, so statement
// rollback composes with transaction rollback.
func (p *Pager) StatementRestore(s *pagerStatementSnapshot) {
	if s == nil {
		return
	}
	p.pages = s.pages
	p.dirty = s.dirty
	p.pageCount = s.pageCount
	p.txnOrigPages = s.origPages
}

// BeginTxn starts a page-level COW transaction.
func (p *Pager) BeginTxn() {
	p.inTxn = true
	p.txnOrigPages = make(map[int][]byte)
}

// CommitTxn finishes a COW transaction, discarding saved originals.
func (p *Pager) CommitTxn() {
	p.inTxn = false
	p.txnOrigPages = nil
}

// RollbackTxn restores only the pages that were modified during the transaction.
func (p *Pager) RollbackTxn() error {
	for pageNum, orig := range p.txnOrigPages {
		if pageNum < len(p.pages) && p.pages[pageNum] != nil {
			copy(p.pages[pageNum], orig)
			p.dirty[pageNum] = true
		}
	}
	if err := p.Flush(); err != nil {
		return err
	}
	p.inTxn = false
	p.txnOrigPages = nil
	return nil
}

// InitNew initializes a new database with a valid header on page 1.
func (p *Pager) InitNew() error {
	if p.pageCount < 1 {
		p.pageCount = 1
	}

	// Build a valid magic header
	var magic [16]byte
	copy(magic[:], magicHeader)

	hdr := &DatabaseHeader{
		Magic:                  magic,
		PageSize:               p.pageSize,
		FileFormatWriteVersion: 1,
		FileFormatReadVersion:  1,
		FileChangeCounter:      1,
		DatabaseSizePages:      uint32(p.pageCount),
		SchemaFormatNumber:     4,
		TextEncoding:           1, // UTF-8
		SQLiteVersionNumber:    3039004,
		VersionValidFor:        1,
	}

	p.header = hdr

	// Create page 1 with the header
	page1 := p.pool.Get()
	headerBuf := WriteHeader(hdr)
	copy(page1, headerBuf)

	p.ensureCapacity(1)
	p.pages[1] = page1
	p.dirty[1] = true

	return p.Flush()
}

// GetHeader returns the database header.
// GetHeader returns the database header, reading from page 1 if needed.
func (p *Pager) GetHeader() *DatabaseHeader {
	if p.header != nil {
		return p.header
	}

	data, err := p.GetPageData(1)
	if err != nil {
		return nil
	}

	if len(data) < headerSize {
		return nil
	}

	h, err := ReadHeader(data[:headerSize])
	if err != nil {
		return nil
	}

	p.header = h
	return h
}

// LoadHeader reads the header from disk and returns an error on failure.
func (p *Pager) LoadHeader() error {
	data, err := p.GetPageData(1)
	if err != nil {
		return err
	}
	if len(data) < headerSize {
		return errors.New("page too small for header")
	}
	h, err := ReadHeader(data[:headerSize])
	if err != nil {
		return err
	}
	p.header = h
	// Restore page count from header, but use file size as fallback if header is stale
	filePages := int((p.file.Len() + int64(p.pageSize) - 1) / int64(p.pageSize))
	headerPages := int(h.DatabaseSizePages)
	if headerPages > 0 && headerPages >= filePages {
		p.pageCount = headerPages
	} else {
		p.pageCount = filePages
	}
	return nil
}

// UpdateHeader writes the current header to page 1.
func (p *Pager) UpdateHeader(h *DatabaseHeader) error {
	p.header = h

	data, err := p.GetPageData(1)
	if err != nil {
		return err
	}

	headerBuf := WriteHeader(h)
	copy(data, headerBuf)

	return p.SetPageData(1, data)
}

// PageHeaderOffset returns the offset of the page header within the page data.
func (p *Pager) PageHeaderOffset(pageNum int) int {
	if pageNum == 1 {
		return headerSize
	}
	return 0
}

// ReadPageHeaderFrom reads a page header from raw bytes.
func ReadPageHeaderFrom(data []byte) PageHeader {
	if len(data) < 8 {
		return PageHeader{}
	}
	var h PageHeader
	h.PageType = data[0]
	h.FirstFreeblock = binary.BigEndian.Uint16(data[1:3])
	h.CellCount = binary.BigEndian.Uint16(data[3:5])
	h.ContentOffset = binary.BigEndian.Uint16(data[5:7])
	h.FragmentedBytes = data[7]
	if len(data) >= 12 && (h.PageType == pageTypeInteriorTable || h.PageType == pageTypeInteriorIndex) {
		h.RightMostPtr = binary.BigEndian.Uint32(data[8:12])
	}
	return h
}

// WritePageHeaderTo writes a page header to raw bytes.
func WritePageHeaderTo(data []byte, h PageHeader) {
	data[0] = h.PageType
	binary.BigEndian.PutUint16(data[1:3], h.FirstFreeblock)
	binary.BigEndian.PutUint16(data[3:5], h.CellCount)
	binary.BigEndian.PutUint16(data[5:7], h.ContentOffset)
	data[7] = h.FragmentedBytes
	if h.PageType == pageTypeInteriorTable || h.PageType == pageTypeInteriorIndex {
		binary.BigEndian.PutUint32(data[8:12], h.RightMostPtr)
	}
}

func (p *Pager) GetSchemaPage() int {
	if p.header == nil {
		return 0
	}
	buf := p.header.ReservedExpansion[:4]
	return int(buf[0])<<24 | int(buf[1])<<16 | int(buf[2])<<8 | int(buf[3])
}

func (p *Pager) SetSchemaPage(page int) {
	if p.header == nil {
		return
	}
	p.header.ReservedExpansion[0] = byte(page >> 24)
	p.header.ReservedExpansion[1] = byte(page >> 16)
	p.header.ReservedExpansion[2] = byte(page >> 8)
	p.header.ReservedExpansion[3] = byte(page)
	// Write updated header back to page 1
	hdrBuf := WriteHeader(p.header)
	if page1 := p.cachedPage(1); page1 != nil {
		copy(page1, hdrBuf)
		if 1 < len(p.dirty) {
			p.dirty[1] = true
		}
	} else if p.file != nil {
		p.file.WriteAt(hdrBuf, 0)
	}
}
