package framework

import (
	"net/http"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/a2a"
	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/router"
	"github.com/DonaldMurillo/gofastr/framework/access"
)

// The exchange keyed tasks by the owner id alone, so a customers:read
// token of a user listed the tasks that user's session ran, artifacts
// included (2026-10-27 A27). WithA2A passes the scopes the token holds
// (access.HeldScopes) to the server, which records them on each task.
func TestA2ANarrowTokenCannotListSessionTasks(t *testing.T) {
	installA2AOwnerExtractor(t)
	r := router.New()
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			ctx := req.Context()
			switch req.Header.Get("Authorization") {
			case "Bearer session-alice":
				ctx = handler.SetUser(ctx, &a2aTestUser{id: "u-alice"})
			case "Bearer token-alice":
				ctx = access.WithHeldScopes(handler.SetUser(ctx, &a2aTestUser{id: "u-alice"}), []string{"customers:read"})
			}
			next.ServeHTTP(w, req.WithContext(ctx))
		})
	})
	app := NewApp(WithDB(sqliteDB(t)), WithoutDefaultMiddleware(), WithRouter(r),
		WithA2A(A2AConfig{Skills: []a2a.Skill{a2aEchoSkill()}}))
	addr, _ := startOnRandomPort(t, app)
	env := &a2aTestEnv{app: app, addr: addr}

	code, body := env.a2aRequest(t, "session-alice",
		`{"jsonrpc":"2.0","id":1,"method":"SendMessage","params":{"message":{"messageId":"m1","role":"ROLE_USER","metadata":{"skill":"echo"},"parts":[{"text":"session secret"}]}}}`)
	if code != http.StatusOK || !strings.Contains(string(body), "TASK_STATE_COMPLETED") {
		t.Fatalf("session SendMessage: %d %s", code, body)
	}
	list := `{"jsonrpc":"2.0","id":2,"method":"ListTasks","params":{"includeArtifacts":true}}`
	if _, body := env.a2aRequest(t, "token-alice", list); strings.Contains(string(body), "session secret") {
		t.Errorf("SECURITY: a customers:read token listed the session's task: %s", body)
	}
	if _, body := env.a2aRequest(t, "session-alice", list); !strings.Contains(string(body), "session secret") {
		t.Errorf("the session no longer lists its own task: %s", body)
	}
}
