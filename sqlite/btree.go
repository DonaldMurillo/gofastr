package sqlite

import (
	"encoding/binary"
	"errors"
	"fmt"
	"sort"
)

const maxCellsPerPage = 512

// BTree manages a B-tree structure for table data.
type BTree struct {
	pager *Pager
}

// NewBTree creates a new BTree using the given pager.
func NewBTree(pager *Pager) *BTree {
	return &BTree{pager: pager}
}

// CreateBTree creates a new B-tree (single leaf page) and returns the root page number.
func (b *BTree) CreateBTree() (int, error) {
	pageNum, err := b.pager.AllocatePage()
	if err != nil {
		return 0, err
	}

	data := make([]byte, b.pager.GetPageSize())
	offset := b.pager.PageHeaderOffset(pageNum)
	data[offset] = pageTypeLeafTable
	ps := b.pager.GetPageSize()
	binary.BigEndian.PutUint16(data[offset+5:], uint16(ps))

	if err := b.pager.SetPageData(pageNum, data); err != nil {
		return 0, err
	}
	return pageNum, nil
}

// Insert inserts or replaces a record at the given rowid.
func (b *BTree) Insert(rootPage int, rowid int64, record *Record) error {
	payload := WriteRecord(record)
	cell := b.buildLeafCell(rowid, payload)
	return b.insertIntoPage(rootPage, rowid, cell)
}

func (b *BTree) buildLeafCell(rowid int64, payload []byte) []byte {
	sizeBuf := EncodeVarint(int64(len(payload)))
	rowidBuf := EncodeVarint(rowid)
	cell := make([]byte, 0, len(sizeBuf)+len(rowidBuf)+len(payload))
	cell = append(cell, sizeBuf...)
	cell = append(cell, rowidBuf...)
	cell = append(cell, payload...)
	return cell
}

func (b *BTree) insertIntoPage(pageNum int, rowid int64, cell []byte) error {
	data, err := b.pager.GetPageDataMutable(pageNum)
	if err != nil {
		return err
	}

	offset := b.pager.PageHeaderOffset(pageNum)
	pageSize := b.pager.GetPageSize()

	// Read page type and cell count inline — avoids ReadPageHeaderFrom struct overhead.
	// Direct big-endian decode avoids binary.BigEndian method call overhead.
	hdr := data[offset:]
	pageType := hdr[0]
	cellCount := int(uint16(hdr[3])<<8 | uint16(hdr[4]))
	contentOffset := uint16(hdr[5])<<8 | uint16(hdr[6])

	if pageType == pageTypeInteriorTable {
		// Binary search interior cells directly on raw page data
		// without allocating a slice.
		headerEnd := offset + 12
		intCellCount := cellCount
		intRightMost := int(binary.BigEndian.Uint32(hdr[8:12]))
		childPage := intRightMost // default: rightmost child for rowid > all

		// Decode a cell rowid at a given index
		cellRowid := func(idx int) (int64, uint32, bool) {
			ptrOff := headerEnd + idx*2
			if ptrOff+2 > len(data) {
				return 0, 0, false
			}
			cellOff := int(binary.BigEndian.Uint16(data[ptrOff : ptrOff+2]))
			if cellOff == 0 || cellOff+4 >= len(data) {
				return 0, 0, false
			}
			lc := binary.BigEndian.Uint32(data[cellOff : cellOff+4])
			rid, _, err := DecodeVarint(data[cellOff+4:])
			if err != nil {
				return 0, 0, false
			}
			return rid, lc, true
		}

		// Find the correct child: left child of first cell where rowid <= cell.rowid
		found := false
		lo, hi := 0, intCellCount
		for lo < hi {
			mid := lo + (hi-lo)/2
			rid, lc, ok := cellRowid(mid)
			if !ok {
				break
			}
			if rowid <= rid {
				childPage = int(lc)
				found = true
				hi = mid // keep searching left for smaller match
			} else {
				lo = mid + 1
			}
		}
		// If no cell matched (rowid > all), childPage stays as RightMostPtr
		_ = found

		if childPage == 0 {
			return errors.New("no child page found")
		}
		return b.insertIntoPage(childPage, rowid, cell)
	}

	if pageType != pageTypeLeafTable {
		return errors.New("expected leaf table page")
	}

	headerEnd := offset + 8
	cellPtrStart := headerEnd // cell pointer array starts here

	// Binary search for insert position
	insertIdx := cellCount
	replaceIdx := -1

	// Helper to extract rowid from cell pointer
	cellRowid := func(idx int) (int64, bool) {
		ptrOff := cellPtrStart + idx*2
		if ptrOff+2 > len(data) {
			return 0, false
		}
		cellOff := int(binary.BigEndian.Uint16(data[ptrOff : ptrOff+2]))
		if cellOff == 0 || cellOff >= len(data) {
			return 0, false
		}
		_, n1, err := DecodeVarint(data[cellOff:])
		if err != nil {
			return 0, false
		}
		rid, _, err := DecodeVarint(data[cellOff+n1:])
		if err != nil {
			return 0, false
		}
		return rid, true
	}

	// True binary search
	lo, hi := 0, cellCount
	for lo < hi {
		mid := lo + (hi-lo)/2
		rid, ok := cellRowid(mid)
		if !ok {
			break
		}
		if rid == rowid {
			replaceIdx = mid
			insertIdx = mid
			break
		}
		if rid < rowid {
			lo = mid + 1
		} else {
			hi = mid
		}
	}
	if replaceIdx < 0 {
		insertIdx = lo
	}

	// Use ContentOffset from page header (maintained by writeLeafPage)
	// to find where cell content area starts — avoids scanning all cell pointers.
	firstContent := int(contentOffset)
	if firstContent == 0 || firstContent > pageSize {
		firstContent = pageSize
		// Fallback: scan cell pointers for minimum offset
		for i := 0; i < cellCount; i++ {
			ptrOff := cellPtrStart + i*2
			if ptrOff+2 <= len(data) {
				co := int(binary.BigEndian.Uint16(data[ptrOff : ptrOff+2]))
				if co > 0 && co < firstContent {
					firstContent = co
				}
			}
		}
	}

	cellPtrEnd := cellPtrStart + cellCount*2
	cellLen := len(cell)
	newPtrEnd := cellPtrEnd
	if replaceIdx < 0 {
		newPtrEnd = cellPtrEnd + 2 // one more pointer
	}

	// For replaces, check if new cell fits at the same position
	if replaceIdx >= 0 {
		replacePtrOff := cellPtrStart + replaceIdx*2
		oldCellOff := int(uint16(data[replacePtrOff])<<8 | uint16(data[replacePtrOff+1]))
		// Decode old cell size
		oldPayloadSize, n1, _ := DecodeVarint(data[oldCellOff:])
		_, n2, _ := DecodeVarint(data[oldCellOff+n1:])
		oldCellEnd := oldCellOff + n1 + n2 + int(oldPayloadSize)
		oldCellLen := oldCellEnd - oldCellOff

		if cellLen <= oldCellLen {
			// New cell fits in same slot — write it
			copy(data[oldCellOff:], cell)
			// Zero out remaining bytes if new cell is smaller
			for i := oldCellOff + cellLen; i < oldCellEnd; i++ {
				data[i] = 0
			}
			return nil
		}
		// New cell is larger — fall through to slow path (rewrite page)
	}

	// Check if cell fits without reading all cells
	if replaceIdx < 0 && firstContent-newPtrEnd >= cellLen {
		// Fast path: write cell directly into free space
		cellWriteOff := firstContent - cellLen

		// Shift pointers from insertIdx onward right by 2 bytes
		for j := cellCount; j > insertIdx; j-- {
			srcOff := cellPtrStart + (j-1)*2
			v := uint16(data[srcOff])<<8 | uint16(data[srcOff+1])
			dstOff := cellPtrStart + j*2
			data[dstOff] = byte(v >> 8)
			data[dstOff+1] = byte(v)
		}
		copy(data[cellWriteOff:], cell)
		dstOff := cellPtrStart + insertIdx*2
		data[dstOff] = byte(cellWriteOff >> 8)
		data[dstOff+1] = byte(cellWriteOff)

		// Update cell count
		cnt := uint16(cellCount + 1)
		data[offset+3] = byte(cnt >> 8)
		data[offset+4] = byte(cnt)

		// Update ContentOffset to reflect new cell position
		data[offset+5] = byte(cellWriteOff >> 8)
		data[offset+6] = byte(cellWriteOff)

		return nil
	}

	// Slow path: need to read all cells and possibly split
	ph := PageHeader{
		PageType:      pageType,
		CellCount:      uint16(cellCount),
		ContentOffset:  contentOffset,
	}
	cells := b.readLeafCells(data, offset, ph)

	if replaceIdx >= 0 {
		cells[replaceIdx] = leafCell{rowid: rowid, data: cell}
		return b.writeLeafPage(pageNum, cells)
	}

	idx := sort.Search(len(cells), func(i int) bool {
		return cells[i].rowid >= rowid
	})
	cells = append(cells, leafCell{})
	copy(cells[idx+1:], cells[idx:])
	cells[idx] = leafCell{rowid: rowid, data: cell}

	if b.cellsFitInPage(cells, pageSize, offset) <= 0 {
		return b.writeLeafPage(pageNum, cells)
	}

	return b.splitAndInsert(pageNum, cells)
}

type leafCell struct {
	rowid int64
	data  []byte
}

func (b *BTree) readLeafCells(data []byte, offset int, ph PageHeader) []leafCell {
	if ph.CellCount == 0 {
		return nil
	}

	var cells []leafCell
	headerEnd := offset + 8

	for i := 0; i < int(ph.CellCount) && i < maxCellsPerPage; i++ {
		ptrOff := headerEnd + i*2
		if ptrOff+2 > len(data) {
			break
		}
		cellOff := int(binary.BigEndian.Uint16(data[ptrOff : ptrOff+2]))
		if cellOff == 0 || cellOff >= len(data) {
			continue
		}

		payloadSize, n1, err := DecodeVarint(data[cellOff:])
		if err != nil {
			continue
		}
		rowid, n2, err := DecodeVarint(data[cellOff+n1:])
		if err != nil {
			continue
		}

		cellEnd := cellOff + n1 + n2 + int(payloadSize)
		if cellEnd > len(data) {
			cellEnd = len(data)
		}
		cellData := data[cellOff:cellEnd]

		cells = append(cells, leafCell{rowid: rowid, data: cellData})
	}

	sort.Slice(cells, func(i, j int) bool {
		return cells[i].rowid < cells[j].rowid
	})

	return cells
}

func (b *BTree) cellsFitInPage(cells []leafCell, pageSize int, headerOffset int) int {
	totalCellData := 0
	for _, c := range cells {
		totalCellData += len(c.data)
	}
	needed := 8 + 2*len(cells) + totalCellData
	usable := pageSize - headerOffset
	return needed - usable
}

func (b *BTree) writeLeafPage(pageNum int, cells []leafCell) error {
	data, err := b.pager.GetPageData(pageNum)
	if err != nil {
		return err
	}

	offset := b.pager.PageHeaderOffset(pageNum)
	pageSize := b.pager.GetPageSize()

	// Clear usable area
	for i := offset; i < pageSize; i++ {
		data[i] = 0
	}

	// Write page header
	data[offset] = pageTypeLeafTable
	binary.BigEndian.PutUint16(data[offset+3:], uint16(len(cells)))

	// Write cells from end of page backward
	contentStart := uint16(pageSize)
	headerEnd := offset + 8

	for i, cell := range cells {
		cellLen := uint16(len(cell.data))
		contentStart -= cellLen
		copy(data[contentStart:], cell.data)
		binary.BigEndian.PutUint16(data[headerEnd+i*2:], contentStart)
	}

	if contentStart < uint16(pageSize) {
		binary.BigEndian.PutUint16(data[offset+5:], contentStart)
	} else {
		binary.BigEndian.PutUint16(data[offset+5:], uint16(pageSize))
	}

	return b.pager.SetPageData(pageNum, data)
}

func (b *BTree) splitAndInsert(pageNum int, cells []leafCell) error {
	if len(cells) < 2 {
		return b.writeLeafPage(pageNum, cells)
	}

	mid := len(cells) / 2

	// Allocate two new leaf pages
	leftPageNum, err := b.pager.AllocatePage()
	if err != nil {
		return err
	}
	rightPageNum, err := b.pager.AllocatePage()
	if err != nil {
		return err
	}

	// Initialize both as leaf pages — AllocatePage already gave us zeroed buffers
	for _, pn := range []int{leftPageNum, rightPageNum} {
		d, err := b.pager.GetPageDataMutable(pn)
		if err != nil {
			return err
		}
		o := b.pager.PageHeaderOffset(pn)
		d[o] = pageTypeLeafTable
		binary.BigEndian.PutUint16(d[o+5:], uint16(b.pager.GetPageSize()))
	}

	if err := b.writeLeafPage(leftPageNum, cells[:mid]); err != nil {
		return err
	}
	if err := b.writeLeafPage(rightPageNum, cells[mid:]); err != nil {
		return err
	}

	// Convert the original page into an interior node (reuse existing buffer)
	interiorData, err := b.pager.GetPageDataMutable(pageNum)
	if err != nil {
		return err
	}
	interiorOffset := b.pager.PageHeaderOffset(pageNum)

	// Clear the page
	for i := interiorOffset; i < len(interiorData); i++ {
		interiorData[i] = 0
	}

	interiorData[interiorOffset] = pageTypeInteriorTable
	binary.BigEndian.PutUint16(interiorData[interiorOffset+3:], 1) // 1 cell
	binary.BigEndian.PutUint32(interiorData[interiorOffset+8:], uint32(rightPageNum))

	// Divider cell: leftChild(4 bytes) + varint(dividerRowid)
	dividerRowid := cells[:mid][len(cells[:mid])-1].rowid
	rowidBuf := EncodeVarint(dividerRowid)
	cellSize := 4 + len(rowidBuf)
	cellStart := b.pager.GetPageSize() - cellSize
	binary.BigEndian.PutUint32(interiorData[cellStart:], uint32(leftPageNum))
	copy(interiorData[cellStart+4:], rowidBuf)

	headerEnd := interiorOffset + 12
	binary.BigEndian.PutUint16(interiorData[headerEnd:], uint16(cellStart))
	binary.BigEndian.PutUint16(interiorData[interiorOffset+5:], uint16(cellStart))

	return nil
}

// Delete removes a record by rowid from the B-tree.
func (b *BTree) Delete(rootPage int, rowid int64) error {
	return b.deleteFromPage(rootPage, rowid)
}

func (b *BTree) deleteFromPage(pageNum int, rowid int64) error {
	data, err := b.pager.GetPageData(pageNum)
	if err != nil {
		return err
	}

	offset := b.pager.PageHeaderOffset(pageNum)
	hdr := data[offset:]
	pageType := hdr[0]

	if pageType == pageTypeInteriorTable {
		ph := ReadPageHeaderFrom(hdr)
		intCells := b.readInteriorCells(data, offset, ph)
		childPage := int(ph.RightMostPtr)
		for _, c := range intCells {
			if rowid <= c.rowid {
				childPage = int(c.leftChild)
				break
			}
		}
		if childPage == 0 {
			return nil
		}
		return b.deleteFromPage(childPage, rowid)
	}

	ph := PageHeader{PageType: pageType, CellCount: binary.BigEndian.Uint16(hdr[3:5]), ContentOffset: binary.BigEndian.Uint16(hdr[5:7])}
	cells := b.readLeafCells(data, offset, ph)

	for i, c := range cells {
		if c.rowid == rowid {
			cells = append(cells[:i], cells[i+1:]...)
			return b.writeLeafPage(pageNum, cells)
		}
	}

	return nil
}

// Search looks up a record by rowid.
func (b *BTree) Search(rootPage int, rowid int64) (*Record, error) {
	data, err := b.pager.GetPageDataReadOnly(rootPage)
	if err != nil {
		return nil, err
	}

	offset := b.pager.PageHeaderOffset(rootPage)
	pageType := data[offset]

	ph := PageHeader{
		PageType:     pageType,
		CellCount:    binary.BigEndian.Uint16(data[offset+3 : offset+5]),
		ContentOffset: binary.BigEndian.Uint16(data[offset+5 : offset+7]),
	}
	if pageType == pageTypeInteriorTable {
		ph.RightMostPtr = binary.BigEndian.Uint32(data[offset+8 : offset+12])
	}

	if pageType == pageTypeLeafTable {
		return b.searchLeaf(data, offset, ph, rowid)
	}
	if pageType == pageTypeInteriorTable {
		return b.searchInterior(rootPage, data, offset, ph, rowid)
	}

	return nil, errors.New("unexpected page type in search")
}

func (b *BTree) searchLeaf(data []byte, offset int, ph PageHeader, rowid int64) (*Record, error) {
	cells := b.readLeafCells(data, offset, ph)

	idx := sort.Search(len(cells), func(i int) bool {
		return cells[i].rowid >= rowid
	})

	if idx < len(cells) && cells[idx].rowid == rowid {
		return b.parseCellRecord(cells[idx].data)
	}

	return nil, nil
}

func (b *BTree) searchInterior(pageNum int, data []byte, offset int, ph PageHeader, rowid int64) (*Record, error) {
	// Zero-alloc binary search on interior cells
	headerEnd := offset + 12
	cellCount := int(ph.CellCount)
	childPage := int(ph.RightMostPtr) // default: rightmost child

	for i := 0; i < cellCount; i++ {
		ptrOff := headerEnd + i*2
		if ptrOff+2 > len(data) {
			break
		}
		cellOff := int(binary.BigEndian.Uint16(data[ptrOff : ptrOff+2]))
		if cellOff == 0 || cellOff+4 >= len(data) {
			continue
		}
		lc := binary.BigEndian.Uint32(data[cellOff : cellOff+4])
		rid, _, err := DecodeVarint(data[cellOff+4:])
		if err != nil {
			continue
		}
		if rowid < rid {
			childPage = int(lc)
			break
		}
	}

	if childPage == 0 {
		return nil, nil
	}
	return b.Search(childPage, rowid)
}

type interiorCell struct {
	rowid     int64
	leftChild uint32
}

func (b *BTree) readInteriorCells(data []byte, offset int, ph PageHeader) []interiorCell {
	var cells []interiorCell
	headerEnd := offset + 12

	for i := 0; i < int(ph.CellCount) && i < maxCellsPerPage; i++ {
		ptrOff := headerEnd + i*2
		if ptrOff+2 > len(data) {
			break
		}
		cellOff := int(binary.BigEndian.Uint16(data[ptrOff : ptrOff+2]))
		if cellOff == 0 || cellOff+4 >= len(data) {
			continue
		}

		leftChild := binary.BigEndian.Uint32(data[cellOff : cellOff+4])
		rowid, _, err := DecodeVarint(data[cellOff+4:])
		if err != nil {
			continue
		}

		cells = append(cells, interiorCell{rowid: rowid, leftChild: leftChild})
	}

	// Cells are written in rowid order during insert/split.
	// Only sort if disorder detected (defensive).
	needSort := false
	for i := 1; i < len(cells); i++ {
		if cells[i].rowid < cells[i-1].rowid {
			needSort = true
			break
		}
	}
	if needSort {
		sort.Slice(cells, func(i, j int) bool {
			return cells[i].rowid < cells[j].rowid
		})
	}

	return cells
}

func (b *BTree) parseCellRecord(cellData []byte) (*Record, error) {
	_, n1, err := DecodeVarint(cellData)
	if err != nil {
		return nil, err
	}
	_, n2, err := DecodeVarint(cellData[n1:])
	if err != nil {
		return nil, err
	}

	recordStart := n1 + n2
	if recordStart >= len(cellData) {
		return &Record{}, nil
	}

	return ReadRecord(cellData[recordStart:])
}

// parseCellRecordInto reuses the cursor's recordBuf to avoid allocation.
func (c *BTreeCursor) parseCellRecordInto(cellData []byte) error {
	_, n1, err := DecodeVarint(cellData)
	if err != nil {
		return err
	}
	_, n2, err := DecodeVarint(cellData[n1:])
	if err != nil {
		return err
	}

	recordStart := n1 + n2
	if recordStart >= len(cellData) {
		c.record = Record{}
		return nil
	}

	err = ReadRecordIntoRecord(cellData[recordStart:], &c.record, c.recordBuf)
	if err != nil {
		return err
	}
	c.recordBuf = c.record.Columns
	return nil
}

// Scan returns a cursor for iterating all rows in the B-tree.
// SeekToRowid positions the cursor at the first row with rowid >= target.
// This is equivalent to a B-tree seek followed by sequential scan.
func (c *BTreeCursor) SeekToRowid(target int64) error {
	c.navStack = nil
	c.currentPage = 0
	c.cells = nil
	c.index = 0
	c.eof = false
	return c.descendToRowid(c.rootPage, target)
}

func (c *BTreeCursor) descendToRowid(pageNum int, target int64) error {
	data, err := c.btree.pager.GetPageDataReadOnly(pageNum)
	if err != nil {
		return err
	}
	offset := c.btree.pager.PageHeaderOffset(pageNum)
	ph := ReadPageHeaderFrom(data[offset:])

	if ph.PageType == pageTypeLeafTable {
		c.currentPage = pageNum
		c.cells = c.btree.readLeafCells(data, offset, ph)
		// Binary search to find first cell >= target
		idx := sort.Search(len(c.cells), func(i int) bool {
			return c.cells[i].rowid >= target
		})
		c.index = idx
		if idx >= len(c.cells) {
			// Need to advance to next leaf
			if err := c.advanceToNextLeaf(); err != nil {
				c.eof = true
			}
		}
		return nil
	}

	if ph.PageType != pageTypeInteriorTable {
		return fmt.Errorf("unexpected page type %d", ph.PageType)
	}

	intCells := c.btree.readInteriorCells(data, offset, ph)

	// Find the child to descend into
	// Interior cells are sorted by rowid. Each cell has leftChild.
	// If target < cells[0].rowid, go to cells[0].leftChild
	// If target >= cells[i].rowid and target < cells[i+1].rowid, go to cells[i+1].leftChild
	// If target >= cells[last].rowid, go to rightMostPtr
	childIdx := sort.Search(len(intCells), func(i int) bool {
		return intCells[i].rowid >= target
	})

	var childPage int
	if childIdx < len(intCells) {
		childPage = int(intCells[childIdx].leftChild)
	} else {
		childPage = int(ph.RightMostPtr)
	}

	// Push this interior node onto nav stack for advanceToNextLeaf
	c.navStack = append(c.navStack, cursorNavEntry{pageNum: pageNum, childIdx: childIdx})

	return c.descendToRowid(childPage, target)
}

func (b *BTree) Scan(rootPage int) (CursorInterface, error) {
	cursor := &BTreeCursor{
		btree:    b,
		rootPage: rootPage,
		started:  false,
		eof:      false,
	}

	if err := cursor.goToLeftmost(); err != nil {
		return nil, err
	}
	return cursor, nil
}

// BTreeCursor iterates over rows in a B-tree in rowid order.
// Uses lazy page-by-page iteration instead of eager collection.
type BTreeCursor struct {
	btree    *BTree
	rootPage int

	// Current leaf page state
	currentPage int    // page number of current leaf
	cells       []leafCell // cells on current page (sub-slices into page data)
	index       int    // index within cells

	// Navigation stack for traversing interior nodes
	// Each entry: (interiorPageNum, childIndex)
	navStack []cursorNavEntry

	started bool
	eof     bool
	rowid   int64
	record  Record // embedded — no per-row *Record alloc

	// Reusable buffer for ReadRecordInto to avoid per-row allocation
	recordBuf []Value
}

type cursorNavEntry struct {
	pageNum int
	childIdx int // which child we're about to visit
}

func (c *BTreeCursor) goToLeftmost() error {
	c.navStack = nil
	c.currentPage = 0
	c.cells = nil
	c.index = 0

	// Walk down to the leftmost leaf
	return c.descendToLeftmost(c.rootPage)
}

func (c *BTreeCursor) descendToLeftmost(pageNum int) error {
	for {
		data, err := c.btree.pager.GetPageDataReadOnly(pageNum)
		if err != nil {
			return err
		}
		offset := c.btree.pager.PageHeaderOffset(pageNum)
		hdr := data[offset:]
		pageType := hdr[0]

		if pageType == pageTypeLeafTable {
			ph := PageHeader{
				PageType:      pageType,
				CellCount:     binary.BigEndian.Uint16(hdr[3:5]),
				ContentOffset: binary.BigEndian.Uint16(hdr[5:7]),
			}
			c.currentPage = pageNum
			c.cells = c.btree.readLeafCells(data, offset, ph)
			c.index = 0
			return nil
		}

		if pageType != pageTypeInteriorTable {
			return fmt.Errorf("unexpected page type %d", pageType)
		}

		ph := ReadPageHeaderFrom(hdr)
		intCells := c.btree.readInteriorCells(data, offset, ph)

		// Push all children onto nav stack in reverse order (rightmost first)
		// so we pop leftmost first
		c.navStack = append(c.navStack, cursorNavEntry{pageNum: pageNum, childIdx: 0})

		// Descend to first (leftmost) child
		if len(intCells) > 0 {
			pageNum = int(intCells[0].leftChild)
		} else {
			pageNum = int(ph.RightMostPtr)
		}
	}
}

// advanceToNextLeaf moves the cursor to the next leaf page.
func (c *BTreeCursor) advanceToNextLeaf() error {
	// Pop nav entries to find the next child to visit
	for len(c.navStack) > 0 {
		entry := c.navStack[len(c.navStack)-1]

		data, err := c.btree.pager.GetPageDataReadOnly(entry.pageNum)
		if err != nil {
			return err
		}
		offset := c.btree.pager.PageHeaderOffset(entry.pageNum)
		ph := ReadPageHeaderFrom(data[offset:])
		intCells := c.btree.readInteriorCells(data, offset, ph)

		numChildren := len(intCells) + 1 // left children + rightmost
		if entry.childIdx+1 < numChildren {
			// There's another child to visit
			c.navStack[len(c.navStack)-1].childIdx++

			var nextChild int
			if entry.childIdx+1 < len(intCells) {
				nextChild = int(intCells[entry.childIdx+1].leftChild)
			} else {
				nextChild = int(ph.RightMostPtr)
			}

			return c.descendToLeftmost(nextChild)
		}

		// All children of this interior node visited, pop
		c.navStack = c.navStack[:len(c.navStack)-1]
	}

	// No more leaves
	c.eof = true
	return nil
}

func (c *BTreeCursor) collectLeaves(pageNum int) error {
	data, err := c.btree.pager.GetPageDataReadOnly(pageNum)
	if err != nil {
		return err
	}

	offset := c.btree.pager.PageHeaderOffset(pageNum)
	hdr := data[offset:]
	pageType := hdr[0]

	if pageType == pageTypeLeafTable {
		ph := PageHeader{
			PageType:      pageType,
			CellCount:     binary.BigEndian.Uint16(hdr[3:5]),
			ContentOffset: binary.BigEndian.Uint16(hdr[5:7]),
		}
		c.cells = c.btree.readLeafCells(data, offset, ph)
		return nil
	}

	if pageType == pageTypeInteriorTable {
		ph := ReadPageHeaderFrom(hdr)
		intCells := c.btree.readInteriorCells(data, offset, ph)

		for _, ic := range intCells {
			if err := c.collectLeaves(int(ic.leftChild)); err != nil {
				return err
			}
		}
		if ph.RightMostPtr != 0 {
			if err := c.collectLeaves(int(ph.RightMostPtr)); err != nil {
				return err
			}
		}
	}
	return nil
}

// Next advances the cursor to the next row.
func (c *BTreeCursor) Next() bool {
	if c.eof {
		return false
	}

	if !c.started {
		c.started = true
		// Note: don't reset c.index here — SeekToRowid may have set it
	}

	// Try current page
	if c.index < len(c.cells) {
		cell := c.cells[c.index]
		c.rowid = cell.rowid
		if err := c.parseCellRecordInto(cell.data); err != nil {
			c.eof = true
			return false
		}
		c.index++
		return true
	}

	// Current page exhausted, move to next leaf
	if err := c.advanceToNextLeaf(); err != nil || c.eof {
		c.eof = true
		return false
	}

	// Try again on new page
	if c.index < len(c.cells) {
		cell := c.cells[c.index]
		c.rowid = cell.rowid
		if err := c.parseCellRecordInto(cell.data); err != nil {
			c.eof = true
			return false
		}
		c.index++
		return true
	}

	c.eof = true
	return false
}

// Get returns the current rowid and record.
func (c *BTreeCursor) Get() (int64, *Record, error) {
	if c.eof || !c.started {
		return 0, nil, nil
	}
	return c.rowid, &c.record, nil
}

// RawRecordData returns the raw record bytes for the current cell,
// skipping the payload-size and rowid varint prefixes.
func (c *BTreeCursor) RawRecordData() []byte {
	if c.eof || !c.started || c.index-1 < 0 || c.index-1 >= len(c.cells) {
		return nil
	}
	cellData := c.cells[c.index-1].data
	_, n1, err := DecodeVarint(cellData)
	if err != nil {
		return nil
	}
	_, n2, err := DecodeVarint(cellData[n1:])
	if err != nil {
		return nil
	}
	return cellData[n1+n2:]
}

// Close releases cursor resources.
func (c *BTreeCursor) Close() error {
	c.cells = nil
	c.navStack = nil
	c.eof = true
	return nil
}
