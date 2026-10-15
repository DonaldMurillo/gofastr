package semantic

import (
	"context"
	"math"
	"testing"
)

func TestFlatAddFailureLeavesStoreUnchanged(t *testing.T) {
	s := NewFlatStore(0, "model")
	err := s.Add(context.Background(), []Chunk{{ID: "a", DocID: "doc", Vec: []float32{1, 0}}, {ID: "b", DocID: "doc", Vec: []float32{1}}})
	if err == nil {
		t.Fatal("dimension mismatch accepted")
	}
	if got := s.Stats(); got.Chunks != 0 || got.Dim != 0 {
		t.Fatalf("failed batch changed store: %+v", got)
	}
}

func TestFlatStoreOwnsVectorsAndMetadata(t *testing.T) {
	s := NewFlatStore(2, "model")
	c := Chunk{ID: "a", DocID: "doc", Vec: []float32{1, 0}, Metadata: map[string]any{"kind": "code"}}
	if err := s.Add(context.Background(), []Chunk{c}); err != nil {
		t.Fatal(err)
	}
	c.Vec[0] = 0
	c.Metadata["kind"] = "changed"
	got, _ := s.ChunkByID("a")
	if got.Vec[0] != 1 || got.Metadata["kind"] != "code" {
		t.Errorf("caller altered stored chunk: %+v", got)
	}
	got.Vec[1] = 1
	got.Metadata["kind"] = "changed again"
	again, _ := s.ChunkByID("a")
	if again.Vec[1] != 0 || again.Metadata["kind"] != "code" {
		t.Errorf("reader altered stored chunk: %+v", again)
	}
}

func TestFlatStoreRejectsNonFiniteVectors(t *testing.T) {
	for _, v := range []float32{float32(math.NaN()), float32(math.Inf(1))} {
		s := NewFlatStore(2, "model")
		if err := s.Add(context.Background(), []Chunk{{ID: "a", Vec: []float32{v, 1}}}); err == nil {
			t.Errorf("accepted vector %v", v)
		}
	}
}

func TestNormalizeTinyVectorStaysFinite(t *testing.T) {
	v := []float32{math.SmallestNonzeroFloat32, 0}
	normalize(v)
	if v[0] != 1 || v[1] != 0 {
		t.Fatalf("tiny normalized vector corrupted: %v", v)
	}
}

func TestMetadataFilterSupportsStructuredValues(t *testing.T) {
	defer func() {
		if p := recover(); p != nil {
			t.Errorf("structured metadata comparison panicked: %v", p)
		}
	}()
	c := Chunk{Metadata: map[string]any{"tags": []any{"go", "test"}}}
	if !chunkMatches(&c, Filter{MetaMatch: map[string]any{"tags": []any{"go", "test"}}}) {
		t.Fatal("equal structured metadata did not match")
	}
}

func TestFixedWindowInvalidConfigReturnsError(t *testing.T) {
	for _, size := range []int{0, -1} {
		func() {
			defer func() {
				if p := recover(); p != nil {
					t.Errorf("size %d panicked: %v", size, p)
				}
			}()
			if _, err := (&FixedWindow{Size: size}).Chunk(Document{ID: "doc", Text: "text"}); err == nil {
				t.Errorf("invalid window size %d accepted", size)
			}
		}()
	}
}
