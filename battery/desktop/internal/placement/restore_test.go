package placement

import (
	"testing"

	"github.com/DonaldMurillo/gofastr/battery/desktop"
)

func TestRestoreDropsInvisibleFrames(t *testing.T) {
	screens := []desktop.Frame{{X: 0, Y: 24, Width: 1440, Height: 876}, {X: -1280, Y: -100, Width: 1280, Height: 1024}}
	center := desktop.Frame{X: 320, Y: 162, Width: 800, Height: 600}
	cases := []struct {
		name  string
		frame desktop.Frame
		keep  bool
	}{
		{"primary", desktop.Frame{X: 100, Y: 50, Width: 800, Height: 600}, true},
		{"negative monitor", desktop.Frame{X: -1100, Y: -40, Width: 800, Height: 600}, true},
		{"exact overlap", desktop.Frame{X: 1340, Y: 850, Width: 800, Height: 600}, true},
		{"unplugged", desktop.Frame{X: 4000, Y: 100, Width: 800, Height: 600}, false},
		{"thin overlap", desktop.Frame{X: 1341, Y: 100, Width: 800, Height: 600}, false},
		{"short overlap", desktop.Frame{X: 100, Y: 851, Width: 800, Height: 600}, false},
		{"monitor seam", desktop.Frame{X: -99, Y: 100, Width: 198, Height: 100}, false},
		{"zero width", desktop.Frame{X: 100, Y: 100, Height: 600}, false},
		{"negative height", desktop.Frame{X: 100, Y: 100, Width: 800, Height: -1}, false},
		{"oversized", desktop.Frame{Width: 32768, Height: 600}, false},
		{"corrupt width", desktop.Frame{Width: int(^uint(0) >> 1), Height: 600}, false},
		{"corrupt origin", desktop.Frame{X: int(^uint(0) >> 1), Width: 800, Height: 600}, false},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			original := test.frame
			got := Restore(&test.frame, screens, 800, 600)
			want := center
			if test.keep {
				want = test.frame
			}
			if got == nil || *got != want {
				t.Fatalf("restored %+v, want %+v", got, want)
			}
			if test.frame != original {
				t.Fatal("restore changed the stored frame")
			}
		})
	}
}

func TestRestoreWithoutScreenUsesDefault(t *testing.T) {
	frame := desktop.Frame{X: 4000, Y: 100, Width: 800, Height: 600}
	if got := Restore(&frame, nil, 800, 600); got != nil {
		t.Fatalf("missing screens retained %+v instead of the shell default", *got)
	}
	if got := Restore(nil, []desktop.Frame{{Width: 1440, Height: 900}}, 800, 600); got != nil {
		t.Fatal("a missing remembered frame replaced the caller's placement")
	}
}

func TestRestoreFitsSmallWorkArea(t *testing.T) {
	frame := desktop.Frame{X: 4000, Width: 800, Height: 600}
	screen := desktop.Frame{X: 100, Y: 50, Width: 200, Height: 120}
	got := Restore(&frame, []desktop.Frame{screen}, 800, 600)
	if got == nil || *got != screen {
		t.Fatalf("restored %+v, want small work area %+v", got, screen)
	}
}
