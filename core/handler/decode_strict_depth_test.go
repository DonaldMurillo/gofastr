package handler

import (
	"runtime"
	"strings"
	"testing"
)

// nestedArrays is {"x": followed by depth nested arrays.
func nestedArrays(depth int) []byte {
	return []byte(`{"x":` + strings.Repeat("[", depth) + strings.Repeat("]", depth) + `}`)
}

func walkBytes(t *testing.T, body []byte) uint64 {
	t.Helper()
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	if err := CheckObjectKeys(body, FoldJSONKey); err != nil {
		t.Fatalf("CheckObjectKeys: %v", err)
	}
	runtime.ReadMemStats(&after)
	return after.TotalAlloc - before.TotalAlloc
}

// TestCheckObjectKeysDeepNestingIsLinear: the key walk Bind, UnmarshalStrict
// and ToolAdapter share keeps one path stack. Rebuilding the path per level
// allocated about 350 MB for this 8 KiB body; one stack stays under 1 MB.
func TestCheckObjectKeysDeepNestingIsLinear(t *testing.T) {
	small := walkBytes(t, nestedArrays(2000))
	large := walkBytes(t, nestedArrays(4000))
	t.Logf("depth 2000: %d bytes, depth 4000: %d bytes", small, large)
	if large > 8<<20 {
		t.Fatalf("an 8 KiB body of 4000 nested arrays allocated %d bytes in the key walk; the path is copied per level", large)
	}
}

func BenchmarkCheckObjectKeysDeepNesting(b *testing.B) {
	body := nestedArrays(4000)
	b.ReportAllocs()
	for b.Loop() {
		_ = CheckObjectKeys(body, FoldJSONKey)
	}
}
