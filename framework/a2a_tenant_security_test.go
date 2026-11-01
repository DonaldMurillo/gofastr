package framework

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/a2a"
	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/router"
	"github.com/DonaldMurillo/gofastr/framework/tenant"
)

// A2A tasks were bound to the user id alone, so a member who left tenant A,
// or switched to B, still listed and read the rows A's tasks returned
// (2026-10-27 P14). WithA2A passes the resolved tenant to the server,
// which records it on each task.
func TestA2ATaskStaysInItsTenant(t *testing.T) {
	installA2AOwnerExtractor(t)
	r := router.New()
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			ctx := handler.SetUser(req.Context(), &a2aTestUser{id: "u-alice"})
			// Stands in for a server-side active-tenant lookup.
			if id := req.Header.Get("X-Test-Active-Tenant"); id != "" {
				ctx = tenant.SetTenantID(ctx, id)
			}
			next.ServeHTTP(w, req.WithContext(ctx))
		})
	})
	app := NewApp(WithDB(sqliteDB(t)), WithoutDefaultMiddleware(), WithRouter(r),
		WithA2A(A2AConfig{Skills: []a2a.Skill{a2aEchoSkill()}}))
	addr, _ := startOnRandomPort(t, app)
	call := func(tenantID, payload string) string {
		t.Helper()
		req, err := http.NewRequest(http.MethodPost, "http://"+addr+"/a2a", strings.NewReader(payload))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer session-alice")
		req.Header.Set("X-Test-Active-Tenant", tenantID)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = resp.Body.Close() }()
		body, _ := io.ReadAll(resp.Body)
		return string(body)
	}

	if body := call("A", `{"jsonrpc":"2.0","id":1,"method":"SendMessage","params":{"message":{"messageId":"m1","role":"ROLE_USER","metadata":{"skill":"echo"},"parts":[{"text":"tenant A secret"}]}}}`); !strings.Contains(body, "TASK_STATE_COMPLETED") {
		t.Fatalf("tenant A SendMessage: %s", body)
	}
	list := `{"jsonrpc":"2.0","id":2,"method":"ListTasks","params":{"includeArtifacts":true}}`
	if body := call("B", list); strings.Contains(body, "tenant A secret") {
		t.Errorf("SECURITY: the user listed tenant A's task from tenant B: %s", body)
	}
	if body := call("A", list); !strings.Contains(body, "tenant A secret") {
		t.Errorf("tenant A no longer lists its own task: %s", body)
	}
}
