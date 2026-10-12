package ui

import (
	"strings"
	"testing"
)

func TestThumbnailDrawsTheImage(t *testing.T) {
	got := string(Thumbnail(ThumbnailConfig{Src: "/p.png", Alt: "Drill", Size: ThumbnailSM}))
	for _, want := range []string{`data-cui-comp="ui-thumbnail"`, "fui-thumbnail--sm", `src="/p.png"`, `alt="Drill"`, `loading="lazy"`} {
		if !strings.Contains(got, want) {
			t.Errorf("thumbnail lacks %q:\n%s", want, got)
		}
	}
}

func TestThumbnailRefusesUnsafeSrc(t *testing.T) {
	for _, src := range []string{"", "javascript:alert(1)", "data:image/svg+xml,<svg onload=alert(1)>", "data:text/html,x"} {
		if got := Thumbnail(ThumbnailConfig{Src: src, Alt: "x"}); got != "" {
			t.Errorf("SECURITY: Thumbnail(%q) = %s, want nothing", src, got)
		}
	}
}
