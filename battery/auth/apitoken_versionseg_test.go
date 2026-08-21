package auth

import "testing"

// A version-like path segment is skipped only when a real resource segment
// follows it. A table genuinely named "v1"/"v2" must still require a scope,
// skipping the last segment leaves resource empty, and the empty case falls
// through unchecked, which would be an authorization bypass.
func TestAPIScopes_VersionSegmentNeverSwallowsResource(t *testing.T) {
	cases := []struct {
		name         string
		method, path string
		scopes       []string
		want         int
	}{
		// The feature: /v1/posts and /v2/posts share the resource "posts".
		{"v1 prefix maps to posts", "GET", "/api/v1/posts", []string{"posts:read"}, 200},
		{"v2 prefix maps to posts", "GET", "/api/v2/posts", []string{"posts:read"}, 200},
		{"minor version prefix", "GET", "/api/v1.2/posts", []string{"posts:read"}, 200},
		{"versioned write needs write", "POST", "/api/v1/posts", []string{"posts:read"}, 403},
		{"versioned path rejects wrong resource", "GET", "/api/v1/posts", []string{"orders:read"}, 403},

		// The bypass: an entity whose table is literally version-shaped.
		{"table named v1 still gated", "GET", "/api/v1", []string{"orders:read"}, 403},
		{"table named v1 allowed with scope", "GET", "/api/v1", []string{"v1:read"}, 200},
		{"table named v2 write gated", "POST", "/api/v2", []string{"v2:read"}, 403},
		{"table named v1 subpath still gated", "GET", "/api/v1/", []string{"orders:read"}, 403},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := driveAPIScopes(t, "/api", tc.method, tc.path, tc.scopes); got != tc.want {
				t.Errorf("%s %s with %v = %d, want %d", tc.method, tc.path, tc.scopes, got, tc.want)
			}
		})
	}
}
