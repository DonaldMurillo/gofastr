package crud

import (
	"database/sql"
	"sync"
)

// maxPooledMapEntries caps the size of pooled maps and slices. Entries
// larger than this are dropped on return so a single oversized request
// can't pin a giant allocation in the pool forever (mirrors
// uihost/builder_pool.go's 64KB builder cap).
const maxPooledMapEntries = 4096

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
// Oversized maps and the slice itself are dropped instead of pooled so a
// pathological row size can't pin huge allocations in the pool forever.
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

// returnPtrSlice returns a pointer slice to the pool. Oversized slices are
// dropped rather than retained so the pool's high-water mark stays bounded.
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

// scanRowsPooled scans rows using pooled maps to reduce allocations.
// The caller must call returnRowSlice on the result after encoding.
func scanRowsPooled(rows *sql.Rows, cols []string, keyFunc func(string) string) (*[]map[string]any, error) {
	return scanRowsPooledWithKeys(rows, cols, convertedKeys(cols, keyFunc))
}

func scanRowsPooledWithKeys(rows *sql.Rows, cols, keys []string) (*[]map[string]any, error) {
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
		for i := range cols {
			row[keys[i]] = convertValue(values[i])
		}
		*results = append(*results, row)
		returnPtrSlice(ptrs)
	}
	// A false rows.Next() can be a mid-iteration error, not just EOF. Propagate
	// it rather than returning a truncated result set as success (mirrors
	// scanRowsWithKeys and the eager loaders). Return the borrowed slice first.
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
