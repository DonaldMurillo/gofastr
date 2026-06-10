package sqlite

import (
	"encoding/binary"
	"math"
	"testing"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func makeDefaultHeader() *DatabaseHeader {
	h := &DatabaseHeader{}
	copy(h.Magic[:], magicHeader)
	h.PageSize = 4096
	h.FileFormatWriteVersion = 1
	h.FileFormatReadVersion = 1
	h.ReservedSpace = 0
	h.MaxEmbeddedPayloadFrac = 64
	h.MinEmbeddedPayloadFrac = 32
	h.LeafPayloadFrac = 32
	h.FileChangeCounter = 1
	h.DatabaseSizePages = 1
	h.TextEncoding = 1 // UTF-8
	h.SchemaFormatNumber = 4
	h.SQLiteVersionNumber = 3039004
	return h
}

// ---------------------------------------------------------------------------
// Database Header
// ---------------------------------------------------------------------------

func TestDatabaseHeader_RoundTrip(t *testing.T) {
	t.Run("default header", func(t *testing.T) {
		orig := makeDefaultHeader()
		data := WriteHeader(orig)
		if len(data) != 100 {
			t.Fatalf("header size = %d, want 100", len(data))
		}
		parsed, err := ReadHeader(data)
		if err != nil {
			t.Fatalf("ReadHeader: %v", err)
		}
		assertHeadersEqual(t, orig, parsed)
	})

	t.Run("all fields max", func(t *testing.T) {
		h := makeDefaultHeader()
		h.PageSize = 32768
		h.FileFormatWriteVersion = 2
		h.FileFormatReadVersion = 2
		h.ReservedSpace = 255
		h.MaxEmbeddedPayloadFrac = 64
		h.MinEmbeddedPayloadFrac = 32
		h.LeafPayloadFrac = 32
		h.FileChangeCounter = 0xFFFFFFFF
		h.DatabaseSizePages = 0xFFFFFFFF
		h.FirstFreelistTrunkPage = 0xFFFFFFFF
		h.TotalFreelistPages = 0xFFFFFFFF
		h.SchemaCookie = 0xFFFFFFFF
		h.SchemaFormatNumber = 0xFFFFFFFF
		h.DefaultPageCacheSize = 0xFFFFFFFF
		h.LargestRootBTreePage = 0xFFFFFFFF
		h.TextEncoding = 3
		h.UserVersion = 0xFFFFFFFF
		h.IncrementalVacuum = 1
		h.ApplicationID = 0xFFFFFFFF
		h.VersionValidFor = 0xFFFFFFFF
		h.SQLiteVersionNumber = 0xFFFFFFFF

		data := WriteHeader(h)
		parsed, err := ReadHeader(data)
		if err != nil {
			t.Fatalf("ReadHeader: %v", err)
		}
		assertHeadersEqual(t, h, parsed)
	})

	t.Run("all fields zero except magic and required", func(t *testing.T) {
		h := &DatabaseHeader{}
		copy(h.Magic[:], magicHeader)
		h.PageSize = 4096
		h.MaxEmbeddedPayloadFrac = 64
		h.MinEmbeddedPayloadFrac = 32
		h.LeafPayloadFrac = 32

		data := WriteHeader(h)
		parsed, err := ReadHeader(data)
		if err != nil {
			t.Fatalf("ReadHeader: %v", err)
		}
		assertHeadersEqual(t, h, parsed)
	})

	t.Run("page size 65536 stored as zero", func(t *testing.T) {
		h := makeDefaultHeader()
		h.PageSize = 65536
		data := WriteHeader(h)
		// Stored value should be 0
		stored := binary.BigEndian.Uint16(data[16:18])
		if stored != 0 {
			t.Fatalf("stored page size = %d, want 0", stored)
		}
		parsed, err := ReadHeader(data)
		if err != nil {
			t.Fatalf("ReadHeader: %v", err)
		}
		if parsed.PageSize != 65536 {
			t.Fatalf("parsed page size = %d, want 65536", parsed.PageSize)
		}
	})

	t.Run("reserved expansion preserved", func(t *testing.T) {
		h := makeDefaultHeader()
		for i := range h.ReservedExpansion {
			h.ReservedExpansion[i] = byte(i)
		}
		data := WriteHeader(h)
		parsed, err := ReadHeader(data)
		if err != nil {
			t.Fatalf("ReadHeader: %v", err)
		}
		for i := range h.ReservedExpansion {
			if parsed.ReservedExpansion[i] != h.ReservedExpansion[i] {
				t.Fatalf("reserved expansion byte %d = %d, want %d", i, parsed.ReservedExpansion[i], h.ReservedExpansion[i])
			}
		}
	})

	t.Run("every individual field", func(t *testing.T) {
		// Test every field can be non-zero and round-trips
		fields := []struct {
			name    string
			modify  func(h *DatabaseHeader)
			extract func(h *DatabaseHeader) interface{}
		}{
			{"FileFormatWriteVersion=2", func(h *DatabaseHeader) { h.FileFormatWriteVersion = 2 }, func(h *DatabaseHeader) interface{} { return h.FileFormatWriteVersion }},
			{"FileFormatReadVersion=2", func(h *DatabaseHeader) { h.FileFormatReadVersion = 2 }, func(h *DatabaseHeader) interface{} { return h.FileFormatReadVersion }},
			{"ReservedSpace=10", func(h *DatabaseHeader) { h.ReservedSpace = 10 }, func(h *DatabaseHeader) interface{} { return h.ReservedSpace }},
			{"FileChangeCounter=42", func(h *DatabaseHeader) { h.FileChangeCounter = 42 }, func(h *DatabaseHeader) interface{} { return h.FileChangeCounter }},
			{"DatabaseSizePages=100", func(h *DatabaseHeader) { h.DatabaseSizePages = 100 }, func(h *DatabaseHeader) interface{} { return h.DatabaseSizePages }},
			{"FirstFreelistTrunkPage=5", func(h *DatabaseHeader) { h.FirstFreelistTrunkPage = 5 }, func(h *DatabaseHeader) interface{} { return h.FirstFreelistTrunkPage }},
			{"TotalFreelistPages=3", func(h *DatabaseHeader) { h.TotalFreelistPages = 3 }, func(h *DatabaseHeader) interface{} { return h.TotalFreelistPages }},
			{"SchemaCookie=7", func(h *DatabaseHeader) { h.SchemaCookie = 7 }, func(h *DatabaseHeader) interface{} { return h.SchemaCookie }},
			{"SchemaFormatNumber=4", func(h *DatabaseHeader) { h.SchemaFormatNumber = 4 }, func(h *DatabaseHeader) interface{} { return h.SchemaFormatNumber }},
			{"DefaultPageCacheSize=50", func(h *DatabaseHeader) { h.DefaultPageCacheSize = 50 }, func(h *DatabaseHeader) interface{} { return h.DefaultPageCacheSize }},
			{"LargestRootBTreePage=8", func(h *DatabaseHeader) { h.LargestRootBTreePage = 8 }, func(h *DatabaseHeader) interface{} { return h.LargestRootBTreePage }},
			{"TextEncoding=UTF16le", func(h *DatabaseHeader) { h.TextEncoding = 2 }, func(h *DatabaseHeader) interface{} { return h.TextEncoding }},
			{"UserVersion=99", func(h *DatabaseHeader) { h.UserVersion = 99 }, func(h *DatabaseHeader) interface{} { return h.UserVersion }},
			{"IncrementalVacuum=1", func(h *DatabaseHeader) { h.IncrementalVacuum = 1 }, func(h *DatabaseHeader) interface{} { return h.IncrementalVacuum }},
			{"ApplicationID=0x12345678", func(h *DatabaseHeader) { h.ApplicationID = 0x12345678 }, func(h *DatabaseHeader) interface{} { return h.ApplicationID }},
			{"VersionValidFor=1000", func(h *DatabaseHeader) { h.VersionValidFor = 1000 }, func(h *DatabaseHeader) interface{} { return h.VersionValidFor }},
			{"SQLiteVersionNumber=3039004", func(h *DatabaseHeader) { h.SQLiteVersionNumber = 3039004 }, func(h *DatabaseHeader) interface{} { return h.SQLiteVersionNumber }},
		}

		for _, f := range fields {
			t.Run(f.name, func(t *testing.T) {
				h := makeDefaultHeader()
				f.modify(h)
				data := WriteHeader(h)
				parsed, err := ReadHeader(data)
				if err != nil {
					t.Fatalf("ReadHeader: %v", err)
				}
				if f.extract(parsed) != f.extract(h) {
					t.Fatalf("field mismatch: got %v, want %v", f.extract(parsed), f.extract(h))
				}
			})
		}
	})
}

func TestDatabaseHeader_Errors(t *testing.T) {
	t.Run("data too small", func(t *testing.T) {
		_, err := ReadHeader(make([]byte, 99))
		if err != ErrHeaderTooSmall {
			t.Fatalf("expected ErrHeaderTooSmall, got %v", err)
		}
	})

	t.Run("empty data", func(t *testing.T) {
		_, err := ReadHeader(nil)
		if err != ErrHeaderTooSmall {
			t.Fatalf("expected ErrHeaderTooSmall, got %v", err)
		}
	})

	t.Run("bad magic", func(t *testing.T) {
		data := make([]byte, 100)
		copy(data, "Bad magic header!")
		_, err := ReadHeader(data)
		if err != ErrBadMagic {
			t.Fatalf("expected ErrBadMagic, got %v", err)
		}
	})

	t.Run("magic missing null terminator", func(t *testing.T) {
		data := make([]byte, 100)
		copy(data, "SQLite format 3!") // '!' instead of '\0'
		_, err := ReadHeader(data)
		if err != ErrBadMagic {
			t.Fatalf("expected ErrBadMagic, got %v", err)
		}
	})

	t.Run("bad page size 1023", func(t *testing.T) {
		h := makeDefaultHeader()
		h.PageSize = 1023
		data := WriteHeader(h)
		_, err := ReadHeader(data)
		if err != ErrBadPageSize {
			t.Fatalf("expected ErrBadPageSize, got %v", err)
		}
	})

	t.Run("bad page size 100", func(t *testing.T) {
		h := makeDefaultHeader()
		h.PageSize = 100
		data := WriteHeader(h)
		_, err := ReadHeader(data)
		if err != ErrBadPageSize {
			t.Fatalf("expected ErrBadPageSize, got %v", err)
		}
	})

	t.Run("valid page sizes", func(t *testing.T) {
		sizes := []int{512, 1024, 2048, 4096, 8192, 16384, 32768, 65536}
		for _, ps := range sizes {
			t.Run("", func(t *testing.T) {
				h := makeDefaultHeader()
				h.PageSize = ps
				data := WriteHeader(h)
				parsed, err := ReadHeader(data)
				if err != nil {
					t.Fatalf("page size %d: %v", ps, err)
				}
				if parsed.PageSize != ps {
					t.Fatalf("page size = %d, want %d", parsed.PageSize, ps)
				}
			})
		}
	})
}

func assertHeadersEqual(t *testing.T, a, b *DatabaseHeader) {
	t.Helper()
	if string(a.Magic[:]) != string(b.Magic[:]) {
		t.Errorf("Magic mismatch")
	}
	if a.PageSize != b.PageSize {
		t.Errorf("PageSize: %d != %d", a.PageSize, b.PageSize)
	}
	if a.FileFormatWriteVersion != b.FileFormatWriteVersion {
		t.Errorf("FileFormatWriteVersion: %d != %d", a.FileFormatWriteVersion, b.FileFormatWriteVersion)
	}
	if a.FileFormatReadVersion != b.FileFormatReadVersion {
		t.Errorf("FileFormatReadVersion: %d != %d", a.FileFormatReadVersion, b.FileFormatReadVersion)
	}
	if a.ReservedSpace != b.ReservedSpace {
		t.Errorf("ReservedSpace: %d != %d", a.ReservedSpace, b.ReservedSpace)
	}
	if a.MaxEmbeddedPayloadFrac != b.MaxEmbeddedPayloadFrac {
		t.Errorf("MaxEmbeddedPayloadFrac: %d != %d", a.MaxEmbeddedPayloadFrac, b.MaxEmbeddedPayloadFrac)
	}
	if a.MinEmbeddedPayloadFrac != b.MinEmbeddedPayloadFrac {
		t.Errorf("MinEmbeddedPayloadFrac: %d != %d", a.MinEmbeddedPayloadFrac, b.MinEmbeddedPayloadFrac)
	}
	if a.LeafPayloadFrac != b.LeafPayloadFrac {
		t.Errorf("LeafPayloadFrac: %d != %d", a.LeafPayloadFrac, b.LeafPayloadFrac)
	}
	if a.FileChangeCounter != b.FileChangeCounter {
		t.Errorf("FileChangeCounter: %d != %d", a.FileChangeCounter, b.FileChangeCounter)
	}
	if a.DatabaseSizePages != b.DatabaseSizePages {
		t.Errorf("DatabaseSizePages: %d != %d", a.DatabaseSizePages, b.DatabaseSizePages)
	}
	if a.FirstFreelistTrunkPage != b.FirstFreelistTrunkPage {
		t.Errorf("FirstFreelistTrunkPage: %d != %d", a.FirstFreelistTrunkPage, b.FirstFreelistTrunkPage)
	}
	if a.TotalFreelistPages != b.TotalFreelistPages {
		t.Errorf("TotalFreelistPages: %d != %d", a.TotalFreelistPages, b.TotalFreelistPages)
	}
	if a.SchemaCookie != b.SchemaCookie {
		t.Errorf("SchemaCookie: %d != %d", a.SchemaCookie, b.SchemaCookie)
	}
	if a.SchemaFormatNumber != b.SchemaFormatNumber {
		t.Errorf("SchemaFormatNumber: %d != %d", a.SchemaFormatNumber, b.SchemaFormatNumber)
	}
	if a.DefaultPageCacheSize != b.DefaultPageCacheSize {
		t.Errorf("DefaultPageCacheSize: %d != %d", a.DefaultPageCacheSize, b.DefaultPageCacheSize)
	}
	if a.LargestRootBTreePage != b.LargestRootBTreePage {
		t.Errorf("LargestRootBTreePage: %d != %d", a.LargestRootBTreePage, b.LargestRootBTreePage)
	}
	if a.TextEncoding != b.TextEncoding {
		t.Errorf("TextEncoding: %d != %d", a.TextEncoding, b.TextEncoding)
	}
	if a.UserVersion != b.UserVersion {
		t.Errorf("UserVersion: %d != %d", a.UserVersion, b.UserVersion)
	}
	if a.IncrementalVacuum != b.IncrementalVacuum {
		t.Errorf("IncrementalVacuum: %d != %d", a.IncrementalVacuum, b.IncrementalVacuum)
	}
	if a.ApplicationID != b.ApplicationID {
		t.Errorf("ApplicationID: %d != %d", a.ApplicationID, b.ApplicationID)
	}
	if a.VersionValidFor != b.VersionValidFor {
		t.Errorf("VersionValidFor: %d != %d", a.VersionValidFor, b.VersionValidFor)
	}
	if a.SQLiteVersionNumber != b.SQLiteVersionNumber {
		t.Errorf("SQLiteVersionNumber: %d != %d", a.SQLiteVersionNumber, b.SQLiteVersionNumber)
	}
}

// ---------------------------------------------------------------------------
// Page Header
// ---------------------------------------------------------------------------

func TestPageHeader_RoundTrip(t *testing.T) {
	t.Run("leaf table page", func(t *testing.T) {
		h := &PageHeader{
			PageType:        pageTypeLeafTable,
			FirstFreeblock:  0,
			CellCount:       3,
			ContentOffset:   4080,
			FragmentedBytes: 0,
		}
		data := WritePageHeader(h)
		if len(data) != 8 {
			t.Fatalf("leaf page header size = %d, want 8", len(data))
		}
		parsed, err := ReadPageHeader(data, false)
		if err != nil {
			t.Fatalf("ReadPageHeader: %v", err)
		}
		assertPageHeadersEqual(t, h, parsed)
	})

	t.Run("interior table page", func(t *testing.T) {
		h := &PageHeader{
			PageType:        pageTypeInteriorTable,
			FirstFreeblock:  0,
			CellCount:       5,
			ContentOffset:   2000,
			FragmentedBytes: 2,
			RightMostPtr:    42,
		}
		data := WritePageHeader(h)
		if len(data) != 12 {
			t.Fatalf("interior page header size = %d, want 12", len(data))
		}
		parsed, err := ReadPageHeader(data, false)
		if err != nil {
			t.Fatalf("ReadPageHeader: %v", err)
		}
		assertPageHeadersEqual(t, h, parsed)
	})

	t.Run("leaf index page", func(t *testing.T) {
		h := &PageHeader{
			PageType:        pageTypeLeafIndex,
			FirstFreeblock:  100,
			CellCount:       10,
			ContentOffset:   3000,
			FragmentedBytes: 5,
		}
		data := WritePageHeader(h)
		if len(data) != 8 {
			t.Fatalf("leaf index page header size = %d, want 8", len(data))
		}
		parsed, err := ReadPageHeader(data, false)
		if err != nil {
			t.Fatalf("ReadPageHeader: %v", err)
		}
		assertPageHeadersEqual(t, h, parsed)
	})

	t.Run("interior index page", func(t *testing.T) {
		h := &PageHeader{
			PageType:        pageTypeInteriorIndex,
			FirstFreeblock:  0,
			CellCount:       2,
			ContentOffset:   500,
			FragmentedBytes: 1,
			RightMostPtr:    99,
		}
		data := WritePageHeader(h)
		if len(data) != 12 {
			t.Fatalf("interior index page header size = %d, want 12", len(data))
		}
		parsed, err := ReadPageHeader(data, false)
		if err != nil {
			t.Fatalf("ReadPageHeader: %v", err)
		}
		assertPageHeadersEqual(t, h, parsed)
	})

	t.Run("page 1 (with 100-byte DB header)", func(t *testing.T) {
		dbHeader := makeDefaultHeader()
		dbData := WriteHeader(dbHeader)
		ph := &PageHeader{
			PageType:        pageTypeLeafTable,
			CellCount:       1,
			ContentOffset:   4080,
			FragmentedBytes: 0,
		}
		pageData := append(dbData, WritePageHeader(ph)...)

		parsed, err := ReadPageHeader(pageData, true)
		if err != nil {
			t.Fatalf("ReadPageHeader page 1: %v", err)
		}
		assertPageHeadersEqual(t, ph, parsed)
	})

	t.Run("header size method", func(t *testing.T) {
		tests := []struct {
			pageType byte
			want     int
		}{
			{pageTypeLeafTable, 8},
			{pageTypeLeafIndex, 8},
			{pageTypeInteriorTable, 12},
			{pageTypeInteriorIndex, 12},
		}
		for _, tt := range tests {
			h := &PageHeader{PageType: tt.pageType}
			if got := h.HeaderSize(); got != tt.want {
				t.Errorf("HeaderSize(%02x) = %d, want %d", tt.pageType, got, tt.want)
			}
		}
	})
}

func TestPageHeader_Errors(t *testing.T) {
	t.Run("data too small", func(t *testing.T) {
		_, err := ReadPageHeader(make([]byte, 7), false)
		if err != ErrPageHeaderTooSmall {
			t.Fatalf("expected ErrPageHeaderTooSmall, got %v", err)
		}
	})

	t.Run("bad page type", func(t *testing.T) {
		data := make([]byte, 12)
		data[0] = 0xFF
		_, err := ReadPageHeader(data, false)
		if err != ErrBadPageType {
			t.Fatalf("expected ErrBadPageType, got %v", err)
		}
	})

	t.Run("interior page truncated", func(t *testing.T) {
		data := make([]byte, 10)
		data[0] = pageTypeInteriorTable
		_, err := ReadPageHeader(data, false)
		if err != ErrPageHeaderTooSmall {
			t.Fatalf("expected ErrPageHeaderTooSmall, got %v", err)
		}
	})

	t.Run("page 1 with insufficient data", func(t *testing.T) {
		_, err := ReadPageHeader(make([]byte, 107), true) // 100 + 7 = not enough
		if err != ErrPageHeaderTooSmall {
			t.Fatalf("expected ErrPageHeaderTooSmall, got %v", err)
		}
	})
}

func assertPageHeadersEqual(t *testing.T, a, b *PageHeader) {
	t.Helper()
	if a.PageType != b.PageType {
		t.Errorf("PageType: %02x != %02x", a.PageType, b.PageType)
	}
	if a.FirstFreeblock != b.FirstFreeblock {
		t.Errorf("FirstFreeblock: %d != %d", a.FirstFreeblock, b.FirstFreeblock)
	}
	if a.CellCount != b.CellCount {
		t.Errorf("CellCount: %d != %d", a.CellCount, b.CellCount)
	}
	if a.ContentOffset != b.ContentOffset {
		t.Errorf("ContentOffset: %d != %d", a.ContentOffset, b.ContentOffset)
	}
	if a.FragmentedBytes != b.FragmentedBytes {
		t.Errorf("FragmentedBytes: %d != %d", a.FragmentedBytes, b.FragmentedBytes)
	}
	if a.RightMostPtr != b.RightMostPtr {
		t.Errorf("RightMostPtr: %d != %d", a.RightMostPtr, b.RightMostPtr)
	}
}

// ---------------------------------------------------------------------------
// Cells
// ---------------------------------------------------------------------------

func TestCell_LeafTable_RoundTrip(t *testing.T) {
	t.Run("simple cell with small integer", func(t *testing.T) {
		rec := &Record{Columns: []Value{
			IntegerValue(42),
			TextValue("hello"),
		}}
		payload := WriteRecord(rec)

		cell := &Cell{
			IsLeaf:      true,
			PayloadSize: uint64(len(payload)),
			RowID:       1,
			Payload:     payload,
		}

		data := WriteLeafTableCell(cell)
		parsed, n, err := ReadCell(data, pageTypeLeafTable)
		if err != nil {
			t.Fatalf("ReadCell: %v", err)
		}
		if n != len(data) {
			t.Errorf("bytes consumed = %d, want %d", n, len(data))
		}
		assertCellsEqual(t, cell, parsed)

		// Parse the record from the payload
		parsedRec, err := ReadRecord(parsed.Payload)
		if err != nil {
			t.Fatalf("ReadRecord: %v", err)
		}
		if len(parsedRec.Columns) != 2 {
			t.Fatalf("columns = %d, want 2", len(parsedRec.Columns))
		}
		if parsedRec.Columns[0].Type != DataTypeInteger || parsedRec.Columns[0].IntVal != 42 {
			t.Errorf("col0 = %v, want integer 42", parsedRec.Columns[0])
		}
		if parsedRec.Columns[1].Type != DataTypeText || parsedRec.Columns[1].TextVal != "hello" {
			t.Errorf("col1 = %v, want text 'hello'", parsedRec.Columns[1])
		}
	})

	t.Run("cell with overflow", func(t *testing.T) {
		payload := make([]byte, 50)
		for i := range payload {
			payload[i] = byte(i)
		}

		cell := &Cell{
			IsLeaf:       true,
			PayloadSize:  uint64(len(payload)),
			RowID:        5,
			Payload:      payload,
			OverflowPage: 42,
		}

		data := WriteLeafTableCell(cell)
		parsed, n, err := ReadCell(data, pageTypeLeafTable)
		if err != nil {
			t.Fatalf("ReadCell: %v", err)
		}
		if n != len(data) {
			t.Errorf("bytes consumed = %d, want %d", n, len(data))
		}
		if parsed.OverflowPage != 42 {
			t.Errorf("OverflowPage = %d, want 42", parsed.OverflowPage)
		}
		if parsed.RowID != 5 {
			t.Errorf("RowID = %d, want 5", parsed.RowID)
		}
		if parsed.PayloadSize != uint64(len(payload)) {
			t.Errorf("PayloadSize = %d, want %d", parsed.PayloadSize, len(payload))
		}
		if len(parsed.Payload) != len(payload) {
			t.Fatalf("Payload len = %d, want %d", len(parsed.Payload), len(payload))
		}
		for i := range payload {
			if parsed.Payload[i] != payload[i] {
				t.Errorf("Payload[%d] = %d, want %d", i, parsed.Payload[i], payload[i])
			}
		}
	})

	t.Run("cell with large rowid", func(t *testing.T) {
		cell := &Cell{
			IsLeaf:      true,
			PayloadSize: 3,
			RowID:       0xFFFFFFFFFF, // large rowid
			Payload:     []byte{1, 2, 3},
		}
		data := WriteLeafTableCell(cell)
		parsed, _, err := ReadCell(data, pageTypeLeafTable)
		if err != nil {
			t.Fatalf("ReadCell: %v", err)
		}
		if parsed.RowID != cell.RowID {
			t.Errorf("RowID = %d, want %d", parsed.RowID, cell.RowID)
		}
	})

	t.Run("empty payload", func(t *testing.T) {
		cell := &Cell{
			IsLeaf:      true,
			PayloadSize: 0,
			RowID:       1,
			Payload:     nil,
		}
		data := WriteLeafTableCell(cell)
		parsed, _, err := ReadCell(data, pageTypeLeafTable)
		if err != nil {
			t.Fatalf("ReadCell: %v", err)
		}
		if parsed.PayloadSize != 0 {
			t.Errorf("PayloadSize = %d, want 0", parsed.PayloadSize)
		}
	})
}

func TestCell_InteriorTable_RoundTrip(t *testing.T) {
	t.Run("simple interior cell", func(t *testing.T) {
		cell := &Cell{
			IsLeaf:        false,
			LeftChildPage: 3,
			Key:           100,
		}
		data := WriteInteriorTableCell(cell)
		parsed, n, err := ReadCell(data, pageTypeInteriorTable)
		if err != nil {
			t.Fatalf("ReadCell: %v", err)
		}
		if n != len(data) {
			t.Errorf("bytes consumed = %d, want %d", n, len(data))
		}
		if parsed.LeftChildPage != 3 {
			t.Errorf("LeftChildPage = %d, want 3", parsed.LeftChildPage)
		}
		if parsed.Key != 100 {
			t.Errorf("Key = %d, want 100", parsed.Key)
		}
	})

	t.Run("interior cell large key", func(t *testing.T) {
		cell := &Cell{
			IsLeaf:        false,
			LeftChildPage: 1,
			Key:           0xFFFFFFFFFFFF,
		}
		data := WriteInteriorTableCell(cell)
		parsed, _, err := ReadCell(data, pageTypeInteriorTable)
		if err != nil {
			t.Fatalf("ReadCell: %v", err)
		}
		if parsed.Key != cell.Key {
			t.Errorf("Key = %d, want %d", parsed.Key, cell.Key)
		}
		if parsed.LeftChildPage != 1 {
			t.Errorf("LeftChildPage = %d, want 1", parsed.LeftChildPage)
		}
	})
}

func TestCell_Errors(t *testing.T) {
	t.Run("empty data", func(t *testing.T) {
		_, _, err := ReadCell(nil, pageTypeLeafTable)
		if err == nil {
			t.Fatal("expected error for empty data")
		}
	})

	t.Run("bad page type", func(t *testing.T) {
		_, _, err := ReadCell(make([]byte, 10), 0xFF)
		if err != ErrBadPageType {
			t.Fatalf("expected ErrBadPageType, got %v", err)
		}
	})

	t.Run("interior cell too small", func(t *testing.T) {
		_, _, err := ReadCell(make([]byte, 3), pageTypeInteriorTable)
		if err != ErrCellTooSmall {
			t.Fatalf("expected ErrCellTooSmall, got %v", err)
		}
	})
}

func assertCellsEqual(t *testing.T, a, b *Cell) {
	t.Helper()
	if a.IsLeaf != b.IsLeaf {
		t.Errorf("IsLeaf: %v != %v", a.IsLeaf, b.IsLeaf)
	}
	if a.LeftChildPage != b.LeftChildPage {
		t.Errorf("LeftChildPage: %d != %d", a.LeftChildPage, b.LeftChildPage)
	}
	if a.PayloadSize != b.PayloadSize {
		t.Errorf("PayloadSize: %d != %d", a.PayloadSize, b.PayloadSize)
	}
	if a.RowID != b.RowID {
		t.Errorf("RowID: %d != %d", a.RowID, b.RowID)
	}
	if a.Key != b.Key {
		t.Errorf("Key: %d != %d", a.Key, b.Key)
	}
	if len(a.Payload) != len(b.Payload) {
		t.Errorf("Payload len: %d != %d", len(a.Payload), len(b.Payload))
		return
	}
	for i := range a.Payload {
		if a.Payload[i] != b.Payload[i] {
			t.Errorf("Payload[%d]: %d != %d", i, a.Payload[i], b.Payload[i])
			break
		}
	}
	if a.OverflowPage != b.OverflowPage {
		t.Errorf("OverflowPage: %d != %d", a.OverflowPage, b.OverflowPage)
	}
}

// ---------------------------------------------------------------------------
// Record
// ---------------------------------------------------------------------------

func TestRecord_RoundTrip(t *testing.T) {
	t.Run("empty record", func(t *testing.T) {
		rec := &Record{Columns: nil}
		data := WriteRecord(rec)
		parsed, err := ReadRecord(data)
		if err != nil {
			t.Fatalf("ReadRecord: %v", err)
		}
		if len(parsed.Columns) != 0 {
			t.Errorf("columns = %d, want 0", len(parsed.Columns))
		}
	})

	t.Run("NULL value", func(t *testing.T) {
		rec := &Record{Columns: []Value{NullValue}}
		data := WriteRecord(rec)
		parsed, err := ReadRecord(data)
		if err != nil {
			t.Fatalf("ReadRecord: %v", err)
		}
		if !parsed.Columns[0].IsNull() {
			t.Errorf("expected NULL, got %v", parsed.Columns[0])
		}
	})

	t.Run("integer zero (serial type 8)", func(t *testing.T) {
		rec := &Record{Columns: []Value{IntegerValue(0)}}
		data := WriteRecord(rec)
		parsed, err := ReadRecord(data)
		if err != nil {
			t.Fatalf("ReadRecord: %v", err)
		}
		if parsed.Columns[0].IntVal != 0 {
			t.Errorf("int value = %d, want 0", parsed.Columns[0].IntVal)
		}
		// Verify it used serial type 8 (0 bytes in body)
		// Header should be: header_size=2, serial_type=8
		if len(data) != 2 {
			t.Errorf("record size = %d, want 2 (header only, no body)", len(data))
		}
	})

	t.Run("integer one (serial type 9)", func(t *testing.T) {
		rec := &Record{Columns: []Value{IntegerValue(1)}}
		data := WriteRecord(rec)
		parsed, err := ReadRecord(data)
		if err != nil {
			t.Fatalf("ReadRecord: %v", err)
		}
		if parsed.Columns[0].IntVal != 1 {
			t.Errorf("int value = %d, want 1", parsed.Columns[0].IntVal)
		}
		if len(data) != 2 {
			t.Errorf("record size = %d, want 2 (header only, no body)", len(data))
		}
	})

	t.Run("int8 range", func(t *testing.T) {
		values := []int64{-128, -1, 2, 127}
		for _, v := range values {
			t.Run("", func(t *testing.T) {
				rec := &Record{Columns: []Value{IntegerValue(v)}}
				data := WriteRecord(rec)
				parsed, err := ReadRecord(data)
				if err != nil {
					t.Fatalf("ReadRecord(%d): %v", v, err)
				}
				if parsed.Columns[0].IntVal != v {
					t.Errorf("got %d, want %d", parsed.Columns[0].IntVal, v)
				}
				// int8 uses serial type 1 = 1 body byte + 2 header bytes = 3 total
				if len(data) != 3 {
					t.Errorf("record size for %d = %d, want 3", v, len(data))
				}
			})
		}
	})

	t.Run("int16 range", func(t *testing.T) {
		values := []int64{-32768, -129, 128, 32767}
		for _, v := range values {
			t.Run("", func(t *testing.T) {
				rec := &Record{Columns: []Value{IntegerValue(v)}}
				data := WriteRecord(rec)
				parsed, err := ReadRecord(data)
				if err != nil {
					t.Fatalf("ReadRecord(%d): %v", v, err)
				}
				if parsed.Columns[0].IntVal != v {
					t.Errorf("got %d, want %d", parsed.Columns[0].IntVal, v)
				}
			})
		}
	})

	t.Run("int24 range", func(t *testing.T) {
		values := []int64{-8388608, -32769, 32768, 8388607}
		for _, v := range values {
			t.Run("", func(t *testing.T) {
				rec := &Record{Columns: []Value{IntegerValue(v)}}
				data := WriteRecord(rec)
				parsed, err := ReadRecord(data)
				if err != nil {
					t.Fatalf("ReadRecord(%d): %v", v, err)
				}
				if parsed.Columns[0].IntVal != v {
					t.Errorf("got %d, want %d", parsed.Columns[0].IntVal, v)
				}
			})
		}
	})

	t.Run("int32 range", func(t *testing.T) {
		values := []int64{-2147483648, -8388609, 8388608, 2147483647}
		for _, v := range values {
			t.Run("", func(t *testing.T) {
				rec := &Record{Columns: []Value{IntegerValue(v)}}
				data := WriteRecord(rec)
				parsed, err := ReadRecord(data)
				if err != nil {
					t.Fatalf("ReadRecord(%d): %v", v, err)
				}
				if parsed.Columns[0].IntVal != v {
					t.Errorf("got %d, want %d", parsed.Columns[0].IntVal, v)
				}
			})
		}
	})

	t.Run("int48 range", func(t *testing.T) {
		values := []int64{-140737488355328, -2147483649, 2147483648, 140737488355327}
		for _, v := range values {
			t.Run("", func(t *testing.T) {
				rec := &Record{Columns: []Value{IntegerValue(v)}}
				data := WriteRecord(rec)
				parsed, err := ReadRecord(data)
				if err != nil {
					t.Fatalf("ReadRecord(%d): %v", v, err)
				}
				if parsed.Columns[0].IntVal != v {
					t.Errorf("got %d, want %d", parsed.Columns[0].IntVal, v)
				}
			})
		}
	})

	t.Run("int64 range", func(t *testing.T) {
		values := []int64{
			-9223372036854775808, // min int64
			-140737488355329,
			140737488355328,
			9223372036854775807, // max int64
		}
		for _, v := range values {
			t.Run("", func(t *testing.T) {
				rec := &Record{Columns: []Value{IntegerValue(v)}}
				data := WriteRecord(rec)
				parsed, err := ReadRecord(data)
				if err != nil {
					t.Fatalf("ReadRecord(%d): %v", v, err)
				}
				if parsed.Columns[0].IntVal != v {
					t.Errorf("got %d, want %d", parsed.Columns[0].IntVal, v)
				}
			})
		}
	})

	t.Run("float", func(t *testing.T) {
		values := []float64{0.0, 1.0, -1.0, 3.14159, 1e100, -1e-100, math.Inf(1), math.Inf(-1)}
		for _, v := range values {
			t.Run("", func(t *testing.T) {
				rec := &Record{Columns: []Value{FloatValue(v)}}
				data := WriteRecord(rec)
				parsed, err := ReadRecord(data)
				if err != nil {
					t.Fatalf("ReadRecord(%v): %v", v, err)
				}
				if parsed.Columns[0].FloatVal != v {
					// NaN special case
					if !(math.IsNaN(v) && math.IsNaN(parsed.Columns[0].FloatVal)) {
						t.Errorf("got %v, want %v", parsed.Columns[0].FloatVal, v)
					}
				}
			})
		}
	})

	t.Run("NaN", func(t *testing.T) {
		rec := &Record{Columns: []Value{FloatValue(math.NaN())}}
		data := WriteRecord(rec)
		parsed, err := ReadRecord(data)
		if err != nil {
			t.Fatalf("ReadRecord: %v", err)
		}
		if !math.IsNaN(parsed.Columns[0].FloatVal) {
			t.Errorf("expected NaN, got %v", parsed.Columns[0].FloatVal)
		}
	})

	t.Run("text values", func(t *testing.T) {
		tests := []string{"", "hello", "Hello, World!", "SQLite 版本 3", string(make([]byte, 255))}
		for _, text := range tests {
			t.Run("", func(t *testing.T) {
				rec := &Record{Columns: []Value{TextValue(text)}}
				data := WriteRecord(rec)
				parsed, err := ReadRecord(data)
				if err != nil {
					t.Fatalf("ReadRecord(%q): %v", text, err)
				}
				if parsed.Columns[0].TextVal != text {
					t.Errorf("got %q, want %q", parsed.Columns[0].TextVal, text)
				}
			})
		}
	})

	t.Run("blob values", func(t *testing.T) {
		tests := [][]byte{
			{},
			{0x00},
			{0xFF},
			{0x01, 0x02, 0x03, 0x04},
			make([]byte, 256),
		}
		for _, blob := range tests {
			t.Run("", func(t *testing.T) {
				rec := &Record{Columns: []Value{BlobValue(blob)}}
				data := WriteRecord(rec)
				parsed, err := ReadRecord(data)
				if err != nil {
					t.Fatalf("ReadRecord: %v", err)
				}
				if len(parsed.Columns[0].BlobVal) != len(blob) {
					t.Fatalf("blob len = %d, want %d", len(parsed.Columns[0].BlobVal), len(blob))
				}
				for i := range blob {
					if parsed.Columns[0].BlobVal[i] != blob[i] {
						t.Fatalf("blob[%d] = %d, want %d", i, parsed.Columns[0].BlobVal[i], blob[i])
					}
				}
			})
		}
	})

	t.Run("multi-column record", func(t *testing.T) {
		rec := &Record{Columns: []Value{
			NullValue,
			IntegerValue(42),
			IntegerValue(0),
			IntegerValue(1),
			IntegerValue(-100000),
			FloatValue(2.718),
			TextValue("test"),
			BlobValue([]byte{0xDE, 0xAD, 0xBE, 0xEF}),
		}}
		data := WriteRecord(rec)
		parsed, err := ReadRecord(data)
		if err != nil {
			t.Fatalf("ReadRecord: %v", err)
		}
		if len(parsed.Columns) != 8 {
			t.Fatalf("columns = %d, want 8", len(parsed.Columns))
		}
		if !parsed.Columns[0].IsNull() {
			t.Errorf("col0: expected NULL")
		}
		if parsed.Columns[1].IntVal != 42 {
			t.Errorf("col1: got %d, want 42", parsed.Columns[1].IntVal)
		}
		if parsed.Columns[2].IntVal != 0 {
			t.Errorf("col2: got %d, want 0", parsed.Columns[2].IntVal)
		}
		if parsed.Columns[3].IntVal != 1 {
			t.Errorf("col3: got %d, want 1", parsed.Columns[3].IntVal)
		}
		if parsed.Columns[4].IntVal != -100000 {
			t.Errorf("col4: got %d, want -100000", parsed.Columns[4].IntVal)
		}
		if parsed.Columns[5].FloatVal != 2.718 {
			t.Errorf("col5: got %v, want 2.718", parsed.Columns[5].FloatVal)
		}
		if parsed.Columns[6].TextVal != "test" {
			t.Errorf("col6: got %q, want 'test'", parsed.Columns[6].TextVal)
		}
		if len(parsed.Columns[7].BlobVal) != 4 {
			t.Fatalf("col7 blob len = %d, want 4", len(parsed.Columns[7].BlobVal))
		}
		expectedBlob := []byte{0xDE, 0xAD, 0xBE, 0xEF}
		for i, b := range expectedBlob {
			if parsed.Columns[7].BlobVal[i] != b {
				t.Errorf("col7[%d] = %x, want %x", i, parsed.Columns[7].BlobVal[i], b)
			}
		}
	})

	t.Run("many columns", func(t *testing.T) {
		columns := make([]Value, 100)
		for i := range columns {
			columns[i] = IntegerValue(int64(i))
		}
		rec := &Record{Columns: columns}
		data := WriteRecord(rec)
		parsed, err := ReadRecord(data)
		if err != nil {
			t.Fatalf("ReadRecord: %v", err)
		}
		if len(parsed.Columns) != 100 {
			t.Fatalf("columns = %d, want 100", len(parsed.Columns))
		}
		for i := range parsed.Columns {
			if parsed.Columns[i].IntVal != int64(i) {
				t.Errorf("col[%d] = %d, want %d", i, parsed.Columns[i].IntVal, i)
			}
		}
	})

	t.Run("double round trip", func(t *testing.T) {
		rec := &Record{Columns: []Value{
			NullValue,
			IntegerValue(12345),
			FloatValue(6.789),
			TextValue("round trip"),
			BlobValue([]byte{1, 2, 3}),
		}}
		data1 := WriteRecord(rec)
		parsed1, err := ReadRecord(data1)
		if err != nil {
			t.Fatalf("first ReadRecord: %v", err)
		}
		data2 := WriteRecord(parsed1)
		parsed2, err := ReadRecord(data2)
		if err != nil {
			t.Fatalf("second ReadRecord: %v", err)
		}
		// After two round trips, the bytes should be identical
		if len(data1) != len(data2) {
			t.Fatalf("data sizes differ: %d vs %d", len(data1), len(data2))
		}
		for i := range data1 {
			if data1[i] != data2[i] {
				t.Errorf("data1[%d] = %d, data2[%d] = %d", i, data1[i], i, data2[i])
			}
		}
		// Values should match
		for i := range parsed1.Columns {
			if parsed1.Columns[i].Type != parsed2.Columns[i].Type {
				t.Errorf("col%d type mismatch: %v vs %v", i, parsed1.Columns[i].Type, parsed2.Columns[i].Type)
			}
		}
	})
}

func TestRecord_SerialTypeOptimization(t *testing.T) {
	// Verify that integer encoding picks the smallest serial type
	tests := []struct {
		value     int64
		wantBytes int // expected body bytes
	}{
		{0, 0},                    // serial type 8
		{1, 0},                    // serial type 9
		{127, 1},                  // serial type 1
		{-128, 1},                 // serial type 1
		{128, 2},                  // serial type 2
		{-32768, 2},               // serial type 2
		{32768, 3},                // serial type 3
		{8388608, 4},              // serial type 4
		{-2147483648, 4},          // serial type 4
		{2147483648, 6},           // serial type 5
		{-140737488355328, 6},     // serial type 5
		{140737488355328, 8},      // serial type 6
		{-9223372036854775808, 8}, // serial type 6
	}

	for _, tt := range tests {
		t.Run("", func(t *testing.T) {
			rec := &Record{Columns: []Value{IntegerValue(tt.value)}}
			data := WriteRecord(rec)
			parsed, err := ReadRecord(data)
			if err != nil {
				t.Fatalf("ReadRecord: %v", err)
			}
			if parsed.Columns[0].IntVal != tt.value {
				t.Errorf("value = %d, want %d", parsed.Columns[0].IntVal, tt.value)
			}
			// Header bytes + body bytes
			headerOverhead := 2 // header_size varint + serial_type varint (assuming single-byte each)
			if len(data) != headerOverhead+tt.wantBytes {
				t.Errorf("total record size = %d, want %d (header=%d + body=%d)",
					len(data), headerOverhead+tt.wantBytes, headerOverhead, tt.wantBytes)
			}
		})
	}
}

func TestRecord_Errors(t *testing.T) {
	t.Run("empty data", func(t *testing.T) {
		_, err := ReadRecord(nil)
		if err == nil {
			t.Fatal("expected error for nil data")
		}
	})

	t.Run("truncated body", func(t *testing.T) {
		// Build a record that claims to have data but is truncated
		// Header: size=3, serial_type=4 (int32 needs 4 bytes)
		// Body: only 2 bytes available
		data := []byte{3, 4, 0x00, 0x01} // header says 3 bytes (size + serialtype), then truncated body
		_, err := ReadRecord(data)
		if err == nil {
			t.Fatal("expected error for truncated body")
		}
	})

	t.Run("reserved serial types 10 and 11", func(t *testing.T) {
		// Manually craft a record with serial type 10
		data := []byte{3, 10, 0} // header_size=3, serial_type=10
		_, err := ReadRecord(data)
		if err == nil {
			t.Fatal("expected error for serial type 10")
		}

		data = []byte{3, 11, 0} // header_size=3, serial_type=11
		_, err = ReadRecord(data)
		if err == nil {
			t.Fatal("expected error for serial type 11")
		}
	})

	t.Run("header size larger than data", func(t *testing.T) {
		data := []byte{200, 1, 2} // header_size claims 200 bytes but only 3 available
		_, err := ReadRecord(data)
		if err == nil {
			t.Fatal("expected error for header size larger than data")
		}
	})
}

// ---------------------------------------------------------------------------
// ComputePayloadSizes
// ---------------------------------------------------------------------------

func TestComputePayloadSizes(t *testing.T) {
	t.Run("small payload fits entirely on leaf page", func(t *testing.T) {
		// usableSize=4096, maxLocal=4096-35=4061
		local, overflow := ComputePayloadSizes(100, 4096, true)
		if local != 100 {
			t.Errorf("local = %d, want 100", local)
		}
		if overflow != 0 {
			t.Errorf("overflow = %d, want 0", overflow)
		}
	})

	t.Run("small payload fits entirely on interior page", func(t *testing.T) {
		local, overflow := ComputePayloadSizes(100, 4096, false)
		if local != 100 {
			t.Errorf("local = %d, want 100", local)
		}
		if overflow != 0 {
			t.Errorf("overflow = %d, want 0", overflow)
		}
	})

	t.Run("payload exactly maxLocal on leaf", func(t *testing.T) {
		maxLocal := 4096 - 35 // 4061
		local, overflow := ComputePayloadSizes(maxLocal, 4096, true)
		if local != maxLocal {
			t.Errorf("local = %d, want %d", local, maxLocal)
		}
		if overflow != 0 {
			t.Errorf("overflow = %d, want 0", overflow)
		}
	})

	t.Run("payload one over maxLocal triggers overflow", func(t *testing.T) {
		maxLocal := 4096 - 35 // 4061
		local, overflow := ComputePayloadSizes(maxLocal+1, 4096, true)
		if overflow == 0 {
			t.Error("expected overflow")
		}
		if local+overflow != maxLocal+1 {
			t.Errorf("local(%d) + overflow(%d) = %d, want %d", local, overflow, local+overflow, maxLocal+1)
		}
	})

	t.Run("leaf vs interior have different maxLocal", func(t *testing.T) {
		// For page 4096: leaf_max=4061, interior_max=1002
		// Use a payload that fits on leaf but not interior
		payloadLen := 2000
		localLeaf, overflowLeaf := ComputePayloadSizes(payloadLen, 4096, true)
		localInt, overflowInt := ComputePayloadSizes(payloadLen, 4096, false)

		// Leaf should fit entirely (2000 < 4061)
		if overflowLeaf != 0 {
			t.Errorf("leaf should fit 2000 bytes, got overflow=%d", overflowLeaf)
		}
		if localLeaf != 2000 {
			t.Errorf("leaf local = %d, want 2000", localLeaf)
		}

		// Interior should overflow (2000 > 1002)
		if overflowInt == 0 {
			t.Errorf("interior should overflow for 2000 bytes")
		}
		if localInt+overflowInt != payloadLen {
			t.Errorf("interior total = %d, want %d", localInt+overflowInt, payloadLen)
		}
	})

	t.Run("very large overflow", func(t *testing.T) {
		// 100KB payload on a 4096-byte page
		payloadLen := 100 * 1024
		local, overflow := ComputePayloadSizes(payloadLen, 4096, true)
		if overflow == 0 {
			t.Error("expected overflow for 100KB payload")
		}
		if local+overflow != payloadLen {
			t.Errorf("local(%d) + overflow(%d) = %d, want %d", local, overflow, local+overflow, payloadLen)
		}
	})

	t.Run("small page size", func(t *testing.T) {
		// 512-byte page
		maxLocal := 512 - 35 // 477
		local, overflow := ComputePayloadSizes(500, 512, true)
		if overflow == 0 {
			t.Error("expected overflow on 512-byte page with 500-byte payload")
		}
		if local+overflow != 500 {
			t.Errorf("total = %d, want 500", local+overflow)
		}

		// Payload that fits
		local, overflow = ComputePayloadSizes(maxLocal, 512, true)
		if overflow != 0 {
			t.Errorf("overflow = %d, want 0 for payload that fits", overflow)
		}
	})

	t.Run("large page size", func(t *testing.T) {
		// 65536-byte page
		maxLocal := 65536 - 35 // 65501
		local, overflow := ComputePayloadSizes(maxLocal, 65536, true)
		if local != maxLocal {
			t.Errorf("local = %d, want %d", local, maxLocal)
		}
		if overflow != 0 {
			t.Errorf("overflow = %d, want 0", overflow)
		}
	})

	t.Run("zero payload", func(t *testing.T) {
		local, overflow := ComputePayloadSizes(0, 4096, true)
		if local != 0 {
			t.Errorf("local = %d, want 0", local)
		}
		if overflow != 0 {
			t.Errorf("overflow = %d, want 0", overflow)
		}
	})

	t.Run("various page sizes consistency", func(t *testing.T) {
		pageSizes := []int{512, 1024, 2048, 4096, 8192, 16384, 32768, 65536}
		for _, ps := range pageSizes {
			t.Run("", func(t *testing.T) {
				// Small payload always fits
				local, overflow := ComputePayloadSizes(10, ps, true)
				if local != 10 || overflow != 0 {
					t.Errorf("page %d: local=%d overflow=%d, want local=10 overflow=0", ps, local, overflow)
				}
			})
		}
	})

	t.Run("overflow local size at least minLocal", func(t *testing.T) {
		// For leaf: minLocal = ((4096-12)*32/255)-23 = (4084*32/255)-23 = 512.5-23 = 489
		usableSize := 4096
		minLocal := ((usableSize-12)*32/255 - 23)
		payloadLen := 10000
		local, _ := ComputePayloadSizes(payloadLen, usableSize, true)
		if local < minLocal {
			t.Errorf("local = %d, less than minLocal %d", local, minLocal)
		}
	})
}

// ---------------------------------------------------------------------------
// Overflow Pages
// ---------------------------------------------------------------------------

func TestOverflowPage_RoundTrip(t *testing.T) {
	t.Run("simple overflow page", func(t *testing.T) {
		usableSize := 4096
		payload := make([]byte, usableSize-4)
		for i := range payload {
			payload[i] = byte(i)
		}

		data := WriteOverflowPage(42, payload, usableSize)
		nextPage, parsedPayload, err := ReadOverflowPage(data, usableSize)
		if err != nil {
			t.Fatalf("ReadOverflowPage: %v", err)
		}
		if nextPage != 42 {
			t.Errorf("nextPage = %d, want 42", nextPage)
		}
		if len(parsedPayload) != len(payload) {
			t.Fatalf("payload len = %d, want %d", len(parsedPayload), len(payload))
		}
		for i := range payload {
			if parsedPayload[i] != payload[i] {
				t.Errorf("payload[%d] = %d, want %d", i, parsedPayload[i], payload[i])
				break
			}
		}
	})

	t.Run("terminal overflow page (nextPage=0)", func(t *testing.T) {
		usableSize := 4096
		payload := make([]byte, 100)
		data := WriteOverflowPage(0, payload, usableSize)
		nextPage, parsedPayload, err := ReadOverflowPage(data, usableSize)
		if err != nil {
			t.Fatalf("ReadOverflowPage: %v", err)
		}
		if nextPage != 0 {
			t.Errorf("nextPage = %d, want 0", nextPage)
		}
		if len(parsedPayload) != usableSize-4 {
			t.Errorf("payload always returns usableSize-4 bytes")
		}
	})

	t.Run("overflow page too small", func(t *testing.T) {
		data := make([]byte, 100)
		_, _, err := ReadOverflowPage(data, 4096)
		if err == nil {
			t.Fatal("expected error for small overflow page")
		}
	})
}

// ---------------------------------------------------------------------------
// Freelist
// ---------------------------------------------------------------------------

func TestFreelist_RoundTrip(t *testing.T) {
	t.Run("empty freelist trunk", func(t *testing.T) {
		usableSize := 4096
		trunk := &FreelistTrunk{
			NextTrunkPage: 0,
			LeafPages:     nil,
		}
		data := WriteFreelistTrunk(trunk, usableSize)
		parsed, err := ReadFreelistTrunk(data, usableSize)
		if err != nil {
			t.Fatalf("ReadFreelistTrunk: %v", err)
		}
		if parsed.NextTrunkPage != 0 {
			t.Errorf("NextTrunkPage = %d, want 0", parsed.NextTrunkPage)
		}
		if len(parsed.LeafPages) != 0 {
			t.Errorf("LeafPages count = %d, want 0", len(parsed.LeafPages))
		}
	})

	t.Run("freelist trunk with leaf pages", func(t *testing.T) {
		usableSize := 4096
		trunk := &FreelistTrunk{
			NextTrunkPage: 10,
			LeafPages:     []uint32{3, 4, 5, 6, 7},
		}
		data := WriteFreelistTrunk(trunk, usableSize)
		parsed, err := ReadFreelistTrunk(data, usableSize)
		if err != nil {
			t.Fatalf("ReadFreelistTrunk: %v", err)
		}
		if parsed.NextTrunkPage != 10 {
			t.Errorf("NextTrunkPage = %d, want 10", parsed.NextTrunkPage)
		}
		if len(parsed.LeafPages) != 5 {
			t.Fatalf("LeafPages count = %d, want 5", len(parsed.LeafPages))
		}
		for i, lp := range trunk.LeafPages {
			if parsed.LeafPages[i] != lp {
				t.Errorf("LeafPages[%d] = %d, want %d", i, parsed.LeafPages[i], lp)
			}
		}
	})

	t.Run("freelist max leaf pages", func(t *testing.T) {
		usableSize := 4096
		maxLeafPages := (usableSize - 8) / 4 // 1022
		leaves := make([]uint32, maxLeafPages)
		for i := range leaves {
			leaves[i] = uint32(i + 100)
		}
		trunk := &FreelistTrunk{
			NextTrunkPage: 0,
			LeafPages:     leaves,
		}
		data := WriteFreelistTrunk(trunk, usableSize)
		parsed, err := ReadFreelistTrunk(data, usableSize)
		if err != nil {
			t.Fatalf("ReadFreelistTrunk: %v", err)
		}
		if len(parsed.LeafPages) != maxLeafPages {
			t.Errorf("LeafPages count = %d, want %d", len(parsed.LeafPages), maxLeafPages)
		}
	})

	t.Run("freelist trunk too small", func(t *testing.T) {
		data := make([]byte, 100)
		_, err := ReadFreelistTrunk(data, 4096)
		if err == nil {
			t.Fatal("expected error for small freelist trunk")
		}
	})
}

// ---------------------------------------------------------------------------
// Cell Pointer Array
// ---------------------------------------------------------------------------

func TestCellPointers(t *testing.T) {
	t.Run("read and write", func(t *testing.T) {
		ptrs := []uint16{100, 200, 300, 400}
		data := WriteCellPointers(ptrs)
		if len(data) != 8 {
			t.Fatalf("cell pointer data size = %d, want 8", len(data))
		}
		parsed := ReadCellPointers(data, 0, 4)
		for i, p := range ptrs {
			if parsed[i] != p {
				t.Errorf("ptr[%d] = %d, want %d", i, parsed[i], p)
			}
		}
	})

	t.Run("empty", func(t *testing.T) {
		data := WriteCellPointers(nil)
		if len(data) != 0 {
			t.Errorf("empty cell pointer data = %d bytes, want 0", len(data))
		}
	})
}

// ---------------------------------------------------------------------------
// Complete page integration
// ---------------------------------------------------------------------------

func TestCompletePage_Integration(t *testing.T) {
	t.Run("page 1 with header + leaf table page + cells", func(t *testing.T) {
		// Build a complete page 1
		pageSize := 4096
		usableSize := pageSize

		// Database header
		dbHeader := makeDefaultHeader()
		dbData := WriteHeader(dbHeader)

		// Create some cells with records
		record1 := &Record{Columns: []Value{IntegerValue(1), TextValue("Alice"), IntegerValue(30)}}
		record2 := &Record{Columns: []Value{IntegerValue(2), TextValue("Bob"), IntegerValue(25)}}
		record3 := &Record{Columns: []Value{IntegerValue(3), TextValue("Charlie"), IntegerValue(35)}}

		payload1 := WriteRecord(record1)
		payload2 := WriteRecord(record2)
		payload3 := WriteRecord(record3)

		cell1 := &Cell{IsLeaf: true, PayloadSize: uint64(len(payload1)), RowID: 1, Payload: payload1}
		cell2 := &Cell{IsLeaf: true, PayloadSize: uint64(len(payload2)), RowID: 2, Payload: payload2}
		cell3 := &Cell{IsLeaf: true, PayloadSize: uint64(len(payload3)), RowID: 3, Payload: payload3}

		cellData1 := WriteLeafTableCell(cell1)
		cellData2 := WriteLeafTableCell(cell2)
		cellData3 := WriteLeafTableCell(cell3)

		// Layout: DB header (100) + page header (8) + cell pointers (6) + cells (from end)
		pageHeaderOffset := headerSize            // 100
		cellPointerOffset := pageHeaderOffset + 8 // 108

		// Place cells at the end of the page
		cell3Offset := usableSize - len(cellData3)
		cell2Offset := cell3Offset - len(cellData2)
		cell1Offset := cell2Offset - len(cellData1)

		pageData := make([]byte, pageSize)
		copy(pageData, dbData)

		// Write page header
		ph := &PageHeader{
			PageType:      pageTypeLeafTable,
			CellCount:     3,
			ContentOffset: uint16(cell1Offset),
		}
		copy(pageData[pageHeaderOffset:], WritePageHeader(ph))

		// Write cell pointers
		cellPtrs := []uint16{uint16(cell1Offset), uint16(cell2Offset), uint16(cell3Offset)}
		ptrData := WriteCellPointers(cellPtrs)
		copy(pageData[cellPointerOffset:], ptrData)

		// Write cells
		copy(pageData[cell1Offset:], cellData1)
		copy(pageData[cell2Offset:], cellData2)
		copy(pageData[cell3Offset:], cellData3)

		// Now parse it back
		// 1. Parse DB header
		parsedDBHeader, err := ReadHeader(pageData)
		if err != nil {
			t.Fatalf("ReadHeader: %v", err)
		}
		if parsedDBHeader.PageSize != pageSize {
			t.Errorf("PageSize = %d, want %d", parsedDBHeader.PageSize, pageSize)
		}

		// 2. Parse page header
		parsedPH, err := ReadPageHeader(pageData, true)
		if err != nil {
			t.Fatalf("ReadPageHeader: %v", err)
		}
		if parsedPH.PageType != pageTypeLeafTable {
			t.Errorf("PageType = %02x, want %02x", parsedPH.PageType, pageTypeLeafTable)
		}
		if parsedPH.CellCount != 3 {
			t.Errorf("CellCount = %d, want 3", parsedPH.CellCount)
		}

		// 3. Read cell pointers
		ptrs := ReadCellPointers(pageData, cellPointerOffset, int(parsedPH.CellCount))

		// 4. Read and verify each cell
		expectedNames := []string{"Alice", "Bob", "Charlie"}
		expectedAges := []int64{30, 25, 35}
		for i, ptr := range ptrs {
			cell, _, err := ReadCell(pageData[ptr:], pageTypeLeafTable)
			if err != nil {
				t.Fatalf("ReadCell %d: %v", i, err)
			}
			rec, err := ReadRecord(cell.Payload)
			if err != nil {
				t.Fatalf("ReadRecord %d: %v", i, err)
			}
			if rec.Columns[1].TextVal != expectedNames[i] {
				t.Errorf("cell %d name = %q, want %q", i, rec.Columns[1].TextVal, expectedNames[i])
			}
			if rec.Columns[2].IntVal != expectedAges[i] {
				t.Errorf("cell %d age = %d, want %d", i, rec.Columns[2].IntVal, expectedAges[i])
			}
		}
	})

	t.Run("interior table page", func(t *testing.T) {
		pageSize := 4096

		cell1 := &Cell{IsLeaf: false, LeftChildPage: 2, Key: 10}
		cell2 := &Cell{IsLeaf: false, LeftChildPage: 3, Key: 20}

		cellData1 := WriteInteriorTableCell(cell1)
		cellData2 := WriteInteriorTableCell(cell2)

		ph := &PageHeader{
			PageType:      pageTypeInteriorTable,
			CellCount:     2,
			ContentOffset: uint16(pageSize - len(cellData1) - len(cellData2)),
			RightMostPtr:  4,
		}

		pageData := make([]byte, pageSize)
		copy(pageData, WritePageHeader(ph))

		cell2Offset := pageSize - len(cellData2)
		cell1Offset := cell2Offset - len(cellData1)
		copy(pageData[cell1Offset:], cellData1)
		copy(pageData[cell2Offset:], cellData2)

		ptrs := []uint16{uint16(cell1Offset), uint16(cell2Offset)}
		copy(pageData[12:], WriteCellPointers(ptrs))

		// Parse
		parsedPH, err := ReadPageHeader(pageData, false)
		if err != nil {
			t.Fatalf("ReadPageHeader: %v", err)
		}
		if parsedPH.RightMostPtr != 4 {
			t.Errorf("RightMostPtr = %d, want 4", parsedPH.RightMostPtr)
		}
		if parsedPH.CellCount != 2 {
			t.Errorf("CellCount = %d, want 2", parsedPH.CellCount)
		}

		parsedPtrs := ReadCellPointers(pageData, 12, 2)
		c1, _, err := ReadCell(pageData[parsedPtrs[0]:], pageTypeInteriorTable)
		if err != nil {
			t.Fatalf("ReadCell 0: %v", err)
		}
		if c1.LeftChildPage != 2 || c1.Key != 10 {
			t.Errorf("cell0: left=%d key=%d, want left=2 key=10", c1.LeftChildPage, c1.Key)
		}

		c2, _, err := ReadCell(pageData[parsedPtrs[1]:], pageTypeInteriorTable)
		if err != nil {
			t.Fatalf("ReadCell 1: %v", err)
		}
		if c2.LeftChildPage != 3 || c2.Key != 20 {
			t.Errorf("cell1: left=%d key=%d, want left=3 key=20", c2.LeftChildPage, c2.Key)
		}
	})
}

// ---------------------------------------------------------------------------
// Varint helpers used by file format
// ---------------------------------------------------------------------------

func TestVarintRaw_SingleByte(t *testing.T) {
	// Test that varint 0 encodes/decodes correctly in record context
	rec := &Record{Columns: []Value{IntegerValue(0)}}
	data := WriteRecord(rec)
	parsed, err := ReadRecord(data)
	if err != nil {
		t.Fatalf("ReadRecord: %v", err)
	}
	if parsed.Columns[0].IntVal != 0 {
		t.Errorf("got %d, want 0", parsed.Columns[0].IntVal)
	}
}

// ---------------------------------------------------------------------------
// Edge cases
// ---------------------------------------------------------------------------

func TestEdgeCases(t *testing.T) {
	t.Run("record with only NULLs", func(t *testing.T) {
		rec := &Record{Columns: []Value{NullValue, NullValue, NullValue}}
		data := WriteRecord(rec)
		parsed, err := ReadRecord(data)
		if err != nil {
			t.Fatalf("ReadRecord: %v", err)
		}
		for i, col := range parsed.Columns {
			if !col.IsNull() {
				t.Errorf("col[%d]: expected NULL, got %v", i, col)
			}
		}
		// All NULLs = header only (no body bytes)
		// header: size_varint + 3 serial_type varints (all 0)
		// size_varint=4, three 0 bytes = 4 bytes total
		if len(data) != 4 {
			t.Errorf("record size = %d, want 4 (all NULLs, header only)", len(data))
		}
	})

	t.Run("record with large text", func(t *testing.T) {
		text := string(make([]byte, 10000))
		rec := &Record{Columns: []Value{TextValue(text)}}
		data := WriteRecord(rec)
		parsed, err := ReadRecord(data)
		if err != nil {
			t.Fatalf("ReadRecord: %v", err)
		}
		if len(parsed.Columns[0].TextVal) != len(text) {
			t.Errorf("text len = %d, want %d", len(parsed.Columns[0].TextVal), len(text))
		}
	})

	t.Run("record with large blob", func(t *testing.T) {
		blob := make([]byte, 50000)
		for i := range blob {
			blob[i] = byte(i % 256)
		}
		rec := &Record{Columns: []Value{BlobValue(blob)}}
		data := WriteRecord(rec)
		parsed, err := ReadRecord(data)
		if err != nil {
			t.Fatalf("ReadRecord: %v", err)
		}
		if len(parsed.Columns[0].BlobVal) != len(blob) {
			t.Errorf("blob len = %d, want %d", len(parsed.Columns[0].BlobVal), len(blob))
		}
		for i := range blob {
			if parsed.Columns[0].BlobVal[i] != blob[i] {
				t.Fatalf("blob[%d] mismatch", i)
			}
		}
	})

	t.Run("negative zero float", func(t *testing.T) {
		rec := &Record{Columns: []Value{FloatValue(math.Copysign(0.0, -1.0))}}
		data := WriteRecord(rec)
		parsed, err := ReadRecord(data)
		if err != nil {
			t.Fatalf("ReadRecord: %v", err)
		}
		if math.Signbit(parsed.Columns[0].FloatVal) != math.Signbit(math.Copysign(0.0, -1.0)) {
			t.Errorf("negative zero sign bit lost")
		}
	})

	t.Run("integer boundary values", func(t *testing.T) {
		boundaries := []int64{
			0, 1, -1,
			127, 128, -128, -129,
			32767, 32768, -32768, -32769,
			8388607, 8388608, -8388608, -8388609,
			2147483647, 2147483648, -2147483648, -2147483649,
			140737488355327, 140737488355328, -140737488355328, -140737488355329,
			9223372036854775807, -9223372036854775808,
		}
		for _, v := range boundaries {
			rec := &Record{Columns: []Value{IntegerValue(v)}}
			data := WriteRecord(rec)
			parsed, err := ReadRecord(data)
			if err != nil {
				t.Fatalf("ReadRecord(%d): %v", v, err)
			}
			if parsed.Columns[0].IntVal != v {
				t.Errorf("got %d, want %d", parsed.Columns[0].IntVal, v)
			}
		}
	})

	t.Run("MaxOverflowPayload", func(t *testing.T) {
		got := MaxOverflowPayload(4096)
		want := 4092 // 4096 - 4
		if got != want {
			t.Errorf("MaxOverflowPayload(4096) = %d, want %d", got, want)
		}
	})
}

// ---------------------------------------------------------------------------
// Manual record construction / parsing
// ---------------------------------------------------------------------------

func TestManualRecord(t *testing.T) {
	t.Run("manually constructed record", func(t *testing.T) {
		// Build a record manually:
		// Header: 04 01 02 03  (header_size=4, serial types: int8, int16, 24-bit int)
		// Body: 2A 00 0A 00 00 01
		//        ^42 ^10      ^1
		header := []byte{4, 1, 2, 3}
		body := []byte{0x2A, 0x00, 0x0A, 0x00, 0x00, 0x01}
		data := append(header, body...)

		rec, err := ReadRecord(data)
		if err != nil {
			t.Fatalf("ReadRecord: %v", err)
		}
		if len(rec.Columns) != 3 {
			t.Fatalf("columns = %d, want 3", len(rec.Columns))
		}
		if rec.Columns[0].IntVal != 42 {
			t.Errorf("col0 = %d, want 42", rec.Columns[0].IntVal)
		}
		if rec.Columns[1].IntVal != 10 {
			t.Errorf("col1 = %d, want 10", rec.Columns[1].IntVal)
		}
		if rec.Columns[2].IntVal != 1 {
			t.Errorf("col2 = %d, want 1", rec.Columns[2].IntVal)
		}
	})

	t.Run("record with text and blob serial types", func(t *testing.T) {
		// Header: 06 0D 0E  (header_size=6, serial types: text 0 bytes=13, blob 0 bytes=12)
		// Wait, let me recalculate:
		// text of length 3: serial_type = 3*2+13 = 19
		// blob of length 2: serial_type = 2*2+12 = 16
		// header_size = 1(size) + 1(text serial) + 1(blob serial) = 3... but varint for 3 is just 03
		// So header = [03, 19, 16]
		// body = "hi!" + [0xFF, 0xFE]
		header := []byte{3, 19, 16}
		body := append([]byte("hi!"), 0xFF, 0xFE)
		data := append(header, body...)

		rec, err := ReadRecord(data)
		if err != nil {
			t.Fatalf("ReadRecord: %v", err)
		}
		if len(rec.Columns) != 2 {
			t.Fatalf("columns = %d, want 2", len(rec.Columns))
		}
		if rec.Columns[0].TextVal != "hi!" {
			t.Errorf("col0 = %q, want 'hi!'", rec.Columns[0].TextVal)
		}
		if len(rec.Columns[1].BlobVal) != 2 || rec.Columns[1].BlobVal[0] != 0xFF || rec.Columns[1].BlobVal[1] != 0xFE {
			t.Errorf("col1 = %v, want [FF FE]", rec.Columns[1].BlobVal)
		}
	})
}

// ---------------------------------------------------------------------------
// Valid page size
// ---------------------------------------------------------------------------

func TestValidPageSize(t *testing.T) {
	tests := []struct {
		size int
		want bool
	}{
		{0, false},
		{1, false},
		{100, false},
		{500, false},
		{511, false},
		{512, true},
		{1023, false},
		{1024, true},
		{2048, true},
		{4096, true},
		{8192, true},
		{16384, true},
		{32768, true},
		{65535, false},
		{65536, true},
	}

	for _, tt := range tests {
		t.Run("", func(t *testing.T) {
			got := validPageSize(tt.size)
			if got != tt.want {
				t.Errorf("validPageSize(%d) = %v, want %v", tt.size, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// encodeSerialValue / decodeSerialValue symmetry
// ---------------------------------------------------------------------------

func TestSerialValue_Symmetry(t *testing.T) {
	t.Run("all integer sizes", func(t *testing.T) {
		values := []int64{
			0, 1, -1,
			127, -128,
			32767, -32768,
			8388607, -8388608,
			2147483647, -2147483648,
			140737488355327, -140737488355328,
			9223372036854775807, -9223372036854775808,
		}
		for _, v := range values {
			st, body := encodeSerialValue(IntegerValue(v))
			val, size, err := decodeSerialValue(body, 0, st)
			if err != nil {
				t.Fatalf("decodeSerialValue(%d, serial=%d): %v", v, st, err)
			}
			if size != len(body) {
				t.Errorf("size mismatch for %d: %d vs %d", v, size, len(body))
			}
			if val.IntVal != v {
				t.Errorf("encode/decode mismatch: got %d, want %d (serial type %d)", val.IntVal, v, st)
			}
		}
	})

	t.Run("float", func(t *testing.T) {
		st, body := encodeSerialValue(FloatValue(3.14159))
		if st != 7 {
			t.Errorf("serial type = %d, want 7", st)
		}
		val, size, err := decodeSerialValue(body, 0, st)
		if err != nil {
			t.Fatalf("decodeSerialValue: %v", err)
		}
		if val.FloatVal != 3.14159 {
			t.Errorf("got %v, want 3.14159", val.FloatVal)
		}
		if size != 8 {
			t.Errorf("size = %d, want 8", size)
		}
	})

	t.Run("text", func(t *testing.T) {
		text := "Hello, SQLite!"
		st, body := encodeSerialValue(TextValue(text))
		expectedST := uint64(len(text)*2 + 13)
		if st != expectedST {
			t.Errorf("serial type = %d, want %d", st, expectedST)
		}
		val, size, err := decodeSerialValue(body, 0, st)
		if err != nil {
			t.Fatalf("decodeSerialValue: %v", err)
		}
		if val.TextVal != text {
			t.Errorf("got %q, want %q", val.TextVal, text)
		}
		if size != len(text) {
			t.Errorf("size = %d, want %d", size, len(text))
		}
	})

	t.Run("blob", func(t *testing.T) {
		blob := []byte{0x00, 0x01, 0x02, 0xFF}
		st, body := encodeSerialValue(BlobValue(blob))
		expectedST := uint64(len(blob)*2 + 12)
		if st != expectedST {
			t.Errorf("serial type = %d, want %d", st, expectedST)
		}
		val, size, err := decodeSerialValue(body, 0, st)
		if err != nil {
			t.Fatalf("decodeSerialValue: %v", err)
		}
		if len(val.BlobVal) != len(blob) {
			t.Fatalf("blob len = %d, want %d", len(val.BlobVal), len(blob))
		}
		for i, b := range blob {
			if val.BlobVal[i] != b {
				t.Errorf("blob[%d] = %d, want %d", i, val.BlobVal[i], b)
			}
		}
		if size != len(blob) {
			t.Errorf("size = %d, want %d", size, len(blob))
		}
	})

	t.Run("null", func(t *testing.T) {
		st, body := encodeSerialValue(NullValue)
		if st != 0 {
			t.Errorf("serial type = %d, want 0", st)
		}
		if len(body) != 0 {
			t.Errorf("body = %d bytes, want 0", len(body))
		}
		val, size, err := decodeSerialValue(nil, 0, st)
		if err != nil {
			t.Fatalf("decodeSerialValue: %v", err)
		}
		if !val.IsNull() {
			t.Errorf("expected NULL")
		}
		if size != 0 {
			t.Errorf("size = %d, want 0", size)
		}
	})
}

// ---------------------------------------------------------------------------
// 24-bit integer edge cases (tricky sign extension)
// ---------------------------------------------------------------------------

func TestInt24_SignExtension(t *testing.T) {
	tests := []struct {
		bytes    []byte
		expected int64
	}{
		{[]byte{0x7F, 0xFF, 0xFF}, 8388607},  // max positive
		{[]byte{0x80, 0x00, 0x00}, -8388608}, // max negative
		{[]byte{0xFF, 0xFF, 0xFF}, -1},       // -1
		{[]byte{0x00, 0x00, 0x01}, 1},        // 1
		{[]byte{0x00, 0x80, 0x00}, 32768},    // 32768
		{[]byte{0xFF, 0x7F, 0xFF}, -32769},   // -32769
	}

	for _, tt := range tests {
		// Construct a record with serial type 3 (int24)
		header := []byte{2, 3} // header_size=2, serial_type=3
		data := append(header, tt.bytes...)
		rec, err := ReadRecord(data)
		if err != nil {
			t.Fatalf("ReadRecord(%v): %v", tt.bytes, err)
		}
		if rec.Columns[0].IntVal != tt.expected {
			t.Errorf("got %d, want %d for bytes %v", rec.Columns[0].IntVal, tt.expected, tt.bytes)
		}
	}
}

// ---------------------------------------------------------------------------
// 48-bit integer edge cases
// ---------------------------------------------------------------------------

func TestInt48_SignExtension(t *testing.T) {
	tests := []struct {
		bytes    []byte
		expected int64
	}{
		{[]byte{0x7F, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF}, 140737488355327},  // max positive
		{[]byte{0x80, 0x00, 0x00, 0x00, 0x00, 0x00}, -140737488355328}, // max negative
		{[]byte{0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF}, -1},               // -1
	}

	for _, tt := range tests {
		header := []byte{2, 5} // header_size=2, serial_type=5
		data := append(header, tt.bytes...)
		rec, err := ReadRecord(data)
		if err != nil {
			t.Fatalf("ReadRecord(%v): %v", tt.bytes, err)
		}
		if rec.Columns[0].IntVal != tt.expected {
			t.Errorf("got %d, want %d for bytes %v", rec.Columns[0].IntVal, tt.expected, tt.bytes)
		}
	}
}
