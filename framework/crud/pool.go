package crud

import (
	"database/sql"
	"sync"

	"github.com/DonaldMurillo/gofastr/core/schema"
)

// maxPooledMapEntries is the largest map or slice put back in a pool.
const maxPooledMapEntries = 4096

var rowSlicePool = sync.Pool{
	New: func() any {
		s := make([]map[string]any, 0, 32)
		return &s
	},
}

var rowMapPool = sync.Pool{
	New: func() any {
		m := make(map[string]any, 16)
		return &m
	},
}

var ptrSlicePool = sync.Pool{
	New: func() any {
		s := make([]any, 0, 16)
		return &s
	},
}

func borrowRowSlice() *[]map[string]any {
	return rowSlicePool.Get().(*[]map[string]any)
}

func returnRowSlice(s *[]map[string]any) {
	for i := range *s {
		m := (*s)[i]
		mapLen := len(m)
		for k := range m {
			delete(m, k)
		}
		if mapLen <= maxPooledMapEntries {
			rowMapPool.Put(&m)
		}
		(*s)[i] = nil
	}
	if cap(*s) > maxPooledMapEntries {
		return
	}
	*s = (*s)[:0]
	rowSlicePool.Put(s)
}

func borrowPtrSlice(n int) *[]any {
	s := ptrSlicePool.Get().(*[]any)
	if cap(*s) < n {
		*s = make([]any, n)
	} else {
		*s = (*s)[:n]
	}
	return s
}

func returnPtrSlice(s *[]any) {
	for i := range *s {
		(*s)[i] = nil
	}
	if cap(*s) > maxPooledMapEntries {
		return
	}
	*s = (*s)[:0]
	ptrSlicePool.Put(s)
}

// scanRowsPooled scans rows into pooled maps; the caller returns the result with returnRowSlice.
func scanRowsPooled(rows *sql.Rows, cols []string, keyFunc func(string) string) (*[]map[string]any, error) {
	return scanRowsPooledForEntity(rows, cols, keyFunc, nil)
}

func scanRowsPooledWithKeys(rows *sql.Rows, cols, keys []string) (*[]map[string]any, error) {
	return scanRowsPooledWithKeysForEntity(rows, cols, keys, nil)
}

func scanRowsPooledForEntity(rows *sql.Rows, cols []string, keyFunc func(string) string, fields []schema.Field) (*[]map[string]any, error) {
	return scanRowsPooledWithKeysForEntity(rows, cols, convertedKeys(cols, keyFunc), fields)
}

func scanRowsPooledWithKeysForEntity(rows *sql.Rows, cols, keys []string, fields []schema.Field) (*[]map[string]any, error) {
	results := borrowRowSlice()
	ncol := len(cols)
	boolCols := databaseBoolColumnsForEntity(rows, ncol, fields, cols)
	for rows.Next() {
		ptrs := borrowPtrSlice(ncol)
		valuesPtr := borrowPtrSlice(ncol)
		values := *valuesPtr
		for i := range ncol {
			(*ptrs)[i] = &values[i]
		}
		if err := rows.Scan(*ptrs...); err != nil {
			returnPtrSlice(ptrs)
			returnPtrSlice(valuesPtr)
			returnRowSlice(results)
			return nil, err
		}
		rowPtr := rowMapPool.Get().(*map[string]any)
		row := *rowPtr
		for i := range ncol {
			row[keys[i]] = convertDatabaseValue(values[i], boolCols[i])
		}
		*results = append(*results, row)
		returnPtrSlice(ptrs)
		returnPtrSlice(valuesPtr)
	}
	if err := rows.Err(); err != nil {
		returnRowSlice(results)
		return nil, err
	}
	return results, nil
}

func convertedKeys(cols []string, keyFunc func(string) string) []string {
	keys := make([]string, len(cols))
	for i, col := range cols {
		keys[i] = keyFunc(col)
	}
	return keys
}
