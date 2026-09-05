package framework

import (
	"net/http"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/a2a"
)

// Pins the framework-level passthrough for the A2A cross-origin gate
// added by the 2026-09-04 red-probe round (core/a2a refuses a present
// Origin naming a foreign authority, like the MCP transport). Without
// A2AConfig.AllowedOrigins a host behind a tunnel could not permit its
// own browser clients; with it, only the listed Origins pass.
// Property: a browser Origin naming an authority other than the
// request's Host is refused before dispatch unless the host listed it.
// Surfaces: framework/a2a.go WithA2A -> a2a.Config.AllowedOrigins.
func TestA2AAllowedOriginsPassthrough(t *testing.T) {
	env := newA2ATestEnv(t, A2AConfig{
		Skills:         []a2a.Skill{a2aEchoSkill()},
		AllowedOrigins: []string{"https://tunnel.example"},
	})
	payload := `{"jsonrpc":"2.0","id":1,"method":"SendMessage","params":{"message":{"messageId":"m1","role":"ROLE_USER","metadata":{"skill":"echo"},"parts":[{"text":"hi"}]}}}`
	post := func(origin string) int {
		t.Helper()
		req, err := http.NewRequest(http.MethodPost, "http://"+env.addr+"/a2a", strings.NewReader(payload))
		if err != nil {
			t.Fatalf("build request: %v", err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer tok-alice")
		req.Header.Set("Origin", origin)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("POST /a2a: %v", err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}
	if got := post("https://tunnel.example"); got != http.StatusOK {
		t.Fatalf("listed Origin: got %d, want 200 (A2AConfig.AllowedOrigins not forwarded)", got)
	}
	if got := post("https://evil.example"); got != http.StatusForbidden {
		t.Fatalf("foreign Origin: got %d, want 403", got)
	}
}
