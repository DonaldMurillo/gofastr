package entityui

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"unicode/utf8"
)

// pgTextViews answers text Postgres cannot store the way Postgres does:
// a driver error (22021), which the handler would map to a 500.
type pgTextViews struct{ *memSavedViews }

var errPG22021 = errors.New("pq: invalid byte sequence for encoding \"UTF8\" (22021)")

func unstorable(s string) bool { return !utf8.ValidString(s) || strings.IndexByte(s, 0) >= 0 }

func (p pgTextViews) Create(ctx context.Context, v SavedView) (SavedView, error) {
	if unstorable(v.Name) || unstorable(v.Filter) {
		return SavedView{}, errPG22021
	}
	return p.memSavedViews.Create(ctx, v)
}

func (p pgTextViews) Delete(ctx context.Context, entity, id string) error {
	if unstorable(id) {
		return errPG22021
	}
	return p.memSavedViews.Delete(ctx, entity, id)
}

func TestSavedViewUnstorableTextIs400(t *testing.T) {
	x, store := savedUI(t)
	x.ui = x.ui.WithSavedViews(pgTextViews{store})
	for _, form := range []url.Values{
		{"name": {"a\x00b"}, "back": {"/orders"}},
		{"name": {"bad \xff"}, "back": {"/orders"}},
		{"name": {"ok"}, "filter": {"status = \"a\x00b\""}, "back": {"/orders"}},
	} {
		if w := postSavedViews(t, x, "u1", "/api/orders/_views", form); w.Code != http.StatusBadRequest {
			t.Errorf("save %q answered %d, want 400", form.Encode(), w.Code)
		}
	}
	if w := postSavedViews(t, x, "u1", "/api/orders/_views/_delete?id=a%00b", url.Values{"back": {"/orders"}}); w.Code != http.StatusNotFound {
		t.Errorf("delete of an unstorable id answered %d, want 404", w.Code)
	}
}
