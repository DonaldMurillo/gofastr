package ui

import (
	"strings"
	"testing"
)

func TestBackToTopExtraAttrsOnRoot(t *testing.T) {
	h := BackToTop(BackToTopConfig{
		ExtraAttrs: map[string]string{"data-test": "hook"},
	})
	root := string(h)[:strings.Index(string(h), ">")+1]
	if !strings.Contains(root, `data-test="hook"`) {
		t.Errorf("button root missing data-test:\n%s", root)
	}
}
