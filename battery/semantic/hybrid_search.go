package semantic

import (
	"context"
	"fmt"

	"github.com/DonaldMurillo/gofastr/battery/search"
)

// WrapSearchBackend adapts a battery/search Backend to KeywordBackend.
func WrapSearchBackend(b search.Backend) KeywordBackend {
	return &searchAdapter{b: b}
}

type searchAdapter struct {
	b search.Backend
}

func (a *searchAdapter) Index(ctx context.Context, id, text string) error {
	return a.IndexOwned(ctx, id, "", text)
}

// IndexOwned indexes a chunk owned by owner, so the search store's eraser
// deletes it with the user.
func (a *searchAdapter) IndexOwned(ctx context.Context, id, owner, text string) (err error) {
	if err := validateDocumentText(Document{ID: id, Owner: owner, Text: text}); err != nil {
		return err
	}
	defer func() {
		if v := recover(); v != nil {
			err = fmt.Errorf("semantic: keyword backend panicked: %v", v)
		}
	}()
	return a.b.Index(ctx, search.Document{ID: id, Type: "embed.chunk", Text: text, Owner: owner})
}

func (a *searchAdapter) Delete(ctx context.Context, id string) (err error) {
	defer func() {
		if v := recover(); v != nil {
			err = fmt.Errorf("semantic: keyword backend panicked: %v", v)
		}
	}()
	return a.b.Delete(ctx, id)
}

func (a *searchAdapter) Search(ctx context.Context, text string, top int) (out []KeywordHit, err error) {
	defer func() {
		if v := recover(); v != nil {
			out = nil
			err = fmt.Errorf("semantic: keyword backend panicked: %v", v)
		}
	}()
	results, err := a.b.Search(ctx, search.Query{Text: text, Type: "embed.chunk", Limit: top})
	if err != nil {
		return nil, err
	}
	out = make([]KeywordHit, len(results))
	for i, r := range results {
		out[i] = KeywordHit{ChunkID: r.Document.ID, Score: r.Score}
	}
	return out, nil
}
