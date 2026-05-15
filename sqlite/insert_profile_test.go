package sqlite

import (
	"fmt"
	"testing"
)

func BenchmarkInsertBreakdown(b *testing.B) {
	e := newBenchEngine(b)
	e.Execute("CREATE TABLE t (id INTEGER PRIMARY KEY, name TEXT, score REAL)")

	// Parse overhead
	b.Run("ParseOnly", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			NewParser(fmt.Sprintf("INSERT INTO t (id, name, score) VALUES (%d, 'user_%d', %f)", i, i, float64(i)*1.1)).Parse()
		}
	})

	// Record writing overhead
	b.Run("WriteRecord", func(b *testing.B) {
		rowValues := []Value{IntegerValue(1), TextValue("hello"), FloatValue(1.5)}
		for i := 0; i < b.N; i++ {
			WriteRecord(valuesToRecord(rowValues))
		}
	})

	// BTree insert only (no SQL parsing, no schema lookup)
	b.Run("BTreeInsertOnly", func(b *testing.B) {
		rowValues := []Value{IntegerValue(1), TextValue("hello"), FloatValue(1.5)}
		record := valuesToRecord(rowValues)
		payload := WriteRecord(record)
		rowid := int64(100)
		for i := 0; i < b.N; i++ {
			cell := e.btree.(*BTree).buildLeafCell(rowid, payload)
			// Don't actually insert to avoid page splits
			_ = cell
			rowid++
		}
	})

	// Full insert (for comparison)
	b.Run("FullInsert", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			e.Execute(fmt.Sprintf("INSERT INTO t (id, name, score) VALUES (%d, 'user_%d', %f)", i, i, float64(i)*1.1))
		}
	})
}
