package sdkdocs

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/entity"
	"golang.org/x/net/html"
)

func TestReadOnlyReferenceOmitsWrites(t *testing.T) {
	for _, mode := range []string{"readonly", "writable", "legacy"} {
		t.Run(mode, func(t *testing.T) {
			reg := testRegistry()
			posts := reg.entities[1]
			posts.Config.States = &entity.StatesConfig{Field: "status", Initial: []string{"draft"}, Transitions: []entity.Transition{
				{Key: "publish", From: []string{"draft"}, To: "live"},
			}}
			posts.Config.Endpoints = []entity.Endpoint{{Method: "POST", Path: "refresh", Description: "Refresh snapshot"}}
			cfg := Config{Registry: reg, CRUDMounted: func(*entity.Entity) bool { return true }}
			if mode != "legacy" {
				cfg.ReadOnlyMounted = func(*entity.Entity) bool { return mode == "readonly" }
			}
			srv := mountedServer(t, cfg)
			resp, body := get(t, srv, "/docs/api/entities/posts")
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("reference status = %d", resp.StatusCode)
			}
			var rendered strings.Builder
			tokens := html.NewTokenizer(strings.NewReader(body))
			for tokens.Next() != html.ErrorToken {
				token := tokens.Token()
				if token.Type == html.TextToken {
					rendered.WriteString(token.Data)
				}
			}
			if tokens.Err() != io.EOF {
				t.Fatal(tokens.Err())
			}
			body = rendered.String()
			for _, snippet := range []string{"Get one record", "List (offset or cursor pagination)", "/posts/_events", "c.ListPosts", "api.posts.list", "Refresh snapshot"} {
				if !strings.Contains(body, snippet) {
					t.Errorf("reference omitted %q", snippet)
				}
			}
			for _, snippet := range []string{"Full update", "Partial update", "Atomic batch", "/posts/_batch", "/transitions/publish", "c.CreatePosts", "api.posts.create"} {
				if got := strings.Contains(body, snippet); got != (mode != "readonly") {
					t.Errorf("reference contains %q = %v, readonly = %v", snippet, got, mode == "readonly")
				}
			}
		})
	}
}
