package app

import (
	"strings"
	"testing"
	"time"
)

func TestLayoutNamesAreAddresses(t *testing.T) {
	for _, name := range []string{"", "main shell", "side#bar", "left~right", "café", "line\nbreak", "x;y"} {
		t.Run("layout/"+name, func(t *testing.T) {
			if got := panicMsg(t, func() { NewLayout(name, LayoutSpec{}, nil) }); got == "" {
				t.Fatalf("invalid layout name %q accepted", name)
			}
		})
		t.Run("area/"+name, func(t *testing.T) {
			if got := panicMsg(t, func() { NewLayout("shell", LayoutSpec{Areas: []AreaSpec{{Name: name}}}, nil) }); got == "" {
				t.Fatalf("invalid area name %q accepted", name)
			}
		})
	}
	if got := panicMsg(t, func() {
		NewLayout("shell", LayoutSpec{Areas: []AreaSpec{{Name: "crumbs"}, {Name: "crumbs"}}}, nil)
	}); !strings.Contains(got, "crumbs") {
		t.Fatalf("duplicate area did not name its address: %q", got)
	}
	layout := NewLayout("0Root-_", LayoutSpec{Areas: []AreaSpec{{Name: "3Aside-_", Transition: Crossfade(time.Millisecond)}}}, nil)
	if css := layout.TransitionCSS(); !strings.Contains(string(css), "vt-0Root-_-3Aside-_") {
		t.Fatalf("valid names did not produce the transition: %s", css)
	}
}
