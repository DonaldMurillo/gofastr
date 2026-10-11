package ui

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/ui/theme"
)

// A testimonial's five stars were a disabled RatingInput: a radio group
// a screen reader announces as a form control, dimmed to 60% and spaced
// on 44px tap targets. Rating is the read-only picture of a score.
func TestRatingIsAnImageNotARadioGroup(t *testing.T) {
	out := string(Rating(RatingDisplayConfig{Value: 4}))
	for _, want := range []string{`role="img"`, `aria-label="4 out of 5"`, `data-cui-comp="ui-rating-display"`} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %s in:\n%s", want, out)
		}
	}
	for _, bad := range []string{"<input", "radiogroup", "<fieldset"} {
		if strings.Contains(out, bad) {
			t.Errorf("display rating carries %s:\n%s", bad, out)
		}
	}
	if got := strings.Count(out, "fui-rating-display__glyph"); got != 5 {
		t.Errorf("glyphs = %d, want 5", got)
	}
	if got := strings.Count(out, "is-on"); got != 4 {
		t.Errorf("filled glyphs = %d, want 4", got)
	}
}

func TestRatingLabelAndMax(t *testing.T) {
	out := string(Rating(RatingDisplayConfig{Value: 2, Max: 3, Label: "Two of three hearts", Shape: RatingShapeHeart}))
	if !strings.Contains(out, `aria-label="Two of three hearts"`) {
		t.Errorf("label not applied:\n%s", out)
	}
	if got := strings.Count(out, "fui-rating-display__glyph"); got != 3 {
		t.Errorf("glyphs = %d, want 3", got)
	}
	if !strings.Contains(out, "fui-rating--heart") {
		t.Errorf("heart shape lost its colour modifier:\n%s", out)
	}
}

func TestRatingRejectsOutOfRange(t *testing.T) {
	for _, v := range []int{-1, 6} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("Value %d did not panic", v)
				}
			}()
			Rating(RatingDisplayConfig{Value: v})
		}()
	}
}

// The display sizes to its glyphs: nothing in its sheet reaches for the
// touch-target floor that spaces the input's stars 44px apart.
func TestRatingSheetHasNoTapTargets(t *testing.T) {
	css := ratingDisplayStyle.Entry().CSSFor(theme.Default())
	if strings.Contains(css, "touch-target") {
		t.Errorf("display sheet sizes glyphs on the tap-target floor:\n%s", css)
	}
	if !strings.Contains(css, "--ui-rating-color") {
		t.Errorf("display sheet does not read the shared --ui-rating-color knob:\n%s", css)
	}
}
