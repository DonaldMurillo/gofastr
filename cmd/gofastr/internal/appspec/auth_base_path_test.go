package appspec

import (
	"strings"
	"testing"
)

// base_path is joined straight onto the battery's routes and the
// generated forms' actions, so only a clean absolute path is accepted.
func TestAuthBasePathMustBeClean(t *testing.T) {
	for _, p := range []string{"", "/auth", "/account", "/v1/auth", "/my-auth_2", "/a.b"} {
		bp := AppSpec{}
		bp.App.Auth.BasePath = p
		if err := ValidateAppSpec(bp); err != nil && strings.Contains(err.Error(), "base_path") {
			t.Errorf("%q refused: %v", p, err)
		}
	}
	for _, p := range []string{"auth", "/auth/", "/", "//evil.example", "/a//b", "/..", "/a/.", "/.hidden", "/a b", "/a?x=1", "/a#b", `/a"b`} {
		bp := AppSpec{}
		bp.App.Auth.BasePath = p
		if err := ValidateAppSpec(bp); err == nil || !strings.Contains(err.Error(), "base_path") {
			t.Errorf("%q accepted: %v", p, err)
		}
	}
}
