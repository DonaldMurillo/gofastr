package crud

import "sync"

// rowPool caches pre-allocated map[string]any slices to reduce GC pressure
// on list endpoints. Each list request borrows from the pool, uses the
// maps during scan, encodes to JSON, then returns them.
//
// The pool is sized for typical page sizes (20-100 rows). Maps are cleared
// before return to avoid leaking references.
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

// borrowRowSlice gets a pre-allocated slice from the pool.
func borrowRowSlice() *[]map[string]any {
	return rowSlicePool.Get().(*[]map[string]any)
}

// returnRowSlice clears and returns the slice to the pool.
// Individual row maps within are also cleared and returned.
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

// borrowRowMap gets a pre-allocated map from the pool.
func borrowRowMap() *map[string]any {
	return rowMapPool.Get().(*map[string]any)
}

// returnRowMap clears and returns a map to the pool.
func returnRowMap(m *map[string]any) {
	for k := range *m {
		delete(*m, k)
	}
	rowMapPool.Put(m)
}

// ptrSlicePool caches []any slices used for scan pointers.
var ptrSlicePool = sync.Pool{
	New: func() any {
		s := make([]any, 0, 16)
		return &s
	},
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
	// Clear references to allow GC of scanned values
	for i := range *s {
		(*s)[i] = nil
	}
	*s = (*s)[:0]
	ptrSlicePool.Put(s)
}
