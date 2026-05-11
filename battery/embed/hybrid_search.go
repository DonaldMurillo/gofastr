package embed

import (
	"context"

	"github.com/gofastr/gofastr/battery/search"
)

// WrapSearchBackend adapts a [battery/search].Backend to the
// [KeywordBackend] interface this package expects. It is intentionally
// thin: every Add/Remove writes through, and Search maps Documents
// back to chunk IDs.
func WrapSearchBackend(b search.Backend) KeywordBackend {
	return &searchAdapter{b: b}
}

type searchAdapter struct {
	b search.Backend
}

func (a *searchAdapter) Index(ctx context.Context, id, text string) error {
	return a.b.Index(ctx, search.Document{ID: id, Type: "embed.chunk", Text: text})
}

func (a *searchAdapter) Delete(ctx context.Context, id string) error {
	return a.b.Delete(ctx, id)
}

func (a *searchAdapter) Search(ctx context.Context, text string, top int) ([]KeywordHit, error) {
	results, err := a.b.Search(ctx, search.Query{Text: text, Type: "embed.chunk", Limit: top})
	if err != nil {
		return nil, err
	}
	out := make([]KeywordHit, len(results))
	for i, r := range results {
		out[i] = KeywordHit{ChunkID: r.Document.ID, Score: r.Score}
	}
	return out, nil
}
