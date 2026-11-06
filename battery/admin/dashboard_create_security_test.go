package admin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/access"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

func TestDashboardHonorsCreateGate(t *testing.T) {
	for _, scenario := range []string{"allowed", "denied", "owner_missing"} {
		t.Run(scenario, func(t *testing.T) {
			config := lockedPosts()
			if scenario == "owner_missing" {
				config.Scope = &entity.ScopeConfig{OwnerField: "owner_id"}
				config.Fields = append(config.Fields, schema.Field{Name: "owner_id", Type: schema.String})
			}
			world := setup(t, map[string]entity.EntityConfig{"posts": config}, Config{Entities: []string{"posts"}, DashboardNew: "posts"}, nil)
			caller := theAdmin
			if scenario == "owner_missing" {
				caller.id = ""
			}
			for _, path := range []string{"/admin", "/admin/_count/posts"} {
				request := httptest.NewRequest(http.MethodGet, path, nil)
				if scenario == "denied" {
					request = request.WithContext(access.WithDecider(request.Context(), func(_ context.Context, _ []string, permission access.Permission, _ access.Ref) access.Decision {
						if permission == "posts:write" {
							return access.DecisionDeny
						}
						return access.DecisionAbstain
					}))
				}
				response := serve(world.as(caller), request)
				found := strings.Contains(response.Body.String(), "/admin/entities/posts/create")
				if response.Code != http.StatusOK || found != (scenario == "allowed") {
					t.Errorf("%s: status %d, offers create=%v", path, response.Code, found)
				}
			}
		})
	}
}
