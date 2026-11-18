package dev

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/router"
)

func setDevEnv(t *testing.T, kv map[string]string) {
	t.Helper()
	for _, k := range []string{"GOFASTR_ENV", "GOFASTR_DEV", "GOFASTR_DEV_LIVERELOAD", "GOFASTR_DEV_MCP", "GOFASTR_DEV_LAYOUT"} {
		t.Setenv(k, kv[k])
	}
}

func TestEnabledRefusesProductionEnv(t *testing.T) {
	setDevEnv(t, map[string]string{"GOFASTR_DEV": "1", "GOFASTR_ENV": "Prod"})
	if Enabled() {
		t.Fatal("Enabled is true with GOFASTR_ENV=Prod")
	}
	setDevEnv(t, map[string]string{"GOFASTR_DEV": "1"})
	if !Enabled() {
		t.Fatal("Enabled is false under GOFASTR_DEV=1")
	}
}

func TestDevMCPHasItsOwnOptOut(t *testing.T) {
	setDevEnv(t, map[string]string{"GOFASTR_DEV": "1", "GOFASTR_DEV_LIVERELOAD": "0"})
	if !DevMCPEnabled() {
		t.Error("GOFASTR_DEV_LIVERELOAD=0 turned the dev MCP off")
	}
	setDevEnv(t, map[string]string{"GOFASTR_DEV": "1", "GOFASTR_DEV_MCP": "0"})
	if DevMCPEnabled() {
		t.Error("GOFASTR_DEV_MCP=0 left the dev MCP on")
	}
}

func readyData(t *testing.T, r *router.Router) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, LiveReloadStreamURL, nil).WithContext(ctx))
	for _, line := range strings.Split(rec.Body.String(), "\n") {
		if d, ok := strings.CutPrefix(line, "data: "); ok {
			return d
		}
	}
	return ""
}

func TestReadyCarriesBuildID(t *testing.T) {
	a, b := router.New(), router.New()
	RegisterLiveReload(a)
	time.Sleep(time.Millisecond)
	RegisterLiveReload(b)
	ida, idb := readyData(t, a), readyData(t, b)
	if ida == "" || idb == "" || ida == idb {
		t.Fatalf("build IDs %q and %q, want non-empty and distinct per registration", ida, idb)
	}
}

func TestInjectorIdentityEncodingAndLastBody(t *testing.T) {
	rec := injected(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Header().Set("Content-Encoding", "identity")
		_, _ = w.Write([]byte("<body><pre>&lt;/body&gt; </body></pre></body>"))
	})
	body := rec.Body.String()
	if !strings.HasSuffix(body, `defer></script></body>`) {
		t.Fatalf("script not spliced before the last </body> of an identity-encoded page:\n%s", body)
	}
}
