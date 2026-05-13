package embed

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

// ============================================================================
// Tier 5 — battery/embed: in-memory flat-cosine retrieval
// ============================================================================
//
// The flat store is documented as brute-force O(corpus) at query time,
// with insertion O(1) per chunk. These benchmarks confirm growth shape
// and let later optimizations (SIMD dot, int8 quantization, ANN
// indexes) be evaluated against a stable baseline.

func benchSetup(b *testing.B, n int, hybrid bool) (Index, context.Context) {
	b.Helper()
	ctx := context.Background()
	opts := Options{Embedder: NewStubEmbedder(128)}
	if hybrid {
		opts.Keyword = NewMemoryKeyword()
	}
	idx, err := Open(opts)
	if err != nil {
		b.Fatalf("Open: %v", err)
	}
	for i := 0; i < n; i++ {
		err := idx.Add(ctx, Document{
			ID:   fmt.Sprintf("doc-%d", i),
			Text: synthChunk(i),
		})
		if err != nil {
			b.Fatalf("Add: %v", err)
		}
	}
	return idx, ctx
}

// synthChunk returns a short, lexically varied paragraph derived from
// i so different docs have meaningfully different token bags.
func synthChunk(i int) string {
	roots := []string{"router", "auth", "cache", "storage", "queue", "search", "embed"}
	verbs := []string{"handles", "validates", "stores", "fetches", "emits", "indexes"}
	nouns := []string{"requests", "tokens", "sessions", "messages", "documents", "queries"}
	r := roots[i%len(roots)]
	v := verbs[(i/3)%len(verbs)]
	n := nouns[(i/5)%len(nouns)]
	return fmt.Sprintf("the %s battery %s %d %s for gofastr applications and clients", r, v, i, n)
}

func BenchmarkEmbed_Add(b *testing.B) {
	for _, n := range []int{100, 1000, 10000} {
		n := n
		b.Run(fmt.Sprintf("corpus=%d", n), func(b *testing.B) {
			idx, ctx := benchSetup(b, n, false)
			b.ResetTimer()
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				_ = idx.Add(ctx, Document{
					ID:   fmt.Sprintf("bench-%d", i+n),
					Text: synthChunk(i + n),
				})
			}
		})
	}
}

func BenchmarkEmbed_Query_VecOnly(b *testing.B) {
	for _, n := range []int{100, 1000, 10000} {
		n := n
		b.Run(fmt.Sprintf("corpus=%d", n), func(b *testing.B) {
			idx, ctx := benchSetup(b, n, false)
			q := Query{Text: "router middleware request", K: 10}
			b.ResetTimer()
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				if _, err := idx.Query(ctx, q); err != nil {
					b.Fatalf("Query: %v", err)
				}
			}
		})
	}
}

func BenchmarkEmbed_Query_Hybrid(b *testing.B) {
	for _, n := range []int{100, 1000, 10000} {
		n := n
		b.Run(fmt.Sprintf("corpus=%d", n), func(b *testing.B) {
			idx, ctx := benchSetup(b, n, true)
			q := Query{Text: "router middleware request", K: 10, Hybrid: true}
			b.ResetTimer()
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				if _, err := idx.Query(ctx, q); err != nil {
					b.Fatalf("Query: %v", err)
				}
			}
		})
	}
}

func BenchmarkEmbed_Query_MMR(b *testing.B) {
	for _, n := range []int{1000, 10000} {
		n := n
		b.Run(fmt.Sprintf("corpus=%d", n), func(b *testing.B) {
			idx, ctx := benchSetup(b, n, true)
			q := Query{Text: "router middleware request", K: 10, Hybrid: true, MMRLambda: 0.4}
			b.ResetTimer()
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				if _, err := idx.Query(ctx, q); err != nil {
					b.Fatalf("Query: %v", err)
				}
			}
		})
	}
}

func BenchmarkEmbed_Stub_Embed(b *testing.B) {
	emb := NewStubEmbedder(128)
	ctx := context.Background()
	text := strings.Repeat("alpha bravo charlie delta echo ", 16)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, _ = emb.Embed(ctx, []string{text})
	}
}
