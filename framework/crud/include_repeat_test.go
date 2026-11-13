package crud

import (
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// ?include=author&include=bogus answered 200 with author loaded: the
// parser read only the first include value, so a later one was never
// validated or applied, while ?include=bogus alone answered 400. A
// repeated include key is refused, as a repeated scalar filter is; one
// include value takes a comma-separated list. List and read-one both
// validate controls at readQuery before resolving include paths.
func TestRepeatedIncludeKeyIsRefused(t *testing.T) {
	posts := entity.Define("posts", entity.EntityConfig{
		Name: "posts", Table: "posts",
		Fields:    []schema.Field{{Name: "title", Type: schema.String}},
		Relations: []entity.Relation{entity.BelongsTo("author", "users", "author_id")},
	}.WithTimestamps(false))
	users := entity.Define("users", entity.EntityConfig{
		Name: "users", Table: "users",
		Fields: []schema.Field{{Name: "name", Type: schema.String}},
	}.WithTimestamps(false))
	reg := stubRegistry{byName: map[string]*entity.Entity{"posts": posts, "users": users}}

	ch := NewCrudHandler(posts, nil)
	for _, q := range []url.Values{
		{"include": {"author", "bogus"}},
		{"include": {"author", "author"}},
		{"include": {"", "bogus"}},
	} {
		_, err := ch.readQuery(httptest.NewRequest("GET", "/posts?"+q.Encode(), nil))
		if err == nil || !strings.Contains(err.Error(), "include is sent") {
			t.Errorf("%v: err = %v, want the repeated-include refusal", q, err)
		}
	}
	if _, err := parseIncludeTreeQ(url.Values{"include": {"author"}}, posts, reg); err != nil {
		t.Errorf("one include value: %v", err)
	}
}
