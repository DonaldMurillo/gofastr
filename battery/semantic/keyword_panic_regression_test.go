package semantic

import (
	"context"
	"testing"
)

type panickySearch struct{ *MemoryKeyword }

func (panickySearch) Search(context.Context, string, int) ([]KeywordHit, error) {
	panic("backend bug")
}

// A host keyword backend that panics in Search fails the query with an
// error, as its Index and Delete panics already do.
func TestKeywordSearchPanicIsAnError(t *testing.T) {
	idx, err := Open(Options{Embedder: NewStubEmbedder(4), Keyword: panickySearch{NewMemoryKeyword()}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { idx.Close() })
	if _, err := idx.Query(context.Background(), Query{Text: "x", Hybrid: true}); err == nil {
		t.Fatal("panicking keyword search returned no error")
	}
}
