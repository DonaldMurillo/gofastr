package routegroup

import "testing"

// TestNormalizePrefix_HardensInput pins the canonical-form contract.
// A non-canonical prefix permanently aliases every child route under
// it: `/admin` and `/admin/` and `\admin` and `/admin/.` should all
// resolve to one mount point, not four parallel ones.
func TestNormalizePrefix_HardensInput(t *testing.T) {
	cases := map[string]string{
		// canonical pass-through
		"":             "",
		"/":            "",
		"/api":         "/api",
		"/api/admin":   "/api/admin",
		"/api/admin/":  "/api/admin",
		"api":          "/api",

		// repeated separators collapse
		"/api//admin":    "/api/admin",
		"//api/admin":    "/api/admin",
		"/api///admin":   "/api/admin",
		"////admin/ops":  "/admin/ops",

		// dot segments resolve
		"/api/./admin":      "/api/admin",
		"/api/../admin":     "/admin",
		"/api/admin/..":     "/api",
		"/api/admin/../../": "",
		"/..":               "",

		// backslashes convert
		"\\api\\admin":  "/api/admin",
		"/api\\admin":   "/api/admin",

		// control bytes strip
		"/api/\nadmin":           "/api/admin",
		"/api/\radmin":           "/api/admin",
		"/api/\x00admin":         "/api/admin",
		"/api/" + "\x1f" + "x":   "/api/x",
	}
	for in, want := range cases {
		t.Run(in, func(t *testing.T) {
			if got := normalizePrefix(in); got != want {
				t.Fatalf("normalizePrefix(%q)=%q, want %q", in, got, want)
			}
		})
	}
}
