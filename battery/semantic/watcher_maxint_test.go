package semantic

import (
	"math"
	"strings"
	"testing"
)

// WatcherOptions.MaxFileSize = math.MaxInt64 reads as "no practical
// limit", but readLimited read through io.LimitReader(src, max+1) and the
// +1 wrapped to MinInt64: LimitReader read nothing, and every file was
// indexed as empty (not skipped: zero bytes is not past the limit). The
// limit now stays put at MaxInt64.
func TestReadLimitedAtMaxInt64ReadsTheFile(t *testing.T) {
	data, skip, err := readLimited(strings.NewReader("hello"), math.MaxInt64)
	if err != nil || skip || string(data) != "hello" {
		t.Fatalf("readLimited(MaxInt64) = %q, skip %v, err %v; want \"hello\"", data, skip, err)
	}
	// The sentinel byte still works below the top.
	if _, skip, _ := readLimited(strings.NewReader("hello"), 4); !skip {
		t.Fatal("readLimited(4) of 5 bytes did not report the overflow")
	}
}
