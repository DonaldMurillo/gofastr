package sqlite

import (
	"bytes"
	"encoding/binary"
	"testing"
)

// ============================================================================
// MemFile Tests
// ============================================================================

func TestMemFile_New(t *testing.T) {
	t.Parallel()
	mf := NewMemFile()
	if mf == nil {
		t.Fatal("NewMemFile returned nil")
	}
	if mf.Len() != 0 {
		t.Fatalf("expected empty MemFile, got Len=%d", mf.Len())
	}
	b := mf.Bytes()
	if len(b) != 0 {
		t.Fatalf("expected empty Bytes(), got %d bytes", len(b))
	}
}

func TestMemFile_WriteRead(t *testing.T) {
	t.Parallel()
	mf := NewMemFile()

	data := []byte("hello world")
	n, err := mf.WriteAt(data, 0)
	if err != nil {
		t.Fatalf("WriteAt: %v", err)
	}
	if n != len(data) {
		t.Fatalf("expected %d bytes written, got %d", len(data), n)
	}
	if mf.Len() != int64(len(data)) {
		t.Fatalf("expected Len=%d, got %d", len(data), mf.Len())
	}

	buf := make([]byte, len(data))
	n, err = mf.ReadAt(buf, 0)
	if err != nil {
		t.Fatalf("ReadAt: %v", err)
	}
	if n != len(data) {
		t.Fatalf("expected %d bytes read, got %d", len(data), n)
	}
	if string(buf) != string(data) {
		t.Fatalf("expected %q, got %q", data, buf)
	}
}

func TestMemFile_WriteBeyondEnd(t *testing.T) {
	t.Parallel()
	mf := NewMemFile()

	// Write at offset 10 to a file with 0 bytes — should extend
	data := []byte{0xAA, 0xBB, 0xCC}
	n, err := mf.WriteAt(data, 10)
	if err != nil {
		t.Fatalf("WriteAt beyond end: %v", err)
	}
	if n != len(data) {
		t.Fatalf("expected %d bytes written, got %d", len(data), n)
	}
	if mf.Len() != 13 {
		t.Fatalf("expected Len=13, got %d", mf.Len())
	}

	// Bytes 0-9 should be zero
	buf := make([]byte, 10)
	mf.ReadAt(buf, 0)
	for i, b := range buf {
		if b != 0 {
			t.Fatalf("byte %d expected 0, got %02x", i, b)
		}
	}

	// Bytes 10-12 should match our data
	buf2 := make([]byte, 3)
	mf.ReadAt(buf2, 10)
	if !bytes.Equal(buf2, data) {
		t.Fatalf("expected %v, got %v", data, buf2)
	}
}

func TestMemFile_WriteOverwrite(t *testing.T) {
	t.Parallel()
	mf := NewMemFile()

	mf.WriteAt([]byte("AAAAAAAAAA"), 0)
	mf.WriteAt([]byte("BBB"), 3)

	buf := make([]byte, 10)
	mf.ReadAt(buf, 0)
	expected := "AAABBBAAAA"
	if string(buf) != expected {
		t.Fatalf("expected %q, got %q", expected, string(buf))
	}
}

func TestMemFile_WriteMultipleRegions(t *testing.T) {
	t.Parallel()
	mf := NewMemFile()

	mf.WriteAt([]byte("foo"), 0)
	mf.WriteAt([]byte("bar"), 6)

	buf := make([]byte, 9)
	mf.ReadAt(buf, 0)
	expected := "foo\x00\x00\x00bar"
	if string(buf) != expected {
		t.Fatalf("expected %q, got %q", expected, string(buf))
	}
}

func TestMemFile_ReadAtZeroLength(t *testing.T) {
	t.Parallel()
	mf := NewMemFile()

	buf := make([]byte, 0)
	n, err := mf.ReadAt(buf, 0)
	if err != nil {
		t.Fatalf("ReadAt zero-length: %v", err)
	}
	if n != 0 {
		t.Fatalf("expected 0 bytes read, got %d", n)
	}
}

func TestMemFile_ReadAtOffsetBeyondEnd(t *testing.T) {
	t.Parallel()
	mf := NewMemFile()

	buf := make([]byte, 4)
	_, err := mf.ReadAt(buf, 100)
	if err == nil {
		t.Fatal("expected error for read beyond end")
	}
}

func TestMemFile_ReadAtShortRead(t *testing.T) {
	t.Parallel()
	mf := NewMemFile()
	mf.WriteAt([]byte("hi"), 0)

	buf := make([]byte, 10)
	n, err := mf.ReadAt(buf, 0)
	if err == nil {
		t.Fatal("expected short read error")
	}
	if n != 2 {
		t.Fatalf("expected 2 bytes read, got %d", n)
	}
}

func TestMemFile_ReadAtWithinFile(t *testing.T) {
	t.Parallel()
	mf := NewMemFile()
	mf.WriteAt([]byte("hello world"), 0)

	buf := make([]byte, 5)
	n, err := mf.ReadAt(buf, 6)
	if err != nil {
		t.Fatalf("ReadAt: %v", err)
	}
	if n != 5 {
		t.Fatalf("expected 5, got %d", n)
	}
	if string(buf) != "world" {
		t.Fatalf("expected 'world', got %q", string(buf))
	}
}

func TestMemFile_TruncateShrink(t *testing.T) {
	t.Parallel()
	mf := NewMemFile()
	mf.WriteAt([]byte("hello world"), 0)

	err := mf.Truncate(5)
	if err != nil {
		t.Fatalf("Truncate: %v", err)
	}
	if mf.Len() != 5 {
		t.Fatalf("expected Len=5, got %d", mf.Len())
	}
	b := mf.Bytes()
	if string(b) != "hello" {
		t.Fatalf("expected 'hello', got %q", string(b))
	}
}

func TestMemFile_TruncateGrow(t *testing.T) {
	t.Parallel()
	mf := NewMemFile()
	mf.WriteAt([]byte("hi"), 0)

	err := mf.Truncate(10)
	if err != nil {
		t.Fatalf("Truncate grow: %v", err)
	}
	if mf.Len() != 10 {
		t.Fatalf("expected Len=10, got %d", mf.Len())
	}

	// Original data preserved
	buf := make([]byte, 2)
	mf.ReadAt(buf, 0)
	if string(buf) != "hi" {
		t.Fatalf("expected 'hi', got %q", string(buf))
	}

	// New area zeroed
	buf2 := make([]byte, 8)
	mf.ReadAt(buf2, 2)
	for i, b := range buf2 {
		if b != 0 {
			t.Fatalf("byte %d expected 0, got %02x", i, b)
		}
	}
}

func TestMemFile_TruncateSameSize(t *testing.T) {
	t.Parallel()
	mf := NewMemFile()
	mf.WriteAt([]byte("test"), 0)

	err := mf.Truncate(4)
	if err != nil {
		t.Fatalf("Truncate same: %v", err)
	}
	if mf.Len() != 4 {
		t.Fatalf("expected Len=4, got %d", mf.Len())
	}
	b := mf.Bytes()
	if string(b) != "test" {
		t.Fatalf("expected 'test', got %q", string(b))
	}
}

func TestMemFile_TruncateToZero(t *testing.T) {
	t.Parallel()
	mf := NewMemFile()
	mf.WriteAt([]byte("data"), 0)

	err := mf.Truncate(0)
	if err != nil {
		t.Fatalf("Truncate to zero: %v", err)
	}
	if mf.Len() != 0 {
		t.Fatalf("expected Len=0, got %d", mf.Len())
	}
}

func TestMemFile_TruncateNegative(t *testing.T) {
	t.Parallel()
	mf := NewMemFile()
	err := mf.Truncate(-1)
	if err == nil {
		t.Fatal("expected error for negative truncate")
	}
}

func TestMemFile_BytesCopyIsolation(t *testing.T) {
	t.Parallel()
	mf := NewMemFile()
	mf.WriteAt([]byte("original"), 0)

	b1 := mf.Bytes()
	b1[0] = 'X' // Modify the copy

	b2 := mf.Bytes()
	if string(b2) != "original" {
		t.Fatalf("Bytes() returned same underlying slice — mutation leaked: %q", string(b2))
	}

	// Also verify modifying the returned slice doesn't affect the file
	buf := make([]byte, 8)
	mf.ReadAt(buf, 0)
	if string(buf) != "original" {
		t.Fatalf("file data was corrupted: %q", string(buf))
	}
}

func TestMemFile_BytesEmpty(t *testing.T) {
	t.Parallel()
	mf := NewMemFile()
	b := mf.Bytes()
	if b == nil {
		t.Fatal("Bytes() returned nil for empty file")
	}
	if len(b) != 0 {
		t.Fatalf("expected 0 bytes, got %d", len(b))
	}
}

func TestMemFile_LargeWrite(t *testing.T) {
	t.Parallel()
	mf := NewMemFile()
	data := make([]byte, 100000)
	for i := range data {
		data[i] = byte(i % 256)
	}

	n, err := mf.WriteAt(data, 0)
	if err != nil {
		t.Fatalf("large write: %v", err)
	}
	if n != len(data) {
		t.Fatalf("expected %d, got %d", len(data), n)
	}
	if mf.Len() != int64(len(data)) {
		t.Fatalf("expected Len=%d, got %d", len(data), mf.Len())
	}

	buf := make([]byte, len(data))
	mf.ReadAt(buf, 0)
	if !bytes.Equal(buf, data) {
		t.Fatal("large data mismatch after read")
	}
}

// ============================================================================
// Pager Tests
// ============================================================================

func TestPager_NewWithValidPageSizes(t *testing.T) {
	t.Parallel()
	validSizes := []int{512, 1024, 2048, 4096, 8192, 16384, 32768, 65536}
	for _, ps := range validSizes {
		t.Run("", func(t *testing.T) {
			t.Parallel()
			mf := NewMemFile()
			p, err := NewPager(mf, ps)
			if err != nil {
				t.Fatalf("pageSize=%d: %v", ps, err)
			}
			if p.GetPageSize() != ps {
				t.Fatalf("expected %d, got %d", ps, p.GetPageSize())
			}
		})
	}
}

func TestPager_NewWithInvalidPageSizes(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		pageSize int
	}{
		{"zero", 0},
		{"negative", -1},
		{"too small", 256},
		{"too large", 131072},
		{"not power of 2 - 3", 3},
		{"not power of 2 - 1000", 1000},
		{"not power of 2 - 5000", 5000},
		{"not power of 2 - 10000", 10000},
		{"one", 1},
		{"511", 511},
		{"513", 513},
		{"1023", 1023},
		{"1025", 1025},
		{"65535", 65535},
		{"65537", 65537},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			mf := NewMemFile()
			_, err := NewPager(mf, tt.pageSize)
			if err == nil {
				t.Fatalf("expected error for pageSize=%d", tt.pageSize)
			}
		})
	}
}

func TestPager_NewWithTooSmallFile(t *testing.T) {
	t.Parallel()
	mf := NewMemFile()
	mf.WriteAt([]byte("short"), 0) // 5 bytes, less than headerSize (100)
	_, err := NewPager(mf, 4096)
	if err == nil {
		t.Fatal("expected error for file smaller than header")
	}
}

func TestPager_NewWithValidFile(t *testing.T) {
	t.Parallel()
	mf := NewMemFile()
	// Create a file big enough for a header
	data := make([]byte, 4096)
	copy(data, "SQLite format 3\x00")
	mf.WriteAt(data, 0)

	p, err := NewPager(mf, 4096)
	if err != nil {
		t.Fatalf("NewPager: %v", err)
	}
	if p.PageCount() != 1 {
		t.Fatalf("expected 1 page, got %d", p.PageCount())
	}
}

func TestPager_NewWithLargeFile(t *testing.T) {
	t.Parallel()
	mf := NewMemFile()
	data := make([]byte, 4096)
	copy(data, "SQLite format 3\x00")
	mf.WriteAt(data, 0)
	// Write a second page of data
	mf.WriteAt(make([]byte, 4096), 4096)

	p, err := NewPager(mf, 4096)
	if err != nil {
		t.Fatalf("NewPager: %v", err)
	}
	if p.PageCount() != 2 {
		t.Fatalf("expected 2 pages, got %d", p.PageCount())
	}
}

func TestPager_EmptyFile(t *testing.T) {
	t.Parallel()
	mf := NewMemFile()
	p, err := NewPager(mf, 4096)
	if err != nil {
		t.Fatalf("NewPager: %v", err)
	}
	if p.PageCount() != 0 {
		t.Fatalf("expected 0 pages for empty file, got %d", p.PageCount())
	}
}

func TestPager_AllocatePage(t *testing.T) {
	t.Parallel()
	mf := NewMemFile()
	p, _ := NewPager(mf, 4096)

	page1, err := p.AllocatePage()
	if err != nil {
		t.Fatalf("AllocatePage: %v", err)
	}
	if page1 != 1 {
		t.Fatalf("first allocated page should be 1, got %d", page1)
	}
	if p.PageCount() != 1 {
		t.Fatalf("expected PageCount=1, got %d", p.PageCount())
	}

	page2, err := p.AllocatePage()
	if err != nil {
		t.Fatalf("AllocatePage: %v", err)
	}
	if page2 != 2 {
		t.Fatalf("second allocated page should be 2, got %d", page2)
	}
	if p.PageCount() != 2 {
		t.Fatalf("expected PageCount=2, got %d", p.PageCount())
	}
}

func TestPager_AllocateManyPages(t *testing.T) {
	t.Parallel()
	mf := NewMemFile()
	p, _ := NewPager(mf, 4096)

	const N = 50
	for i := 1; i <= N; i++ {
		page, err := p.AllocatePage()
		if err != nil {
			t.Fatalf("AllocatePage %d: %v", i, err)
		}
		if page != i {
			t.Fatalf("page %d: expected %d, got %d", i, i, page)
		}
	}
	if p.PageCount() != N {
		t.Fatalf("expected %d pages, got %d", N, p.PageCount())
	}
}

func TestPager_GetPageDataOutOfRange(t *testing.T) {
	t.Parallel()
	mf := NewMemFile()
	p, _ := NewPager(mf, 4096)

	tests := []struct {
		name string
		num  int
	}{
		{"zero", 0},
		{"negative", -1},
		{"negative large", -100},
		{"one beyond", 1}, // no pages allocated
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := p.GetPageData(tt.num)
			if err == nil {
				t.Fatalf("expected error for page %d", tt.num)
			}
		})
	}
}

func TestPager_SetPageDataOutOfRange(t *testing.T) {
	t.Parallel()
	mf := NewMemFile()
	p, _ := NewPager(mf, 4096)

	tests := []struct {
		name string
		num  int
	}{
		{"zero", 0},
		{"negative", -1},
		{"beyond", 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := p.SetPageData(tt.num, make([]byte, 4096))
			if err == nil {
				t.Fatalf("expected error for page %d", tt.num)
			}
		})
	}
}

func TestPager_SetPageDataWrongSize(t *testing.T) {
	t.Parallel()
	mf := NewMemFile()
	p, _ := NewPager(mf, 4096)
	p.AllocatePage()

	err := p.SetPageData(1, make([]byte, 100))
	if err == nil {
		t.Fatal("expected error for wrong data size")
	}
}

func TestPager_GetSetPageDataRoundTrip(t *testing.T) {
	t.Parallel()
	mf := NewMemFile()
	p, _ := NewPager(mf, 4096)
	p.AllocatePage()

	data := make([]byte, 4096)
	for i := range data {
		data[i] = byte(i % 256)
	}

	err := p.SetPageData(1, data)
	if err != nil {
		t.Fatalf("SetPageData: %v", err)
	}

	got, err := p.GetPageData(1)
	if err != nil {
		t.Fatalf("GetPageData: %v", err)
	}
	if !bytes.Equal(got, data) {
		t.Fatal("page data mismatch")
	}
}

func TestPager_PageDataIsolation(t *testing.T) {
	t.Parallel()
	mf := NewMemFile()
	p, _ := NewPager(mf, 4096)
	p.AllocatePage()

	original := make([]byte, 4096)
	for i := range original {
		original[i] = byte(i)
	}
	p.SetPageData(1, original)

	// Get the data and modify the returned copy
	got, _ := p.GetPageData(1)
	got[0] = 0xFF

	// Get again — should still have original data
	got2, _ := p.GetPageData(1)
	if got2[0] != original[0] {
		t.Fatalf("page data was corrupted via returned slice: expected %02x, got %02x", original[0], got2[0])
	}

	// Also verify the first returned slice was a copy
	if got[0] != 0xFF {
		t.Fatal("unexpectedly modified the wrong slice")
	}
}

func TestPager_MultiplePagesRoundTrip(t *testing.T) {
	t.Parallel()
	mf := NewMemFile()
	p, _ := NewPager(mf, 4096)

	const N = 5
	pages := make([][]byte, N)
	for i := 0; i < N; i++ {
		p.AllocatePage()
		pages[i] = make([]byte, 4096)
		for j := range pages[i] {
			pages[i][j] = byte((i*256 + j) % 256)
		}
		p.SetPageData(i+1, pages[i])
	}

	for i := 0; i < N; i++ {
		got, err := p.GetPageData(i + 1)
		if err != nil {
			t.Fatalf("GetPageData(%d): %v", i+1, err)
		}
		if !bytes.Equal(got, pages[i]) {
			t.Fatalf("page %d data mismatch", i+1)
		}
	}
}

func TestPager_FlushPersistsToMemFile(t *testing.T) {
	t.Parallel()
	mf := NewMemFile()
	p, _ := NewPager(mf, 4096)
	p.AllocatePage()

	data := make([]byte, 4096)
	copy(data, "this is page 1 data that should be flushed")
	p.SetPageData(1, data)

	err := p.Flush()
	if err != nil {
		t.Fatalf("Flush: %v", err)
	}

	// Verify data is in the MemFile
	if mf.Len() < int64(4096) {
		t.Fatalf("MemFile too small: %d", mf.Len())
	}

	buf := make([]byte, 4096)
	mf.ReadAt(buf, 0)
	if !bytes.Equal(buf, data) {
		t.Fatal("data in MemFile doesn't match page data")
	}
}

func TestPager_FlushMultiplePages(t *testing.T) {
	t.Parallel()
	mf := NewMemFile()
	p, _ := NewPager(mf, 4096)

	p.AllocatePage() // page 1
	p.AllocatePage() // page 2

	data1 := make([]byte, 4096)
	data2 := make([]byte, 4096)
	copy(data1, "page one")
	copy(data2, "page two")
	p.SetPageData(1, data1)
	p.SetPageData(2, data2)

	p.Flush()

	buf1 := make([]byte, 4096)
	buf2 := make([]byte, 4096)
	mf.ReadAt(buf1, 0)
	mf.ReadAt(buf2, 4096)
	if !bytes.Equal(buf1, data1) {
		t.Fatal("page 1 data mismatch in file")
	}
	if !bytes.Equal(buf2, data2) {
		t.Fatal("page 2 data mismatch in file")
	}
}

func TestPager_FlushClearsDirty(t *testing.T) {
	t.Parallel()
	mf := NewMemFile()
	p, _ := NewPager(mf, 4096)
	p.AllocatePage()

	data := make([]byte, 4096)
	copy(data, "dirty data")
	p.SetPageData(1, data)
	p.Flush()

	// Write to MemFile directly, then Flush should not overwrite
	// because dirty should be cleared
	mf.WriteAt([]byte("external write"), 0)

	// Flush again — should not overwrite our external write since dirty is clear
	p.Flush()

	buf := make([]byte, 14)
	mf.ReadAt(buf, 0)
	if string(buf) != "external write" {
		t.Fatalf("Flush overwrote clean page: %q", string(buf))
	}
}

func TestPager_CloseFlushesAndClears(t *testing.T) {
	t.Parallel()
	mf := NewMemFile()
	p, _ := NewPager(mf, 4096)
	p.AllocatePage()

	data := make([]byte, 4096)
	copy(data, "close test")
	p.SetPageData(1, data)

	err := p.Close()
	if err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Verify data was flushed
	buf := make([]byte, 10)
	mf.ReadAt(buf, 0)
	if string(buf) != "close test" {
		t.Fatalf("data not flushed on close: %q", string(buf))
	}
}

func TestPager_InitNew(t *testing.T) {
	t.Parallel()
	mf := NewMemFile()
	p, _ := NewPager(mf, 4096)

	err := p.InitNew()
	if err != nil {
		t.Fatalf("InitNew: %v", err)
	}

	if p.PageCount() < 1 {
		t.Fatal("expected at least 1 page after InitNew")
	}

	// Verify the MemFile has the magic header
	if mf.Len() < 16 {
		t.Fatalf("file too small: %d", mf.Len())
	}

	magicBuf := make([]byte, 16)
	mf.ReadAt(magicBuf, 0)
	if string(magicBuf) != magicHeader {
		t.Fatalf("bad magic: %q", string(magicBuf))
	}
}

func TestPager_InitNewPageSize(t *testing.T) {
	t.Parallel()
	sizes := []int{512, 1024, 2048, 4096, 8192, 16384, 32768, 65536}
	for _, ps := range sizes {
		t.Run("", func(t *testing.T) {
			t.Parallel()
			mf := NewMemFile()
			p, _ := NewPager(mf, ps)
			p.InitNew()

			if mf.Len() != int64(ps) {
				t.Fatalf("pageSize=%d: expected file len %d, got %d", ps, ps, mf.Len())
			}
		})
	}
}

func TestPager_GetHeader(t *testing.T) {
	t.Parallel()
	mf := NewMemFile()
	p, _ := NewPager(mf, 4096)
	p.InitNew()

	h := p.GetHeader()
	if h == nil {
		t.Fatal("GetHeader returned nil")
	}

	if string(h.Magic[:]) != magicHeader {
		t.Fatalf("bad magic: %q", string(h.Magic[:]))
	}
	if h.PageSize != 4096 {
		t.Fatalf("expected PageSize=4096, got %d", h.PageSize)
	}
	if h.FileChangeCounter == 0 {
		t.Fatal("FileChangeCounter should be non-zero")
	}
	if h.TextEncoding != 1 {
		t.Fatalf("expected UTF-8 encoding (1), got %d", h.TextEncoding)
	}
}

func TestPager_GetHeaderNilBeforeInit(t *testing.T) {
	t.Parallel()
	mf := NewMemFile()
	p, _ := NewPager(mf, 4096)
	// No InitNew, no pages allocated
	h := p.GetHeader()
	if h != nil {
		t.Fatal("expected nil header before InitNew")
	}
}

func TestPager_UpdateHeaderRoundTrip(t *testing.T) {
	t.Parallel()
	mf := NewMemFile()
	p, _ := NewPager(mf, 4096)
	p.InitNew()

	h := p.GetHeader()
	originalCounter := h.FileChangeCounter

	h.FileChangeCounter = originalCounter + 42
	h.DatabaseSizePages = 99
	h.TextEncoding = 2 // UTF-16le

	err := p.UpdateHeader(h)
	if err != nil {
		t.Fatalf("UpdateHeader: %v", err)
	}

	// Flush to persist
	p.Flush()

	// Create a new pager from the same file
	p2, err := NewPager(mf, 4096)
	if err != nil {
		t.Fatalf("NewPager from existing file: %v", err)
	}

	h2 := p2.GetHeader()
	if h2 == nil {
		t.Fatal("second pager GetHeader returned nil")
	}
	if h2.FileChangeCounter != originalCounter+42 {
		t.Fatalf("expected FileChangeCounter=%d, got %d", originalCounter+42, h2.FileChangeCounter)
	}
	if h2.DatabaseSizePages != 99 {
		t.Fatalf("expected DatabaseSizePages=99, got %d", h2.DatabaseSizePages)
	}
	if h2.TextEncoding != 2 {
		t.Fatalf("expected TextEncoding=2, got %d", h2.TextEncoding)
	}
}

func TestPager_UpdateHeaderFieldsRoundTrip(t *testing.T) {
	t.Parallel()
	mf := NewMemFile()
	p, _ := NewPager(mf, 4096)
	p.InitNew()

	tests := []struct {
		name    string
		modify  func(h *DatabaseHeader)
		extract func(h *DatabaseHeader) interface{}
	}{
		{"FileChangeCounter", func(h *DatabaseHeader) { h.FileChangeCounter = 12345 }, func(h *DatabaseHeader) interface{} { return h.FileChangeCounter }},
		{"DatabaseSizePages", func(h *DatabaseHeader) { h.DatabaseSizePages = 77 }, func(h *DatabaseHeader) interface{} { return h.DatabaseSizePages }},
		{"TextEncoding_UTF16le", func(h *DatabaseHeader) { h.TextEncoding = 2 }, func(h *DatabaseHeader) interface{} { return h.TextEncoding }},
		{"TextEncoding_UTF16be", func(h *DatabaseHeader) { h.TextEncoding = 3 }, func(h *DatabaseHeader) interface{} { return h.TextEncoding }},
		{"SchemaCookie", func(h *DatabaseHeader) { h.SchemaCookie = 42 }, func(h *DatabaseHeader) interface{} { return h.SchemaCookie }},
		{"UserVersion", func(h *DatabaseHeader) { h.UserVersion = 999 }, func(h *DatabaseHeader) interface{} { return h.UserVersion }},
		{"ApplicationID", func(h *DatabaseHeader) { h.ApplicationID = 0xDEADBEEF }, func(h *DatabaseHeader) interface{} { return h.ApplicationID }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Fresh pager for each subtest
			mf := NewMemFile()
			p, _ := NewPager(mf, 4096)
			p.InitNew()

			h := p.GetHeader()
			tt.modify(h)
			p.UpdateHeader(h)
			p.Flush()

			p2, _ := NewPager(mf, 4096)
			h2 := p2.GetHeader()
			if tt.extract(h2) != tt.extract(h) {
				t.Fatalf("round-trip failed: expected %v, got %v", tt.extract(h), tt.extract(h2))
			}
		})
	}
}

func TestPager_PageHeaderOffset(t *testing.T) {
	t.Parallel()
	mf := NewMemFile()
	p, _ := NewPager(mf, 4096)

	tests := []struct {
		pageNum int
		want    int
	}{
		{1, headerSize}, // 100
		{2, 0},
		{3, 0},
		{10, 0},
		{100, 0},
		{0, 0},
		{-1, 0},
	}
	for _, tt := range tests {
		t.Run("", func(t *testing.T) {
			t.Parallel()
			got := p.PageHeaderOffset(tt.pageNum)
			if got != tt.want {
				t.Fatalf("PageHeaderOffset(%d) = %d, want %d", tt.pageNum, got, tt.want)
			}
		})
	}
}

func TestPager_ReadPageHeaderFrom(t *testing.T) {
	t.Parallel()

	// Build a leaf table page header
	data := make([]byte, 12)
	data[0] = pageTypeLeafTable                 // 0x0d
	binary.BigEndian.PutUint16(data[1:3], 0)    // FirstFreeblock
	binary.BigEndian.PutUint16(data[3:5], 3)    // CellCount
	binary.BigEndian.PutUint16(data[5:7], 4000) // ContentOffset
	data[7] = 0                                 // FragmentedBytes

	ph := ReadPageHeaderFrom(data)
	if ph.PageType != pageTypeLeafTable {
		t.Fatalf("expected pageType 0x%02x, got 0x%02x", pageTypeLeafTable, ph.PageType)
	}
	if ph.CellCount != 3 {
		t.Fatalf("expected CellCount=3, got %d", ph.CellCount)
	}
	if ph.ContentOffset != 4000 {
		t.Fatalf("expected ContentOffset=4000, got %d", ph.ContentOffset)
	}
	if ph.FirstFreeblock != 0 {
		t.Fatalf("expected FirstFreeblock=0, got %d", ph.FirstFreeblock)
	}
	if ph.FragmentedBytes != 0 {
		t.Fatalf("expected FragmentedBytes=0, got %d", ph.FragmentedBytes)
	}
}

func TestPager_ReadPageHeaderFromInteriorPage(t *testing.T) {
	t.Parallel()

	data := make([]byte, 12)
	data[0] = pageTypeInteriorTable             // 0x05
	binary.BigEndian.PutUint16(data[1:3], 10)   // FirstFreeblock
	binary.BigEndian.PutUint16(data[3:5], 5)    // CellCount
	binary.BigEndian.PutUint16(data[5:7], 2000) // ContentOffset
	data[7] = 2                                 // FragmentedBytes
	binary.BigEndian.PutUint32(data[8:12], 42)  // RightMostPtr

	ph := ReadPageHeaderFrom(data)
	if ph.PageType != pageTypeInteriorTable {
		t.Fatalf("expected 0x%02x, got 0x%02x", pageTypeInteriorTable, ph.PageType)
	}
	if ph.CellCount != 5 {
		t.Fatalf("expected 5, got %d", ph.CellCount)
	}
	if ph.RightMostPtr != 42 {
		t.Fatalf("expected RightMostPtr=42, got %d", ph.RightMostPtr)
	}
	if ph.FirstFreeblock != 10 {
		t.Fatalf("expected FirstFreeblock=10, got %d", ph.FirstFreeblock)
	}
	if ph.FragmentedBytes != 2 {
		t.Fatalf("expected FragmentedBytes=2, got %d", ph.FragmentedBytes)
	}
}

func TestPager_ReadPageHeaderFromTooShort(t *testing.T) {
	t.Parallel()

	// Less than 8 bytes
	ph := ReadPageHeaderFrom([]byte{0x0d, 0x00, 0x00})
	if ph.PageType != 0 {
		t.Fatalf("expected zero PageType for short data, got %d", ph.PageType)
	}
}

func TestPager_WriteReadPageHeaderRoundTrip(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		pageType byte
	}{
		{"leaf table", pageTypeLeafTable},
		{"leaf index", pageTypeLeafIndex},
		{"interior table", pageTypeInteriorTable},
		{"interior index", pageTypeInteriorIndex},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			buf := make([]byte, 12)
			h := PageHeader{
				PageType:        tt.pageType,
				FirstFreeblock:  100,
				CellCount:       7,
				ContentOffset:   3000,
				FragmentedBytes: 3,
			}
			if tt.pageType == pageTypeInteriorTable || tt.pageType == pageTypeInteriorIndex {
				h.RightMostPtr = 12345
			}
			WritePageHeaderTo(buf, h)

			got := ReadPageHeaderFrom(buf)
			if got.PageType != h.PageType {
				t.Fatalf("PageType: expected %d, got %d", h.PageType, got.PageType)
			}
			if got.FirstFreeblock != h.FirstFreeblock {
				t.Fatalf("FirstFreeblock: expected %d, got %d", h.FirstFreeblock, got.FirstFreeblock)
			}
			if got.CellCount != h.CellCount {
				t.Fatalf("CellCount: expected %d, got %d", h.CellCount, got.CellCount)
			}
			if got.ContentOffset != h.ContentOffset {
				t.Fatalf("ContentOffset: expected %d, got %d", h.ContentOffset, got.ContentOffset)
			}
			if got.FragmentedBytes != h.FragmentedBytes {
				t.Fatalf("FragmentedBytes: expected %d, got %d", h.FragmentedBytes, got.FragmentedBytes)
			}
			if tt.pageType == pageTypeInteriorTable || tt.pageType == pageTypeInteriorIndex {
				if got.RightMostPtr != h.RightMostPtr {
					t.Fatalf("RightMostPtr: expected %d, got %d", h.RightMostPtr, got.RightMostPtr)
				}
			}
		})
	}
}

func TestPager_CacheThenReadFromFile(t *testing.T) {
	t.Parallel()
	mf := NewMemFile()
	p, _ := NewPager(mf, 4096)
	p.AllocatePage()

	data := make([]byte, 4096)
	copy(data, "cached data")
	p.SetPageData(1, data)
	p.Flush()

	// Create a new pager — should read from file since cache is empty
	p2, _ := NewPager(mf, 4096)
	got, err := p2.GetPageData(1)
	if err != nil {
		t.Fatalf("GetPageData from new pager: %v", err)
	}
	if !bytes.Equal(got, data) {
		t.Fatal("data read from file doesn't match")
	}
}

func TestPager_InitNewThenGetHeader(t *testing.T) {
	t.Parallel()
	mf := NewMemFile()
	p, _ := NewPager(mf, 4096)
	p.InitNew()

	h := p.GetHeader()
	if h == nil {
		t.Fatal("nil header")
	}
	if h.PageSize != 4096 {
		t.Fatalf("expected PageSize=4096, got %d", h.PageSize)
	}
	if h.FileFormatWriteVersion != 1 {
		t.Fatalf("expected FileFormatWriteVersion=1, got %d", h.FileFormatWriteVersion)
	}
	if h.FileFormatReadVersion != 1 {
		t.Fatalf("expected FileFormatReadVersion=1, got %d", h.FileFormatReadVersion)
	}
	if h.SchemaFormatNumber != 4 {
		t.Fatalf("expected SchemaFormatNumber=4, got %d", h.SchemaFormatNumber)
	}
}

func TestPager_PageCountAfterOperations(t *testing.T) {
	t.Parallel()
	mf := NewMemFile()
	p, _ := NewPager(mf, 4096)

	if p.PageCount() != 0 {
		t.Fatalf("expected 0, got %d", p.PageCount())
	}

	p.AllocatePage()
	if p.PageCount() != 1 {
		t.Fatalf("expected 1, got %d", p.PageCount())
	}

	p.AllocatePage()
	p.AllocatePage()
	if p.PageCount() != 3 {
		t.Fatalf("expected 3, got %d", p.PageCount())
	}
}

func TestPager_FlushExtendsMemFile(t *testing.T) {
	t.Parallel()
	mf := NewMemFile()
	p, _ := NewPager(mf, 4096)

	p.AllocatePage()
	p.AllocatePage()
	p.SetPageData(1, make([]byte, 4096))
	p.SetPageData(2, make([]byte, 4096))
	p.Flush()

	if mf.Len() != 2*4096 {
		t.Fatalf("expected MemFile len=%d, got %d", 2*4096, mf.Len())
	}
}

func TestPager_PageDataOnFileBackedPager(t *testing.T) {
	t.Parallel()
	mf := NewMemFile()
	p1, _ := NewPager(mf, 4096)
	p1.AllocatePage()

	data := make([]byte, 4096)
	copy(data[100:], "test from p1") // offset 100 = page header area
	p1.SetPageData(1, data)
	p1.Flush()

	// New pager reads from file
	p2, _ := NewPager(mf, 4096)
	got, err := p2.GetPageData(1)
	if err != nil {
		t.Fatalf("GetPageData: %v", err)
	}
	if string(got[100:112]) != "test from p1" {
		t.Fatalf("expected 'test from p1', got %q", string(got[100:112]))
	}
}

func TestPager_SetPageDataDoesNotMutateOriginal(t *testing.T) {
	t.Parallel()
	mf := NewMemFile()
	p, _ := NewPager(mf, 4096)
	p.AllocatePage()

	data := make([]byte, 4096)
	copy(data, "original")
	p.SetPageData(1, data)

	// Modify the original slice
	data[0] = 'X'

	got, _ := p.GetPageData(1)
	if got[0] != 'o' {
		t.Fatalf("SetPageData didn't copy input: got %c", got[0])
	}
}

func TestPager_FlushAfterAllocateOnly(t *testing.T) {
	t.Parallel()
	mf := NewMemFile()
	p, _ := NewPager(mf, 4096)
	p.AllocatePage()
	p.Flush()

	// AllocatePage creates a dirty page of zeros
	if mf.Len() < 4096 {
		t.Fatalf("expected at least 4096 bytes, got %d", mf.Len())
	}

	buf := make([]byte, 4096)
	mf.ReadAt(buf, 0)
	for i, b := range buf {
		if b != 0 {
			t.Fatalf("expected all zeros at byte %d, got %02x", i, b)
		}
	}
}

func TestPager_MultipleFlushes(t *testing.T) {
	t.Parallel()
	mf := NewMemFile()
	p, _ := NewPager(mf, 4096)
	p.AllocatePage()

	// First write
	d1 := make([]byte, 4096)
	copy(d1, "first")
	p.SetPageData(1, d1)
	p.Flush()

	// Second write to same page
	d2 := make([]byte, 4096)
	copy(d2, "second")
	p.SetPageData(1, d2)
	p.Flush()

	buf := make([]byte, 6)
	mf.ReadAt(buf, 0)
	if string(buf) != "second" {
		t.Fatalf("expected 'second', got %q", string(buf))
	}
}

func TestPager_WritePageHeaderTo(t *testing.T) {
	t.Parallel()

	buf := make([]byte, 12)
	h := PageHeader{
		PageType:        pageTypeLeafTable,
		FirstFreeblock:  0x0102,
		CellCount:       0x0304,
		ContentOffset:   0x0506,
		FragmentedBytes: 0x07,
	}
	WritePageHeaderTo(buf, h)

	if buf[0] != pageTypeLeafTable {
		t.Fatalf("PageType byte wrong: %02x", buf[0])
	}
	if binary.BigEndian.Uint16(buf[1:3]) != 0x0102 {
		t.Fatalf("FirstFreeblock wrong: %04x", binary.BigEndian.Uint16(buf[1:3]))
	}
	if binary.BigEndian.Uint16(buf[3:5]) != 0x0304 {
		t.Fatalf("CellCount wrong: %04x", binary.BigEndian.Uint16(buf[3:5]))
	}
	if binary.BigEndian.Uint16(buf[5:7]) != 0x0506 {
		t.Fatalf("ContentOffset wrong: %04x", binary.BigEndian.Uint16(buf[5:7]))
	}
	if buf[7] != 0x07 {
		t.Fatalf("FragmentedBytes wrong: %02x", buf[7])
	}
}

func TestPager_WritePageHeaderToInterior(t *testing.T) {
	t.Parallel()

	buf := make([]byte, 12)
	h := PageHeader{
		PageType:     pageTypeInteriorTable,
		CellCount:    10,
		RightMostPtr: 0xAABBCCDD,
	}
	WritePageHeaderTo(buf, h)

	if buf[0] != pageTypeInteriorTable {
		t.Fatalf("PageType wrong: %02x", buf[0])
	}
	if binary.BigEndian.Uint32(buf[8:12]) != 0xAABBCCDD {
		t.Fatalf("RightMostPtr wrong: %08x", binary.BigEndian.Uint32(buf[8:12]))
	}
}

func TestPager_InitNewHeaderBytesCorrect(t *testing.T) {
	t.Parallel()
	mf := NewMemFile()
	p, _ := NewPager(mf, 4096)
	p.InitNew()

	// The first 16 bytes should be the magic string
	buf := make([]byte, 16)
	mf.ReadAt(buf, 0)
	if string(buf) != magicHeader {
		t.Fatalf("magic mismatch: %q != %q", string(buf), magicHeader)
	}

	// Page size at offset 16-17 (big-endian)
	psBuf := make([]byte, 2)
	mf.ReadAt(psBuf, 16)
	ps := binary.BigEndian.Uint16(psBuf)
	if int(ps) != 4096 {
		t.Fatalf("page size in header: expected 4096, got %d", ps)
	}
}

func TestPager_FlushWithNoDirtyPages(t *testing.T) {
	t.Parallel()
	mf := NewMemFile()
	p, _ := NewPager(mf, 4096)
	p.InitNew() // This flushes

	// Flush again with no new dirty pages
	err := p.Flush()
	if err != nil {
		t.Fatalf("Flush with no dirty pages: %v", err)
	}
}

func TestPager_CloseThenFlush(t *testing.T) {
	t.Parallel()
	mf := NewMemFile()
	p, _ := NewPager(mf, 4096)
	p.InitNew()

	// Close first
	err := p.Close()
	if err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Pages map is nil now, but Flush should handle it gracefully
	err = p.Flush()
	if err != nil {
		t.Fatalf("Flush after Close: %v", err)
	}
}

func TestMemFile_WriteAtLargeOffset(t *testing.T) {
	t.Parallel()
	mf := NewMemFile()

	data := []byte{0x42}
	n, err := mf.WriteAt(data, 100000)
	if err != nil {
		t.Fatalf("WriteAt large offset: %v", err)
	}
	if n != 1 {
		t.Fatalf("expected 1 byte written, got %d", n)
	}
	if mf.Len() != 100001 {
		t.Fatalf("expected Len=100001, got %d", mf.Len())
	}

	buf := make([]byte, 1)
	mf.ReadAt(buf, 100000)
	if buf[0] != 0x42 {
		t.Fatalf("expected 0x42, got 0x%02x", buf[0])
	}
}

func TestMemFile_TruncateGrowPreservesExisting(t *testing.T) {
	t.Parallel()
	mf := NewMemFile()
	mf.WriteAt([]byte("ABC"), 0)
	mf.Truncate(100)

	buf := make([]byte, 3)
	mf.ReadAt(buf, 0)
	if string(buf) != "ABC" {
		t.Fatalf("existing data lost: %q", string(buf))
	}
}
