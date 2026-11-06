package framework

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	uiapp "github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/framework/uihost"
)

func TestLLMsLinksMountedManifest(t *testing.T) {
	for _, automatic := range []bool{false, true} {
		name := "manual"
		if automatic {
			name = "automatic"
		}
		t.Run(name, func(t *testing.T) {
			host := uihost.New(uiapp.NewApp("manifest"), uihost.WithAgentReady(uihost.AgentReadyConfig{
				Title: "Manifest", AgentCard: &uihost.AgentCardConfig{Name: "Manifest", MCPEndpoint: "/mcp"},
			}))
			opts := []AppOption{WithConfig(AppConfig{Name: "manifest"}), WithSecret("manifest-test-secret-with-32-bytes")}
			if automatic {
				opts = append(opts, WithMCP())
			}
			app := NewUIHostApp(host, opts...)
			if !automatic {
				app.Router().Get("/mcp", http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) { writer.WriteHeader(http.StatusOK) }))
			}
			started, cleanup := startApp(t, app)
			defer cleanup()
			get := func(path string) *httptest.ResponseRecorder {
				recorder := httptest.NewRecorder()
				started.router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))
				return recorder
			}
			body := get("/llms.txt").Body.String()
			if !strings.Contains(body, "[MCP endpoint](/mcp)") {
				t.Fatalf("MCP endpoint lost: %s", body)
			}
			linked := strings.Contains(body, "[MCP manifest]")
			mounted := get("/.well-known/mcp.json").Code == http.StatusOK
			if linked != mounted || mounted != automatic {
				t.Errorf("manifest linked=%t mounted=%t automatic=%t", linked, mounted, automatic)
			}
		})
	}
}
