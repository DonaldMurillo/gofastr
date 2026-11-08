package framework

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDevMCPExposeAcceptsRemoteHost(t *testing.T) {
	t.Setenv("GOFASTR_DEV", "1")
	t.Setenv("GOFASTR_ENV", "development")
	t.Setenv("GOFASTR_DEV_MCP", "")
	for _, expose := range []string{"", "1"} {
		t.Setenv("GOFASTR_DEV_MCP_EXPOSE", expose)
		for _, addr := range []string{"0.0.0.0:8080", "127.0.0.1:8080"} {
			app := NewApp()
			app.guardDevMCPBind(addr, "tcp")
			if err := app.InitPlugins(); err != nil {
				t.Fatal(err)
			}
			for _, host := range []string{"192.168.50.10:8080", "dev.example.test:8080"} {
				req := httptest.NewRequest(http.MethodPost, "http://"+host+"/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`))
				req.Header.Set("Content-Type", "application/json")
				response := httptest.NewRecorder()
				app.MCP.ServeHTTP(response, req)
				want := http.StatusForbidden
				if expose == "1" && addr == "0.0.0.0:8080" {
					want = http.StatusOK
				}
				if response.Code != want {
					t.Errorf("expose=%q addr=%s host=%s: status=%d want=%d body=%s", expose, addr, host, response.Code, want, response.Body.String())
				}
			}
		}
	}
}
