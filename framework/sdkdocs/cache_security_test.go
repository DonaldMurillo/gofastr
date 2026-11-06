package sdkdocs

import (
	"context"
	"net/http"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/app/decide"
)

func TestArtifactCachePrivacy(t *testing.T) {
	for _, gated := range []bool{false, true} {
		name, want := "public", "no-cache"
		var policy app.Policy
		if gated {
			name, want = "gated", "private, no-cache"
			policy = app.PolicyFunc(func(context.Context) app.Decision { return decide.Allow() })
		}
		t.Run(name, func(t *testing.T) {
			registry := testRegistry()
			server := mountedServer(t, Config{Registry: registry, Artifacts: testArtifacts(t, registry, true),
				Policy: policy, IncludeGated: gated})
			for _, artifact := range []string{"go.zip", "client.js", "client.d.ts", "manifest.json"} {
				t.Run(artifact, func(t *testing.T) {
					path := "/docs/api/sdk/" + artifact
					response, _ := get(t, server, path)
					if response.StatusCode != http.StatusOK || response.Header.Get("Cache-Control") != want {
						t.Errorf("GET status=%d, cache=%q; want 200, %q", response.StatusCode, response.Header.Get("Cache-Control"), want)
					}
					etag := response.Header.Get("ETag")
					for _, method := range []string{http.MethodGet, http.MethodHead} {
						request, err := http.NewRequest(method, server.URL+path, nil)
						if err != nil {
							t.Fatal(err)
						}
						request.Header.Set("If-None-Match", etag)
						response, err := server.Client().Do(request)
						if err != nil {
							t.Fatal(err)
						}
						response.Body.Close()
						status := http.StatusOK
						if etag != "" {
							status = http.StatusNotModified
						}
						if response.StatusCode != status || response.Header.Get("Cache-Control") != want {
							t.Errorf("%s revalidation status=%d, cache=%q; want %d, %q", method,
								response.StatusCode, response.Header.Get("Cache-Control"), status, want)
						}
					}
				})
			}
		})
	}
}
