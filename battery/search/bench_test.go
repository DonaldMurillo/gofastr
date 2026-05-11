package search

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

// ============================================================================
// Tier 4 — in-memory search backend
// ============================================================================

// BenchmarkMemory_Index measures the cost of inserting one document into the
// memory backend at varying corpus sizes. The memory backend's documented
// growth is O(corpus); confirm.
func BenchmarkMemory_Index(b *testing.B) {
	for _, n := range []int{100, 1000, 10000} {
		n := n
		b.Run(fmt.Sprintf("corpus=%d", n), func(b *testing.B) {
			idx := NewMemory()
			ctx := context.Background()
			for i := 0; i < n; i++ {
				_ = idx.Index(ctx, Document{
					ID:   fmt.Sprintf("doc-%d", i),
					Type: "posts",
					Text: synthText(i),
				})
			}
			b.ResetTimer()
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				_ = idx.Index(ctx, Document{
					ID:   fmt.Sprintf("bench-%d", i),
					Type: "posts",
					Text: synthText(i + n),
				})
			}
		})
	}
}

// BenchmarkMemory_Search measures the cost of one query against varying
// corpus sizes. The memory backend scans linearly; expect O(corpus).
func BenchmarkMemory_Search(b *testing.B) {
	for _, n := range []int{100, 1000, 10000} {
		n := n
		b.Run(fmt.Sprintf("corpus=%d", n), func(b *testing.B) {
			idx := NewMemory()
			ctx := context.Background()
			for i := 0; i < n; i++ {
				_ = idx.Index(ctx, Document{
					ID:   fmt.Sprintf("doc-%d", i),
					Type: "posts",
					Text: synthText(i),
				})
			}
			q := Query{Text: "framework release", Type: "posts", Limit: 10}
			b.ResetTimer()
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				if _, err := idx.Search(ctx, q); err != nil {
					b.Fatalf("search: %v", err)
				}
			}
		})
	}
}

// synthText generates a deterministic body of ~80 chars with a stable token
// distribution. ID influences a few tokens so queries match different
// fractions of the corpus.
func synthText(i int) string {
	parts := []string{
		"go", "framework", "release", "notes", "performance", "indexing",
		"search", "query", "result", "documentation",
	}
	var b strings.Builder
	b.WriteString(parts[i%len(parts)])
	for j := 1; j < 8; j++ {
		b.WriteByte(' ')
		b.WriteString(parts[(i+j*7)%len(parts)])
	}
	b.WriteString(fmt.Sprintf(" id=%d", i))
	return b.String()
}
