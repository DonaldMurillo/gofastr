package admin

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/access"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

func TestPaletteHonorsCreateGate(t *testing.T) {
	x := setup(t, map[string]entity.EntityConfig{"posts": lockedPosts()}, Config{Entities: []string{"posts"}}, nil)
	for _, mode := range []string{"allowed", "decider"} {
		t.Run(mode, func(t *testing.T) {
			var handler http.Handler = x.as(theAdmin)
			request := newPost("/admin/_palette", url.Values{"q": {""}})
			if mode == "decider" {
				request = request.WithContext(access.WithDecider(request.Context(), func(_ context.Context, _ []string, permission access.Permission, _ access.Ref) access.Decision {
					if permission == "posts:write" {
						return access.DecisionDeny
					}
					return access.DecisionAbstain
				}))
			}
			response := serve(handler, request)
			if response.Code != http.StatusOK {
				t.Fatalf("palette status %d: %s", response.Code, response.Body.String())
			}
			found := strings.Contains(response.Body.String(), "/admin/entities/posts/create")
			if found != (mode == "allowed") {
				t.Errorf("%s palette offers create=%v", mode, found)
			}
		})
	}
}
