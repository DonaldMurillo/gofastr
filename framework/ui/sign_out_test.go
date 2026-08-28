package ui

import (
	"strings"
	"testing"
)

func TestSignOutRendersFormAndButton(t *testing.T) {
	h := string(SignOut(SignOutConfig{}))
	for _, want := range []string{
		`data-fui-comp="ui-sign-out"`,
		`method="post"`,
		`action="/auth/logout"`,
		`type="submit"`,
	} {
		if !strings.Contains(h, want) {
			t.Errorf("SignOut missing %q:\n%s", want, h)
		}
	}
}

func TestSignOutExtraAttrsOnRoot(t *testing.T) {
	h := SignOut(SignOutConfig{ExtraAttrs: map[string]string{"data-test": "hook"}})
	root := string(h)[:strings.Index(string(h), ">")+1]
	if !strings.Contains(root, `data-test="hook"`) {
		t.Errorf("SignOut root missing data-test:\n%s", root)
	}
}
