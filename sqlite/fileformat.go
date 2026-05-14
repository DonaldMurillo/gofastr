package sqlite

import (
	"encoding/binary"
	"errors"
	"math"
	"sync"
)

// ---------------------------------------------------------------------------
// Errors
// ---------------------------------------------------------------------------

var (
	ErrBadMagic          = errors.New("sqlite: bad magic header")
	ErrHeaderTooSmall    = errors.New("sqlite: header data too small")
	ErrBadPageSize       = errors.New("sqlite: invalid page size")
	ErrBadPayloadFrac    = errors.New("sqlite: invalid payload fraction")
	ErrBadPageType       = errors.New("sqlite: invalid page type")
	ErrPageHeaderTooSmall = errors.New("sqlite: page header data too small")
	ErrCellTooSmall      = errors.New("sqlite: cell data too small")
	ErrRecordTooSmall    = errors.New("sqlite: record data too small")
	ErrBadSerialType     = errors.New("sqlite: invalid serial type")
	ErrTruncatedRecord   = errors.New("sqlite: truncated record body")
	ErrNoColumns         = errors.New("sqlite: record has no columns")
)

// ---------------------------------------------------------------------------
// Database Header
// ---------------------------------------------------------------------------

// DatabaseHeader represents the 100-byte SQLite database header found at the
// start of every database file (first page).
type DatabaseHeader struct {
	Magic                   [16]byte
	PageSize                int // actual page size (65536 is valid, 0 in header means 65536)
	FileFormatWriteVersion  uint8
	FileFormatReadVersion   uint8
	ReservedSpace           uint8
	MaxEmbeddedPayloadFrac  uint8
	MinEmbeddedPayloadFrac  uint8
	LeafPayloadFrac         uint8
	FileChangeCounter        uint32
	DatabaseSizePages       uint32
	FirstFreelistTrunkPage  uint32
	TotalFreelistPages      uint32
	SchemaCookie            uint32
	SchemaFormatNumber      uint32
	DefaultPageCacheSize    uint32
	LargestRootBTreePage    uint32
	TextEncoding            uint32
	UserVersion             uint32
	IncrementalVacuum       uint32
	ApplicationID           uint32
	ReservedExpansion       [20]byte
	VersionValidFor         uint32
	SQLiteVersionNumber     uint32
}

// ReadHeader parses the 100-byte database header from data.
func ReadHeader(data []byte) (*DatabaseHeader, error) {
	if len(data) < headerSize {
		return nil, ErrHeaderTooSmall
	}

	var h DatabaseHeader

	// Magic
	copy(h.Magic[:], data[0:16])
	if string(h.Magic[:]) != magicHeader {
		return nil, ErrBadMagic
	}

	h.PageSize = int(binary.BigEndian.Uint16(data[16:18]))
	if h.PageSize == 0 {
		h.PageSize = 65536
	}
	if !validPageSize(h.PageSize) {
		return nil, ErrBadPageSize
	}

	h.FileFormatWriteVersion = data[18]
	h.FileFormatReadVersion = data[19]
	h.ReservedSpace = data[20]
	h.MaxEmbeddedPayloadFrac = data[21]
	h.MinEmbeddedPayloadFrac = data[22]
	h.LeafPayloadFrac = data[23]
	h.FileChangeCounter = binary.BigEndian.Uint32(data[24:28])
	h.DatabaseSizePages = binary.BigEndian.Uint32(data[28:32])
	h.FirstFreelistTrunkPage = binary.BigEndian.Uint32(data[32:36])
	h.TotalFreelistPages = binary.BigEndian.Uint32(data[36:40])
	h.SchemaCookie = binary.BigEndian.Uint32(data[40:44])
	h.SchemaFormatNumber = binary.BigEndian.Uint32(data[44:48])
	h.DefaultPageCacheSize = binary.BigEndian.Uint32(data[48:52])
	h.LargestRootBTreePage = binary.BigEndian.Uint32(data[52:56])
	h.TextEncoding = binary.BigEndian.Uint32(data[56:60])
	h.UserVersion = binary.BigEndian.Uint32(data[60:64])
	h.IncrementalVacuum = binary.BigEndian.Uint32(data[64:68])
	h.ApplicationID = binary.BigEndian.Uint32(data[68:72])
	copy(h.ReservedExpansion[:], data[72:92])
	h.VersionValidFor = binary.BigEndian.Uint32(data[92:96])
	h.SQLiteVersionNumber = binary.BigEndian.Uint32(data[96:100])

	return &h, nil
}

// WriteHeader serializes a DatabaseHeader to exactly 100 bytes.
func WriteHeader(h *DatabaseHeader) []byte {
	buf := make([]byte, headerSize)

	copy(buf[0:16], h.Magic[:])

	ps := h.PageSize
	if ps >= 65536 {
		ps = 0 // 0 in the header means 65536
	}
	binary.BigEndian.PutUint16(buf[16:18], uint16(ps))

	buf[18] = h.FileFormatWriteVersion
	buf[19] = h.FileFormatReadVersion
	buf[20] = h.ReservedSpace
	buf[21] = h.MaxEmbeddedPayloadFrac
	buf[22] = h.MinEmbeddedPayloadFrac
	buf[23] = h.LeafPayloadFrac
	binary.BigEndian.PutUint32(buf[24:28], h.FileChangeCounter)
	binary.BigEndian.PutUint32(buf[28:32], h.DatabaseSizePages)
	binary.BigEndian.PutUint32(buf[32:36], h.FirstFreelistTrunkPage)
	binary.BigEndian.PutUint32(buf[36:40], h.TotalFreelistPages)
	binary.BigEndian.PutUint32(buf[40:44], h.SchemaCookie)
	binary.BigEndian.PutUint32(buf[44:48], h.SchemaFormatNumber)
	binary.BigEndian.PutUint32(buf[48:52], h.DefaultPageCacheSize)
	binary.BigEndian.PutUint32(buf[52:56], h.LargestRootBTreePage)
	binary.BigEndian.PutUint32(buf[56:60], h.TextEncoding)
	binary.BigEndian.PutUint32(buf[60:64], h.UserVersion)
	binary.BigEndian.PutUint32(buf[64:68], h.IncrementalVacuum)
	binary.BigEndian.PutUint32(buf[68:72], h.ApplicationID)
	copy(buf[72:92], h.ReservedExpansion[:])
	binary.BigEndian.PutUint32(buf[92:96], h.VersionValidFor)
	binary.BigEndian.PutUint32(buf[96:100], h.SQLiteVersionNumber)

	return buf
}

// validPageSize checks if the page size is a power of 2 between 512 and 65536.
func validPageSize(ps int) bool {
	if ps < minPageSize || ps > maxPageSize {
		return false
	}
	return (ps & (ps - 1)) == 0
}

// ---------------------------------------------------------------------------
// B-tree Page Header
// ---------------------------------------------------------------------------

// PageHeader represents the header of a B-tree page.
// For leaf table pages (0x0d) and leaf index pages (0x0a) the header is 8 bytes.
// For interior table pages (0x05) and interior index pages (0x02) the header is 12 bytes.
type PageHeader struct {
	PageType        byte
	FirstFreeblock  uint16
	CellCount       uint16
	ContentOffset   uint16
	FragmentedBytes uint8
	RightMostPtr    uint32 // Only valid for interior pages (0x02, 0x05)
}

// HeaderSize returns the size of the serialized page header in bytes.
func (h *PageHeader) HeaderSize() int {
	if h.PageType == pageTypeInteriorTable || h.PageType == pageTypeInteriorIndex {
		return 12
	}
	return 8
}

// ReadPageHeader parses a B-tree page header from data.
// isFirst indicates this is page 1 (which has the 100-byte DB header before the B-tree header).
func ReadPageHeader(data []byte, isFirst bool) (*PageHeader, error) {
	offset := 0
	if isFirst {
		offset = headerSize
	}

	needInterior := 12
	if len(data) < offset+8 {
		return nil, ErrPageHeaderTooSmall
	}

	pageType := data[offset]
	if pageType != pageTypeInteriorTable &&
		pageType != pageTypeInteriorIndex &&
		pageType != pageTypeLeafTable &&
		pageType != pageTypeLeafIndex {
		return nil, ErrBadPageType
	}

	// Need more bytes for interior pages
	if (pageType == pageTypeInteriorTable || pageType == pageTypeInteriorIndex) &&
		len(data) < offset+needInterior {
		return nil, ErrPageHeaderTooSmall
	}

	h := &PageHeader{
		PageType:        pageType,
		FirstFreeblock:  binary.BigEndian.Uint16(data[offset+1 : offset+3]),
		CellCount:       binary.BigEndian.Uint16(data[offset+3 : offset+5]),
		ContentOffset:   binary.BigEndian.Uint16(data[offset+5 : offset+7]),
		FragmentedBytes: data[offset+7],
	}

	if pageType == pageTypeInteriorTable || pageType == pageTypeInteriorIndex {
		h.RightMostPtr = binary.BigEndian.Uint32(data[offset+8 : offset+12])
	}

	return h, nil
}

// WritePageHeader serializes a PageHeader to its byte representation.
func WritePageHeader(h *PageHeader) []byte {
	size := h.HeaderSize()
	buf := make([]byte, size)

	buf[0] = h.PageType
	binary.BigEndian.PutUint16(buf[1:3], h.FirstFreeblock)
	binary.BigEndian.PutUint16(buf[3:5], h.CellCount)
	binary.BigEndian.PutUint16(buf[5:7], h.ContentOffset)
	buf[7] = h.FragmentedBytes

	if h.PageType == pageTypeInteriorTable || h.PageType == pageTypeInteriorIndex {
		binary.BigEndian.PutUint32(buf[8:12], h.RightMostPtr)
	}

	return buf
}

// ---------------------------------------------------------------------------
// Cell
// ---------------------------------------------------------------------------

// Cell represents a B-tree cell. It can be either a leaf table cell or an
// interior table cell (or index cells, which use Payload + optional overflow).
type Cell struct {
	// Interior table cells
	LeftChildPage uint32

	// Leaf table cells (and index cells)
	PayloadSize uint64 // varint: total payload length
	RowID       uint64 // varint: rowid (leaf table only)
	Key         uint64 // varint: key for interior table (= rowid)

	// Payload data (local portion only; overflow handled externally)
	Payload []byte

	// Overflow page number (0 = no overflow)
	OverflowPage uint32

	// Whether this is a leaf cell (has payload + rowid) or interior cell (has left child + key)
	IsLeaf bool
}

// ReadCell reads a cell from data given the page type.
// Returns the cell and the number of bytes consumed.
func ReadCell(data []byte, pageType byte) (*Cell, int, error) {
	if len(data) < 1 {
		return nil, 0, ErrCellTooSmall
	}

	switch pageType {
	case pageTypeLeafTable:
		return readLeafTableCell(data)
	case pageTypeInteriorTable:
		return readInteriorTableCell(data)
	case pageTypeLeafIndex:
		return readLeafIndexCell(data)
	case pageTypeInteriorIndex:
		return readInteriorIndexCell(data)
	default:
		return nil, 0, ErrBadPageType
	}
}

// readLeafTableCell reads a leaf table B-tree cell:
//   payload_size (varint) | rowid (varint) | payload | [overflow_page]
func readLeafTableCell(data []byte) (*Cell, int, error) {
	off := 0

	// Payload size
	payloadSize, n, err := DecodeVarintRaw(data[off:])
	if err != nil {
		return nil, 0, err
	}
	off += n

	// RowID
	rowID, n, err := DecodeVarintRaw(data[off:])
	if err != nil {
		return nil, 0, err
	}
	off += n

	// Compute how much payload is local (we need usableSize from context,
	// but for reading we just take what's available). The caller is
	// responsible for splitting local vs overflow. We read the payload
	// that is present.
	remaining := uint64(len(data) - off)
	payloadLen := payloadSize
	if payloadLen > remaining {
		payloadLen = remaining
		// If payload overflows, last 4 bytes of local payload are overflow page
		// We'll handle that below
	}

	payload := make([]byte, payloadLen)
	copy(payload, data[off:off+int(payloadLen)])
	off += int(payloadLen)

	// If the full payload wasn't stored locally, the last 4 bytes of the
	// local portion represent the overflow page number. However, when we
	// read the full cell buffer, the overflow page number follows the
	// local payload. The caller will handle overflow splitting based on
	// usableSize. For simplicity, we just return whatever is in the buffer.

	cell := &Cell{
		IsLeaf:      true,
		PayloadSize: payloadSize,
		RowID:       rowID,
		Payload:     payload,
	}

	// If the payload we read is less than the full payload, the remaining
	// data is on overflow pages. The overflow page number follows the
	// local payload bytes in the raw cell format.
	if payloadSize > uint64(len(payload)) && len(data)-off >= 4 {
		cell.OverflowPage = binary.BigEndian.Uint32(data[off : off+4])
		off += 4
	}
	// Also check if there are exactly 4 trailing bytes after local payload
	// that could be an overflow pointer (covers the case where the reader
	// has the full cell buffer including the overflow pointer written by
	// WriteLeafTableCell even when all payload fits locally).
	if cell.OverflowPage == 0 && len(data)-off >= 4 {
		candidate := binary.BigEndian.Uint32(data[off : off+4])
		if candidate != 0 {
			cell.OverflowPage = candidate
			off += 4
		}
	}

	return cell, off, nil
}

// readInteriorTableCell reads an interior table B-tree cell:
//   left_child_page (4 bytes) | key (varint)
func readInteriorTableCell(data []byte) (*Cell, int, error) {
	if len(data) < 4 {
		return nil, 0, ErrCellTooSmall
	}

	leftChild := binary.BigEndian.Uint32(data[0:4])
	key, n, err := DecodeVarintRaw(data[4:])
	if err != nil {
		return nil, 0, err
	}

	return &Cell{
		IsLeaf:        false,
		LeftChildPage: leftChild,
		Key:           key,
	}, 4 + n, nil
}

// readLeafIndexCell reads a leaf index B-tree cell:
//   payload_size (varint) | payload | [overflow_page]
func readLeafIndexCell(data []byte) (*Cell, int, error) {
	off := 0

	payloadSize, n, err := DecodeVarintRaw(data[off:])
	if err != nil {
		return nil, 0, err
	}
	off += n

	remaining := uint64(len(data) - off)
	payloadLen := payloadSize
	if payloadLen > remaining {
		payloadLen = remaining
	}

	payload := make([]byte, payloadLen)
	copy(payload, data[off:off+int(payloadLen)])
	off += int(payloadLen)

	cell := &Cell{
		IsLeaf:      true,
		PayloadSize: payloadSize,
		Payload:     payload,
	}

	if uint64(len(payload)) < payloadSize && len(data)-off >= 4 {
		cell.OverflowPage = binary.BigEndian.Uint32(data[off : off+4])
		off += 4
	}

	return cell, off, nil
}

// readInteriorIndexCell reads an interior index B-tree cell:
//   left_child_page (4 bytes) | payload_size (varint) | payload | [overflow_page]
func readInteriorIndexCell(data []byte) (*Cell, int, error) {
	if len(data) < 4 {
		return nil, 0, ErrCellTooSmall
	}

	off := 0
	leftChild := binary.BigEndian.Uint32(data[0:4])
	off += 4

	payloadSize, n, err := DecodeVarintRaw(data[off:])
	if err != nil {
		return nil, 0, err
	}
	off += n

	remaining := uint64(len(data) - off)
	payloadLen := payloadSize
	if payloadLen > remaining {
		payloadLen = remaining
	}

	payload := make([]byte, payloadLen)
	copy(payload, data[off:off+int(payloadLen)])
	off += int(payloadLen)

	cell := &Cell{
		IsLeaf:        false,
		LeftChildPage: leftChild,
		PayloadSize:   payloadSize,
		Payload:       payload,
	}

	if uint64(len(payload)) < payloadSize && len(data)-off >= 4 {
		cell.OverflowPage = binary.BigEndian.Uint32(data[off : off+4])
		off += 4
	}

	return cell, off, nil
}

// WriteCell serializes a Cell to bytes.
func WriteCell(c *Cell) []byte {
	if !c.IsLeaf && c.LeftChildPage > 0 && c.PayloadSize == 0 && len(c.Payload) == 0 {
		// Interior table cell: left_child_page | key
		return writeInteriorTableCell(c)
	}
	if c.IsLeaf && c.LeftChildPage == 0 {
		// Could be leaf table or leaf index
		if c.RowID > 0 || len(c.Payload) > 0 {
			return writeLeafTableCell(c)
		}
	}
	// Interior index: left_child | payload_size | payload
	if !c.IsLeaf && c.LeftChildPage > 0 && c.PayloadSize > 0 {
		return writeInteriorIndexCell(c)
	}
	// Leaf table fallback
	return writeLeafTableCell(c)
}

func writeLeafTableCell(c *Cell) []byte {
	buf := EncodeVarintRaw(c.PayloadSize)
	buf = append(buf, EncodeVarintRaw(c.RowID)...)
	buf = append(buf, c.Payload...)
	if c.OverflowPage != 0 {
		var ov [4]byte
		binary.BigEndian.PutUint32(ov[:], c.OverflowPage)
		buf = append(buf, ov[:]...)
	}
	return buf
}

func writeInteriorTableCell(c *Cell) []byte {
	buf := make([]byte, 4)
	binary.BigEndian.PutUint32(buf, c.LeftChildPage)
	buf = append(buf, EncodeVarintRaw(c.Key)...)
	return buf
}

func writeInteriorIndexCell(c *Cell) []byte {
	buf := make([]byte, 4)
	binary.BigEndian.PutUint32(buf, c.LeftChildPage)
	buf = append(buf, EncodeVarintRaw(c.PayloadSize)...)
	buf = append(buf, c.Payload...)
	if c.OverflowPage != 0 {
		var ov [4]byte
		binary.BigEndian.PutUint32(ov[:], c.OverflowPage)
		buf = append(buf, ov[:]...)
	}
	return buf
}

// WriteLeafTableCell serializes a leaf table cell explicitly.
func WriteLeafTableCell(c *Cell) []byte {
	return writeLeafTableCell(c)
}

// WriteInteriorTableCell serializes an interior table cell explicitly.
func WriteInteriorTableCell(c *Cell) []byte {
	return writeInteriorTableCell(c)
}

// ---------------------------------------------------------------------------
// Record
// ---------------------------------------------------------------------------

// Record represents a SQLite record (the payload of a leaf table cell or
// index cell). It consists of a header (serial types for each column) followed
// by the body (the actual column values).
type Record struct {
	Columns []Value
}

// recordPool caches Record objects to reduce GC pressure during scans.
var recordPool = sync.Pool{
	New: func() interface{} { return &Record{} },
}

// GetRecord fetches a Record from the pool.
func GetRecord() *Record {
	return recordPool.Get().(*Record)
}

// PutRecord returns a Record to the pool.
func PutRecord(r *Record) {
	// Clear to help GC
	for i := range r.Columns {
		r.Columns[i] = Value{}
	}
	r.Columns = r.Columns[:0]
	recordPool.Put(r)
}

// ReadRecord parses a record from payload bytes.
func ReadRecord(data []byte) (*Record, error) {
	if len(data) == 0 {
		return nil, ErrRecordTooSmall
	}

	off := 0

	// Header size (varint) includes its own length
	headerSizeVal, n, err := DecodeVarintRaw(data[off:])
	if err != nil {
		return nil, err
	}
	off += n

	if headerSizeVal < uint64(n) {
		return nil, ErrRecordTooSmall
	}
	if headerSizeVal > uint64(len(data)) {
		return nil, ErrTruncatedRecord
	}

	// Count serial types first to pre-allocate
	headerEnd := int(headerSizeVal)
	numCols := 0
	for scanOff := off; scanOff < headerEnd; {
		_, sn, err := DecodeVarintRaw(data[scanOff:])
		if err != nil {
			break
		}
		scanOff += sn
		numCols++
	}

	if numCols == 0 {
		return &Record{Columns: nil}, nil
	}

	// Use stack array for small column counts
	var serialTypesBuf [16]uint64
	serialTypes := serialTypesBuf[:0]
	if numCols > 16 {
		serialTypes = make([]uint64, 0, numCols)
	}

	for off < headerEnd {
		st, sn, err := DecodeVarintRaw(data[off:])
		if err != nil {
			return nil, err
		}
		off += sn
		serialTypes = append(serialTypes, st)
	}

	// Read values from the body
	bodyOff := headerEnd
	columns := make([]Value, numCols)

	for i, st := range serialTypes {
		val, size, err := decodeSerialValue(data, bodyOff, st)
		if err != nil {
			return nil, err
		}
		columns[i] = val
		bodyOff += size
	}

	return &Record{Columns: columns}, nil
}

// ReadRecordInto parses a record from payload bytes, reusing an existing slice.
// If buf has sufficient capacity, it is reused (resliced). Otherwise a new slice is allocated.
func ReadRecordInto(data []byte, buf []Value) (*Record, error) {
	if len(data) == 0 {
		return nil, ErrRecordTooSmall
	}

	off := 0

	headerSizeVal, n, err := DecodeVarintRaw(data[off:])
	if err != nil {
		return nil, err
	}
	off += n

	if headerSizeVal < uint64(n) {
		return nil, ErrRecordTooSmall
	}
	if headerSizeVal > uint64(len(data)) {
		return nil, ErrTruncatedRecord
	}

	headerEnd := int(headerSizeVal)

	// Count serial types
	numCols := 0
	for scanOff := off; scanOff < headerEnd; {
		_, sn, err := DecodeVarintRaw(data[scanOff:])
		if err != nil {
			break
		}
		scanOff += sn
		numCols++
	}

	if numCols == 0 {
		return &Record{Columns: nil}, nil
	}

	// Use stack array for serial types
	var serialTypesBuf [16]uint64
	serialTypes := serialTypesBuf[:0]
	if numCols > 16 {
		serialTypes = make([]uint64, 0, numCols)
	}

	for off < headerEnd {
		st, sn, err := DecodeVarintRaw(data[off:])
		if err != nil {
			return nil, err
		}
		off += sn
		serialTypes = append(serialTypes, st)
	}

	// Reuse buf if possible
	var columns []Value
	if cap(buf) >= numCols {
		columns = buf[:numCols]
	} else {
		columns = make([]Value, numCols)
	}

	bodyOff := headerEnd
	for i, st := range serialTypes {
		val, size, err := decodeSerialValue(data, bodyOff, st)
		if err != nil {
			return nil, err
		}
		columns[i] = val
		bodyOff += size
	}

	return &Record{Columns: columns}, nil
}

// ReadRecordIntoRecord decodes a record into an existing Record struct,
// avoiding the per-call *Record allocation.
func ReadRecordIntoRecord(data []byte, rec *Record, buf []Value) error {
	if len(data) == 0 {
		return ErrRecordTooSmall
	}

	off := 0

	headerSizeVal, n, err := DecodeVarintRaw(data[off:])
	if err != nil {
		return err
	}
	off += n

	if headerSizeVal < uint64(n) {
		return ErrRecordTooSmall
	}
	if headerSizeVal > uint64(len(data)) {
		return ErrTruncatedRecord
	}

	headerEnd := int(headerSizeVal)

	// Count serial types
	numCols := 0
	for scanOff := off; scanOff < headerEnd; {
		_, sn, err := DecodeVarintRaw(data[scanOff:])
		if err != nil {
			break
		}
		scanOff += sn
		numCols++
	}

	if numCols == 0 {
		rec.Columns = nil
		return nil
	}

	var serialTypesBuf [16]uint64
	serialTypes := serialTypesBuf[:0]
	if numCols > 16 {
		serialTypes = make([]uint64, 0, numCols)
	}

	for off < headerEnd {
		st, sn, err := DecodeVarintRaw(data[off:])
		if err != nil {
			return err
		}
		off += sn
		serialTypes = append(serialTypes, st)
	}

	var columns []Value
	if cap(buf) >= numCols {
		columns = buf[:numCols]
	} else {
		columns = make([]Value, numCols)
	}

	bodyOff := headerEnd
	for i, st := range serialTypes {
		val, size, err := decodeSerialValue(data, bodyOff, st)
		if err != nil {
			return err
		}
		columns[i] = val
		bodyOff += size
	}

	rec.Columns = columns
	return nil
}

// WriteRecord serializes a Record to bytes.
func WriteRecord(r *Record) []byte {
	ncols := len(r.Columns)
	if ncols == 0 {
		return []byte{1, 0} // header_size=1, serial_type=NULL
	}

	// Phase 1: compute serial types to determine header size
	serialTypes := make([]uint64, ncols)
	totalBody := 0
	for i, col := range r.Columns {
		st := computeSerialType(col)
		serialTypes[i] = st
		totalBody += serialValueSize(col)
	}

	// Compute header body size (serial type varints)
	headerBodyLen := 0
	for _, st := range serialTypes {
		headerBodyLen += VarintSize(st)
	}

	// Header size varint includes its own length
	headerSizeVal := uint64(headerBodyLen)
	headerSizeLen := VarintSize(headerSizeVal + uint64(VarintSize(headerSizeVal)))
	// Iteratively converge (header size varint may change its own length)
	for {
		total := uint64(headerSizeLen) + uint64(headerBodyLen)
		newLen := VarintSize(total)
		if newLen == headerSizeLen {
			break
		}
		headerSizeLen = newLen
	}

	totalHeader := headerSizeLen + headerBodyLen

	// Phase 2: single allocation, write everything directly
	result := make([]byte, totalHeader+totalBody)
	off := 0

	// Header size varint
	off += EncodeVarintInto(uint64(totalHeader), result, off)

	// Serial type varints
	for _, st := range serialTypes {
		off += EncodeVarintInto(st, result, off)
	}

	// Body values
	for _, col := range r.Columns {
		off += encodeValueInto(col, result, off)
	}

	return result[:off]
}

// decodeSerialValue decodes a single value from the record body given a serial type.
// Returns the Value and the number of bytes consumed.
func decodeSerialValue(data []byte, off int, serialType uint64) (Value, int, error) {
	switch serialType {
	case 0: // NULL
		return NullValue, 0, nil

	case 1: // 8-bit signed integer
		if off+1 > len(data) {
			return Value{}, 0, ErrTruncatedRecord
		}
		return IntegerValue(int64(int8(data[off]))), 1, nil

	case 2: // 16-bit signed big-endian integer
		if off+2 > len(data) {
			return Value{}, 0, ErrTruncatedRecord
		}
		v := int16(binary.BigEndian.Uint16(data[off : off+2]))
		return IntegerValue(int64(v)), 2, nil

	case 3: // 24-bit signed big-endian integer
		if off+3 > len(data) {
			return Value{}, 0, ErrTruncatedRecord
		}
		v := int32(data[off])<<16 | int32(data[off+1])<<8 | int32(data[off+2])
		if data[off]&0x80 != 0 {
			v |= -1 << 24
		}
		return IntegerValue(int64(v)), 3, nil

	case 4: // 32-bit signed big-endian integer
		if off+4 > len(data) {
			return Value{}, 0, ErrTruncatedRecord
		}
		v := int32(binary.BigEndian.Uint32(data[off : off+4]))
		return IntegerValue(int64(v)), 4, nil

	case 5: // 48-bit signed big-endian integer
		if off+6 > len(data) {
			return Value{}, 0, ErrTruncatedRecord
		}
		v := int64(data[off])<<40 | int64(data[off+1])<<32 |
			int64(data[off+2])<<24 | int64(data[off+3])<<16 |
			int64(data[off+4])<<8 | int64(data[off+5])
		if data[off]&0x80 != 0 {
			v |= -1 << 48
		}
		return IntegerValue(v), 6, nil

	case 6: // 64-bit signed big-endian integer
		if off+8 > len(data) {
			return Value{}, 0, ErrTruncatedRecord
		}
		v := int64(binary.BigEndian.Uint64(data[off : off+8]))
		return IntegerValue(v), 8, nil

	case 7: // IEEE 754 float
		if off+8 > len(data) {
			return Value{}, 0, ErrTruncatedRecord
		}
		bits := binary.BigEndian.Uint64(data[off : off+8])
		return FloatValue(math.Float64frombits(bits)), 8, nil

	case 8: // integer 0
		return IntegerValue(0), 0, nil

	case 9: // integer 1
		return IntegerValue(1), 0, nil

	case 10, 11: // reserved
		return Value{}, 0, ErrBadSerialType

	default:
		if serialType >= 12 {
			if serialType%2 == 0 {
				// BLOB of (N-12)/2 bytes
				blobLen := (serialType - 12) / 2
				if off+int(blobLen) > len(data) {
					return Value{}, 0, ErrTruncatedRecord
				}
				blob := make([]byte, blobLen)
				copy(blob, data[off:off+int(blobLen)])
				return BlobValue(blob), int(blobLen), nil
			}
			// TEXT of (N-13)/2 bytes
			textLen := (serialType - 13) / 2
			if off+int(textLen) > len(data) {
				return Value{}, 0, ErrTruncatedRecord
			}
			text := string(data[off : off+int(textLen)])
			return TextValue(text), int(textLen), nil
		}
		return Value{}, 0, ErrBadSerialType
	}
}

// encodeSerialValue encodes a Value into its serial type and body bytes.
// serialValueSize returns the number of bytes the encoded body occupies.
func serialValueSize(v Value) int {
	switch v.Type {
	case DataTypeNull:
		return 0
	case DataTypeInteger:
		return integerValueSize(v.IntVal)
	case DataTypeFloat:
		return 8
	case DataTypeText:
		return len(v.TextVal)
	case DataTypeBlob:
		return len(v.BlobVal)
	}
	return 0
}

func integerValueSize(v int64) int {
	if v == 0 || v == 1 {
		return 0
	}
	if v >= -128 && v <= 127 {
		return 1
	}
	if v >= -32768 && v <= 32767 {
		return 2
	}
	if v >= -8388608 && v <= 8388607 {
		return 3
	}
	if v >= -2147483648 && v <= 2147483647 {
		return 4
	}
	if v >= -140737488355328 && v <= 140737488355327 {
		return 6
	}
	return 8
}

// encodeSerialValueInto writes the serial type into stBuf at stOff, and body bytes into bodyBuf at bodyOff.
// Returns (serialType, stBytesWritten, bodyBytesWritten).
func encodeSerialValueInto(v Value, stBuf []byte, stOff int, bodyBuf []byte, bodyOff int) (uint64, int, int) {
	switch v.Type {
	case DataTypeNull:
		return 0, EncodeVarintInto(0, stBuf, stOff), 0
	case DataTypeInteger:
		st := integerSerialType(v.IntVal)
		nst := EncodeVarintInto(st, stBuf, stOff)
		nbody := encodeIntegerInto(v.IntVal, bodyBuf, bodyOff)
		return st, nst, nbody
	case DataTypeFloat:
		nst := EncodeVarintInto(7, stBuf, stOff)
		bits := math.Float64bits(v.FloatVal)
		binary.BigEndian.PutUint64(bodyBuf[bodyOff:], bits)
		return 7, nst, 8
	case DataTypeText:
		serialType := uint64(len(v.TextVal)*2 + 13)
		nst := EncodeVarintInto(serialType, stBuf, stOff)
		copy(bodyBuf[bodyOff:], v.TextVal)
		return serialType, nst, len(v.TextVal)
	case DataTypeBlob:
		serialType := uint64(len(v.BlobVal)*2 + 12)
		nst := EncodeVarintInto(serialType, stBuf, stOff)
		copy(bodyBuf[bodyOff:], v.BlobVal)
		return serialType, nst, len(v.BlobVal)
	}
	return 0, EncodeVarintInto(0, stBuf, stOff), 0
}

// computeSerialType returns the serial type for a value without allocating.
func computeSerialType(v Value) uint64 {
	switch v.Type {
	case DataTypeNull:
		return 0
	case DataTypeInteger:
		return integerSerialType(v.IntVal)
	case DataTypeFloat:
		return 7
	case DataTypeText:
		return uint64(len(v.TextVal)*2 + 13)
	case DataTypeBlob:
		return uint64(len(v.BlobVal)*2 + 12)
	}
	return 0
}

// encodeValueInto writes the body bytes for a value directly into buf at off.
// Returns bytes written.
func encodeValueInto(v Value, buf []byte, off int) int {
	switch v.Type {
	case DataTypeNull:
		return 0
	case DataTypeInteger:
		return encodeIntegerInto(v.IntVal, buf, off)
	case DataTypeFloat:
		binary.BigEndian.PutUint64(buf[off:], math.Float64bits(v.FloatVal))
		return 8
	case DataTypeText:
		copy(buf[off:], v.TextVal)
		return len(v.TextVal)
	case DataTypeBlob:
		copy(buf[off:], v.BlobVal)
		return len(v.BlobVal)
	}
	return 0
}

func integerSerialType(v int64) uint64 {
	if v == 0 {
		return 8
	}
	if v == 1 {
		return 9
	}
	if v >= -128 && v <= 127 {
		return 1
	}
	if v >= -32768 && v <= 32767 {
		return 2
	}
	if v >= -8388608 && v <= 8388607 {
		return 3
	}
	if v >= -2147483648 && v <= 2147483647 {
		return 4
	}
	if v >= -140737488355328 && v <= 140737488355327 {
		return 5
	}
	return 6
}

func encodeIntegerInto(v int64, buf []byte, off int) int {
	if v == 0 || v == 1 {
		return 0
	}
	if v >= -128 && v <= 127 {
		buf[off] = byte(v)
		return 1
	}
	if v >= -32768 && v <= 32767 {
		binary.BigEndian.PutUint16(buf[off:], uint16(int16(v)))
		return 2
	}
	if v >= -8388608 && v <= 8388607 {
		buf[off] = byte(v >> 16)
		buf[off+1] = byte(v >> 8)
		buf[off+2] = byte(v)
		return 3
	}
	if v >= -2147483648 && v <= 2147483647 {
		binary.BigEndian.PutUint32(buf[off:], uint32(int32(v)))
		return 4
	}
	if v >= -140737488355328 && v <= 140737488355327 {
		buf[off] = byte(v >> 40)
		buf[off+1] = byte(v >> 32)
		buf[off+2] = byte(v >> 24)
		buf[off+3] = byte(v >> 16)
		buf[off+4] = byte(v >> 8)
		buf[off+5] = byte(v)
		return 6
	}
	binary.BigEndian.PutUint64(buf[off:], uint64(v))
	return 8
}

// encodeSerialValue is the allocating version kept for test compatibility.
func encodeSerialValue(v Value) (uint64, []byte) {
	switch v.Type {
	case DataTypeNull:
		return 0, nil
	case DataTypeInteger:
		return encodeIntegerAlloc(v.IntVal)
	case DataTypeFloat:
		var buf [8]byte
		binary.BigEndian.PutUint64(buf[:], math.Float64bits(v.FloatVal))
		return 7, buf[:]
	case DataTypeText:
		return uint64(len(v.TextVal)*2 + 13), []byte(v.TextVal)
	case DataTypeBlob:
		return uint64(len(v.BlobVal)*2 + 12), v.BlobVal
	}
	return 0, nil
}

func encodeIntegerAlloc(v int64) (uint64, []byte) {
	if v == 0 {
		return 8, nil
	}
	if v == 1 {
		return 9, nil
	}
	if v >= -128 && v <= 127 {
		return 1, []byte{byte(v)}
	}
	if v >= -32768 && v <= 32767 {
		var buf [2]byte
		binary.BigEndian.PutUint16(buf[:], uint16(int16(v)))
		return 2, buf[:]
	}
	if v >= -8388608 && v <= 8388607 {
		var buf [3]byte
		buf[0] = byte(v >> 16)
		buf[1] = byte(v >> 8)
		buf[2] = byte(v)
		return 3, buf[:]
	}
	if v >= -2147483648 && v <= 2147483647 {
		var buf [4]byte
		binary.BigEndian.PutUint32(buf[:], uint32(int32(v)))
		return 4, buf[:]
	}
	if v >= -140737488355328 && v <= 140737488355327 {
		var buf [6]byte
		buf[0] = byte(v >> 40)
		buf[1] = byte(v >> 32)
		buf[2] = byte(v >> 24)
		buf[3] = byte(v >> 16)
		buf[4] = byte(v >> 8)
		buf[5] = byte(v)
		return 5, buf[:]
	}
	var buf [8]byte
	binary.BigEndian.PutUint64(buf[:], uint64(v))
	return 6, buf[:]
}

// ---------------------------------------------------------------------------
// Payload overflow computation
// ---------------------------------------------------------------------------

// ComputePayloadSizes computes how many bytes of a payload fit on the current
// page (localSize) vs how many must go to overflow pages (overflowSize).
//
// Parameters:
//   - payloadLen: total payload size in bytes
//   - usableSize: usable page size (pageSize - reservedSpace)
//   - isLeaf: true for leaf table B-tree cells, false for interior
//
// For leaf table cells:
//
//	maxLocal = usableSize - 35
//	minLocal = ((usableSize - 12) * 32 / 255) - 23
//
// For interior table cells:
//
//	maxLocal = ((usableSize - 12) * 64 / 255) - 23
//	minLocal = ((usableSize - 12) * 32 / 255) - 23
func ComputePayloadSizes(payloadLen, usableSize int, isLeaf bool) (localSize, overflowSize int) {
	var maxLocal, minLocal int

	if isLeaf {
		maxLocal = usableSize - 35
		minLocal = ((usableSize-12)*32/255 - 23)
	} else {
		maxLocal = ((usableSize-12)*64/255 - 23)
		minLocal = ((usableSize-12)*32/255 - 23)
	}

	// Ensure maxLocal >= minLocal (should always be true for valid page sizes)
	if maxLocal < minLocal {
		maxLocal = minLocal
	}

	if payloadLen <= maxLocal {
		// Everything fits locally
		return payloadLen, 0
	}

	// Payload overflows. Store minLocal bytes (or up to maxLocal if the
	// surplus after removing the overflow pointer is <= maxLocal).
	// The formula from SQLite:
	//   localSize = minLocal + ((payloadLen - minLocal) % (usableSize - 4))
	//   if localSize > maxLocal: localSize = minLocal
	localSize = minLocal + ((payloadLen - minLocal) % (usableSize - 4))
	if localSize > maxLocal {
		localSize = minLocal
	}

	overflowSize = payloadLen - localSize
	return localSize, overflowSize
}

// ---------------------------------------------------------------------------
// Overflow page helpers
// ---------------------------------------------------------------------------

// OverflowPageHeaderSize is the size of an overflow page header (4 bytes for
// the next overflow page pointer).
const OverflowPageHeaderSize = 4

// MaxOverflowPayload returns the maximum payload bytes that fit on a single
// overflow page.
func MaxOverflowPayload(usableSize int) int {
	return usableSize - OverflowPageHeaderSize
}

// ReadOverflowPage reads an overflow page, returning the next overflow page
// number (0 = end of chain) and the payload data.
func ReadOverflowPage(data []byte, usableSize int) (nextPage uint32, payload []byte, err error) {
	if len(data) < usableSize {
		return 0, nil, errors.New("sqlite: overflow page too small")
	}
	nextPage = binary.BigEndian.Uint32(data[0:4])
	payload = make([]byte, usableSize-OverflowPageHeaderSize)
	copy(payload, data[OverflowPageHeaderSize:usableSize])
	return nextPage, payload, nil
}

// WriteOverflowPage serializes an overflow page.
func WriteOverflowPage(nextPage uint32, payload []byte, usableSize int) []byte {
	buf := make([]byte, usableSize)
	binary.BigEndian.PutUint32(buf[0:4], nextPage)
	copy(buf[OverflowPageHeaderSize:], payload)
	return buf
}

// ---------------------------------------------------------------------------
// Freelist helpers
// ---------------------------------------------------------------------------

// FreelistTrunk represents a freelist trunk page.
type FreelistTrunk struct {
	NextTrunkPage uint32
	LeafPages     []uint32
}

// ReadFreelistTrunk parses a freelist trunk page.
func ReadFreelistTrunk(data []byte, usableSize int) (*FreelistTrunk, error) {
	if len(data) < usableSize {
		return nil, errors.New("sqlite: freelist trunk page too small")
	}

	nextTrunk := binary.BigEndian.Uint32(data[0:4])
	leafCount := int(binary.BigEndian.Uint32(data[4:8]))
	maxLeafCount := (usableSize - 8) / 4
	if leafCount > maxLeafCount {
		leafCount = maxLeafCount
	}

	leaves := make([]uint32, leafCount)
	for i := 0; i < leafCount; i++ {
		leaves[i] = binary.BigEndian.Uint32(data[8+i*4 : 12+i*4])
	}

	return &FreelistTrunk{
		NextTrunkPage: nextTrunk,
		LeafPages:     leaves,
	}, nil
}

// WriteFreelistTrunk serializes a freelist trunk page.
func WriteFreelistTrunk(trunk *FreelistTrunk, usableSize int) []byte {
	buf := make([]byte, usableSize)
	binary.BigEndian.PutUint32(buf[0:4], trunk.NextTrunkPage)
	binary.BigEndian.PutUint32(buf[4:8], uint32(len(trunk.LeafPages)))
	for i, lp := range trunk.LeafPages {
		binary.BigEndian.PutUint32(buf[8+i*4:], lp)
	}
	return buf
}

// ---------------------------------------------------------------------------
// Page cell pointer array helpers
// ---------------------------------------------------------------------------

// ReadCellPointers reads the 2-byte cell pointer array from a page.
// offset is where the cell pointer array starts (after the page header).
func ReadCellPointers(data []byte, offset int, count int) []uint16 {
	ptrs := make([]uint16, count)
	for i := 0; i < count; i++ {
		ptrs[i] = binary.BigEndian.Uint16(data[offset+i*2 : offset+i*2+2])
	}
	return ptrs
}

// WriteCellPointers writes a 2-byte cell pointer array.
func WriteCellPointers(ptrs []uint16) []byte {
	buf := make([]byte, len(ptrs)*2)
	for i, p := range ptrs {
		binary.BigEndian.PutUint16(buf[i*2:], p)
	}
	return buf
}
