package scan

import (
	"fmt"
	"slices"
	"testing"

	"github.com/DonaldMurillo/gofastr/internal/upgrade"
)

func TestScanTagLimitIsNotUnsatisfiable(t *testing.T) {
	note := &upgrade.Note{Find: upgrade.Find{Uses: []upgrade.Symbol{siteHeaderSym}}}
	result := mustRun(t, newWorkspace(t, defaultKit, map[string]string{
		"main.go":     plainMain(),
		"wide_or.go":  "//go:build alpha || beta || gamma || delta || epsilon || zeta || eta || theta || iota\n\npackage main\n",
		"wide_and.go": "//go:build one && two && three && four && five && six && seven && eight && nine && ten\n\npackage main\n",
	}), note)
	want := []string{
		fmt.Sprintf("wide_and.go (constraint names more than %d tags)", maxConstraintTags),
		fmt.Sprintf("wide_or.go (constraint names more than %d tags)", maxConstraintTags),
	}
	if !slices.Equal(result.Unscanned, want) {
		t.Fatalf("Unscanned = %v, want %v", result.Unscanned, want)
	}
}
