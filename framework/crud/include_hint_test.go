package crud

import (
	"net/url"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// #503: ?include=issue answered 400 "segment "issue" has no relation"
// on an entity whose relation is named issue_id, and the caller had to
// guess the spelling. A near miss names the relation; anything else
// lists nothing, so an unrelated guess learns no schema.
func TestIncludeNearMissNamesRelation(t *testing.T) {
	comments := entity.Define("comments", entity.EntityConfig{
		Name: "comments", Table: "comments",
		Fields: []schema.Field{{Name: "body", Type: schema.Text}},
		Relations: []entity.Relation{
			entity.BelongsTo("issue_id", "issues", "issue_id"),
			entity.BelongsTo("author", "users", "author_id"),
		},
	}.WithTimestamps(false))
	reg := stubRegistry{byName: map[string]*entity.Entity{"comments": comments}}

	for include, want := range map[string]string{
		"issue":  `did you mean "issue_id"?`,
		"issues": `did you mean "issue_id"?`,
		"user":   `did you mean "author"?`,
		"users":  `did you mean "author"?`,
	} {
		_, err := parseIncludeTreeQ(url.Values{"include": {include}}, comments, reg)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("include=%s: err = %v, want it to contain %s", include, err, want)
		}
	}

	_, err := parseIncludeTreeQ(url.Values{"include": {"xyz"}}, comments, reg)
	if err == nil || strings.Contains(err.Error(), "did you mean") {
		t.Errorf("include=xyz: err = %v, want an unknown-include error with no suggestion", err)
	}
}
