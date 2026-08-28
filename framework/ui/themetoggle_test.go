package ui

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/render"
)

func TestThemeToggleExtraAttrsOnEveryRootShape(t *testing.T) {
	extra := map[string]string{"data-test": "hook", "aria-label": "evil"}
	for name, h := range map[string]render.HTML{
		"icon":  ThemeToggle(ThemeToggleConfig{ExtraAttrs: extra}),
		"label": ThemeToggle(ThemeToggleConfig{Variant: ThemeToggleLabel, ExtraAttrs: extra}),
		"pill":  ThemeToggle(ThemeToggleConfig{Variant: ThemeTogglePill, ExtraAttrs: extra}),
	} {
		root := string(h)[:strings.Index(string(h), ">")+1]
		if !strings.Contains(root, `data-test="hook"`) {
			t.Errorf("%s root missing data-test:\n%s", name, root)
		}
		if strings.Contains(root, "evil") {
			t.Errorf("%s root: aria-label is i18n-owned; ExtraAttrs copy must be dropped:\n%s", name, root)
		}
	}
}
