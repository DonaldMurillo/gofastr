package crud

import (
	"database/sql"
	"sync"
)

// rowSlicePool caches pre-allocated []map[string]any slices.
var rowSlicePool = sync.Pool{
	New: func() any {
		s := make([]map[string]any, 0, 32)
		return &s
	},
}

// rowMapPool caches individual row maps.
var rowMapPool = sync.Pool{
	New: func() any {
		m := make(map[string]any, 16)
		return &m
	},
}

// ptrSlicePool caches []any slices used for scan pointers.
var ptrSlicePool = sync.Pool{
	New: func() any {
		s := make([]any, 0, 16)
		return &s
	},
}

// borrowRowSlice gets a pre-allocated slice from the pool.
func borrowRowSlice() *[]map[string]any {
	return rowSlicePool.Get().(*[]map[string]any)
}

// returnRowSlice clears and returns the slice to the pool.
// Individual row maps within are also cleared and returned to rowMapPool.
func returnRowSlice(s *[]map[string]any) {
	for i := range *s {
		m := (*s)[i]
		for k := range m {
			delete(m, k)
		}
		rowMapPool.Put(&m)
		(*s)[i] = nil
	}
	*s = (*s)[:0]
	rowSlicePool.Put(s)
}

// borrowPtrSlice gets a pre-allocated []any for scan pointers.
func borrowPtrSlice(n int) *[]any {
	s := ptrSlicePool.Get().(*[]any)
	if cap(*s) < n {
		*s = make([]any, n)
	} else {
		*s = (*s)[:n]
	}
	return s
}

// returnPtrSlice returns a pointer slice to the pool.
func returnPtrSlice(s *[]any) {
	for i := range *s {
		(*s)[i] = nil
	}
	*s = (*s)[:0]
	ptrSlicePool.Put(s)
}

// scanRowsPooled scans rows using pooled maps to reduce allocations.
// The caller must call returnRowSlice on the result after encoding.
func scanRowsPooled(rows *sql.Rows, cols []string, keyFunc func(string) string) (*[]map[string]any, error) {
	results := borrowRowSlice()
	for rows.Next() {
		ptrs := borrowPtrSlice(len(cols))
		values := make([]any, len(cols))
		for i := range values {
			(*ptrs)[i] = &values[i]
		}
		if err := rows.Scan(*ptrs...); err != nil {
			returnPtrSlice(ptrs)
			returnRowSlice(results)
			return nil, err
		}
		// Use pooled map instead of make
		rowPtr := rowMapPool.Get().(*map[string]any)
		row := *rowPtr
		for i, col := range cols {
			row[keyFunc(col)] = convertValue(values[i])
		}
		*results = append(*results, row)
		returnPtrSlice(ptrs)
	}
	return results, nil
}
