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
	ph := ReadPageHeaderFrom(data[offset:])

	if ph.PageType == pageTypeInteriorTable {
		// Navigate to the correct child page
		intCells := b.readInteriorCells(data, offset, ph)
		childPage := int(ph.RightMostPtr)
		for _, c := range intCells {
			if rowid <= c.rowid {
				childPage = int(c.leftChild)
				break
			}
		}
		if childPage == 0 {
			return errors.New("no child page found")
		}
		return b.insertIntoPage(childPage, rowid, cell)
	}

	if ph.PageType != pageTypeLeafTable {
		return errors.New("expected leaf table page")
	}

	cellCount := int(ph.CellCount)
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

	// Calculate free space
	// Cell content area grows from the end of the page toward the beginning.
	// Free space = space between cell pointer array end and first cell content.
	cellPtrEnd := cellPtrStart + cellCount*2
	firstContent := pageSize
	for i := 0; i < cellCount; i++ {
		ptrOff := cellPtrStart + i*2
		if ptrOff+2 <= len(data) {
			co := int(binary.BigEndian.Uint16(data[ptrOff : ptrOff+2]))
			if co > 0 && co < firstContent {
				firstContent = co
			}
		}
	}

	cellLen := len(cell)
	newPtrEnd := cellPtrEnd
	if replaceIdx < 0 {
		newPtrEnd = cellPtrEnd + 2 // one more pointer
	}

	// For replaces, check if new cell fits at the same position
	if replaceIdx >= 0 {
		replacePtrOff := cellPtrStart + replaceIdx*2
		oldCellOff := int(binary.BigEndian.Uint16(data[replacePtrOff : replacePtrOff+2]))
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
			binary.BigEndian.PutUint16(data[cellPtrStart+j*2:], binary.BigEndian.Uint16(data[srcOff:srcOff+2]))
		}
		copy(data[cellWriteOff:], cell)
		binary.BigEndian.PutUint16(data[cellPtrStart+insertIdx*2:], uint16(cellWriteOff))

		// Update cell count
		binary.BigEndian.PutUint16(data[offset+3:], uint16(cellCount+1))

		return nil
	}

	// Slow path: need to read all cells and possibly split
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

	// Initialize both as leaf pages
	for _, pn := range []int{leftPageNum, rightPageNum} {
		d := make([]byte, b.pager.GetPageSize())
		o := b.pager.PageHeaderOffset(pn)
		d[o] = pageTypeLeafTable
		binary.BigEndian.PutUint16(d[o+5:], uint16(b.pager.GetPageSize()))
		b.pager.SetPageData(pn, d)
	}

	if err := b.writeLeafPage(leftPageNum, cells[:mid]); err != nil {
		return err
	}
	if err := b.writeLeafPage(rightPageNum, cells[mid:]); err != nil {
		return err
	}

	// Convert the original page into an interior node
	interiorData := make([]byte, b.pager.GetPageSize())
	interiorOffset := b.pager.PageHeaderOffset(pageNum)

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

	b.pager.SetPageData(pageNum, interiorData)

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
	ph := ReadPageHeaderFrom(data[offset:])

	if ph.PageType == pageTypeInteriorTable {
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
	ph := ReadPageHeaderFrom(data[offset:])

	if ph.PageType == pageTypeLeafTable {
		return b.searchLeaf(data, offset, ph, rowid)
	}
	if ph.PageType == pageTypeInteriorTable {
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
	cells := b.readInteriorCells(data, offset, ph)

	childPage := int(ph.RightMostPtr)
	for _, c := range cells {
		if rowid < c.rowid {
			childPage = int(c.leftChild)
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

	sort.Slice(cells, func(i, j int) bool {
		return cells[i].rowid < cells[j].rowid
	})

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

// Scan returns a cursor for iterating all rows in the B-tree.
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
	record  *Record
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
		ph := ReadPageHeaderFrom(data[offset:])

		if ph.PageType == pageTypeLeafTable {
			c.currentPage = pageNum
			c.cells = c.btree.readLeafCells(data, offset, ph)
			c.index = 0
			return nil
		}

		if ph.PageType != pageTypeInteriorTable {
			return fmt.Errorf("unexpected page type %d", ph.PageType)
		}

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
	ph := ReadPageHeaderFrom(data[offset:])

	if ph.PageType == pageTypeLeafTable {
		c.cells = c.btree.readLeafCells(data, offset, ph)
		return nil
	}

	if ph.PageType == pageTypeInteriorTable {
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
		c.index = 0
	}

	// Try current page
	if c.index < len(c.cells) {
		cell := c.cells[c.index]
		c.rowid = cell.rowid
		rec, err := c.btree.parseCellRecord(cell.data)
		if err != nil {
			c.eof = true
			return false
		}
		c.record = rec
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
		rec, err := c.btree.parseCellRecord(cell.data)
		if err != nil {
			c.eof = true
			return false
		}
		c.record = rec
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
	return c.rowid, c.record, nil
}

// Close releases cursor resources.
func (c *BTreeCursor) Close() error {
	c.cells = nil
	c.navStack = nil
	c.eof = true
	return nil
}
