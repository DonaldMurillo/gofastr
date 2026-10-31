package crud

import (
	"context"
	"testing"
)

// An in-process include filter is read as written. The names were pasted
// into a synthetic request line unescaped, so a space split the line and
// httptest.NewRequest panicked, a + became a space and a # cut the rest
// off (2026-10-27 bug bash D10).
func TestInProcessIncludeFilterVerbatim(t *testing.T) {
	ch, db, _ := covRelWorld(t)
	seedRows(t, db, "comments", []map[string]any{
		{"id": "c3", "post_id": "p2", "body": "hello world"},
		{"id": "c4", "post_id": "p2", "body": "a+b"},
		{"id": "c5", "post_id": "p2", "body": "x#y"},
		{"id": "c6", "post_id": "p2", "body": "a b"},
	})
	for _, body := range []string{"hello world", "a+b", "x#y"} {
		t.Run(body, func(t *testing.T) {
			include := "comments(body=" + body + ")"
			rows, err := ch.ListAll(context.Background(), ListOptions{Includes: []string{include}})
			if err != nil {
				t.Fatalf("ListAll include %s: %v", include, err)
			}
			for _, row := range rows {
				if row["id"] != "p2" {
					continue
				}
				comments, _ := row["comments"].([]map[string]any)
				if len(comments) != 1 || comments[0]["body"] != body {
					t.Fatalf("include %s attached %v, want the one comment %q", include, comments, body)
				}
				return
			}
			t.Fatal("p2 missing")
		})
	}
	got, err := ch.GetOne(context.Background(), "p2", []string{"comments(body=hello world)"})
	if err != nil {
		t.Fatalf("GetOne: %v", err)
	}
	if comments, _ := got["comments"].([]map[string]any); len(comments) != 1 {
		t.Fatalf("GetOne attached %v, want one comment", comments)
	}
	type post struct {
		ID string `json:"id"`
	}
	if _, err := NewTypedQuery[post](ch).Include("comments(body=hello world)").Find(context.Background()); err != nil {
		t.Fatalf("TypedQuery Include: %v", err)
	}
}
