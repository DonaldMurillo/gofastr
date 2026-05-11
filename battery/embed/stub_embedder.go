package embed

import (
	"context"
	"hash/fnv"
	"math"
	"strings"
	"unicode"
)

// StubEmbedder is a deterministic, dependency-free [Embedder] used by
// tests and offline development. It produces a hashed bag-of-words
// representation: each whitespace-separated token is hashed into one of
// Dim buckets with a sign derived from a second hash, and the result is
// L2-normalized.
//
// It is NOT a real embedding model — there is no semantic similarity
// across paraphrases or synonyms — but it is fast, allocation-light,
// and produces high cosine similarity for documents that share tokens,
// which is enough to test the retrieval pipeline end-to-end.
type StubEmbedder struct {
	dim int
}

// NewStubEmbedder returns a StubEmbedder of the given dimension.
// Dimensions <= 0 fall back to 128.
func NewStubEmbedder(dim int) *StubEmbedder {
	if dim <= 0 {
		dim = 128
	}
	return &StubEmbedder{dim: dim}
}

// Embed implements [Embedder].
func (s *StubEmbedder) Embed(_ context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i, t := range texts {
		out[i] = s.encode(t)
	}
	return out, nil
}

// Dim implements [Embedder].
func (s *StubEmbedder) Dim() int { return s.dim }

// Name implements [Embedder].
func (s *StubEmbedder) Name() string { return "stub-fnv-bow" }

func (s *StubEmbedder) encode(text string) []float32 {
	v := make([]float32, s.dim)
	lower := strings.ToLower(text)
	for _, tok := range tokenize(lower) {
		h := fnv.New64a()
		h.Write([]byte(tok))
		sum := h.Sum64()
		idx := int(sum % uint64(s.dim))
		// second hash for the sign so different tokens partially cancel
		// instead of always reinforcing the same bucket.
		sign := float32(1)
		if (sum>>32)&1 == 1 {
			sign = -1
		}
		v[idx] += sign
	}
	// L2 normalize so cosine similarity = dot product.
	var sumsq float64
	for _, x := range v {
		sumsq += float64(x) * float64(x)
	}
	if sumsq > 0 {
		inv := float32(1.0 / math.Sqrt(sumsq))
		for i := range v {
			v[i] *= inv
		}
	}
	return v
}

func tokenize(s string) []string {
	return strings.FieldsFunc(s, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
}

var _ Embedder = (*StubEmbedder)(nil)
