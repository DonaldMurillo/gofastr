package semantic

import (
	"context"
	"hash/fnv"
	"math"
	"strings"
	"unicode"
)

// StubEmbedder is a deterministic hashed bag-of-words Embedder for tests and
// offline development.
type StubEmbedder struct {
	dim int
}

// NewStubEmbedder returns a StubEmbedder of dimension dim, 128 when dim <= 0.
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
		sign := float32(1)
		if (sum>>32)&1 == 1 {
			sign = -1
		}
		v[idx] += sign
	}
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
