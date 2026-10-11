package ui

import (
	"regexp"
	"strings"
	"testing"
)

var stepTagRe = regexp.MustCompile(`<(?:a|button)[^>]*aria-label="(?:Previous|Next) record"[^>]*>`)

// Prev and Next draw the drawer's step controls after the path, before
// copy link: icon links that swap the target into the same layer. At an
// end of the list the missing one is the same control disabled, so the
// bar keeps its shape; with neither, the bar draws no step controls.
func TestDrawerBarSteps(t *testing.T) {
	both := string(DrawerBar(DrawerBarConfig{Path: "/r/2", Prev: "/r/1", Next: "/r/3", CopyURL: "http://x/r/2"}))
	tags := stepTagRe.FindAllString(both, -1)
	if len(tags) != 2 {
		t.Fatalf("want two step controls, got %d:\n%s", len(tags), both)
	}
	for i, want := range []string{`href="/r/1"`, `href="/r/3"`} {
		if !strings.Contains(tags[i], want) || !strings.Contains(tags[i], "data-cui-intercept-swap") {
			t.Errorf("step %d = %s, want a swap link with %s", i, tags[i], want)
		}
	}
	path, prev, copyAt := strings.Index(both, "fui-drawer-bar__path"), strings.Index(both, tags[0]), strings.Index(both, `aria-label="Copy link"`)
	if !(path < prev && prev < copyAt) {
		t.Errorf("steps sit at %d, want after the path (%d) and before copy (%d)", prev, path, copyAt)
	}

	last := string(DrawerBar(DrawerBarConfig{Path: "/r/3", Prev: "/r/2"}))
	tags = stepTagRe.FindAllString(last, -1)
	if len(tags) != 2 || !strings.HasPrefix(tags[1], "<button") || !strings.Contains(tags[1], "disabled") || strings.Contains(tags[1], "href=") {
		t.Fatalf("the last record's Next must be a disabled button: %v", tags)
	}

	if none := string(DrawerBar(DrawerBarConfig{Path: "/r/1"})); stepTagRe.MatchString(none) || strings.Contains(none, "data-cui-intercept-swap") {
		t.Errorf("a bar with no neighbours drew step controls:\n%s", none)
	}
}
