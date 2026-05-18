package ui

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/render"
)

func TestRatingRequiresName(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("RatingInput without Name should panic")
		}
	}()
	RatingInput(RatingConfig{Label: "x"})
}

func TestRatingRequiresLabel(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("RatingInput without Label should panic")
		}
	}()
	RatingInput(RatingConfig{Name: "x"})
}

func TestRatingDefault5Stars(t *testing.T) {
	h := string(RatingInput(RatingConfig{Name: "r", Label: "Rate"}))
	if c := strings.Count(h, `type="radio"`); c != 5 {
		t.Errorf("default Max=5 should emit 5 radios, got %d:\n%s", c, h)
	}
}

func TestRatingMaxOverride(t *testing.T) {
	h := string(RatingInput(RatingConfig{Name: "r", Label: "Rate", Max: 7}))
	if c := strings.Count(h, `type="radio"`); c != 7 {
		t.Errorf("Max=7 should emit 7 radios, got %d:\n%s", c, h)
	}
}

func TestRatingValueInitialChecked(t *testing.T) {
	h := string(RatingInput(RatingConfig{Name: "r", Label: "Rate", Value: 4}))
	if c := strings.Count(h, " checked"); c != 1 {
		t.Errorf("Value=4 should leave exactly 1 radio checked, got %d:\n%s", c, h)
	}
}

func TestRatingHeartVariantClass(t *testing.T) {
	h := string(RatingInput(RatingConfig{Name: "r", Label: "Rate", Shape: RatingShapeHeart}))
	if !strings.Contains(h, "ui-rating--heart") {
		t.Errorf("Heart shape should add .ui-rating--heart:\n%s", h)
	}
}

func TestRatingSizeAndGapVariantClasses(t *testing.T) {
	h := string(RatingInput(RatingConfig{
		Name: "r", Label: "Rate",
		Size: RatingSizeLarge, Gap: RatingGapWide,
	}))
	if !strings.Contains(h, "ui-rating--large") {
		t.Errorf("Size=Large should add .ui-rating--large:\n%s", h)
	}
	if !strings.Contains(h, "ui-rating--gap-wide") {
		t.Errorf("Gap=Wide should add .ui-rating--gap-wide:\n%s", h)
	}
}

func TestRatingCustomIconOverridesShape(t *testing.T) {
	custom := render.HTML(`<svg viewBox="0 0 4 4"><circle cx="2" cy="2" r="2"/></svg>`)
	h := string(RatingInput(RatingConfig{
		Name: "r", Label: "Rate", Shape: RatingShapeHeart, Icon: custom,
	}))
	if !strings.Contains(h, "viewBox=\"0 0 4 4\"") {
		t.Errorf("custom Icon should override Shape glyph:\n%s", h)
	}
	if strings.Contains(h, "ui-rating--heart") {
		t.Errorf("Icon overrides Shape — Shape variant class should not emit:\n%s", h)
	}
}

func TestRatingRadioInputsHaveAriaLabel(t *testing.T) {
	h := string(RatingInput(RatingConfig{Name: "r", Label: "Rate"}))
	if !strings.Contains(h, `aria-label="1 star out of 5"`) {
		t.Errorf("each radio should carry an aria-label like '1 star out of 5':\n%s", h)
	}
	if !strings.Contains(h, `aria-label="5 stars out of 5"`) {
		t.Errorf("each radio should carry an aria-label like 'N stars out of 5':\n%s", h)
	}
}

func TestRatingRejectsUnknownShape(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("RatingInput with unknown Shape should panic")
		}
	}()
	RatingInput(RatingConfig{Name: "r", Label: "Rate", Shape: RatingShape("bogus")})
}

func TestRatingRejectsUnknownSize(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("RatingInput with unknown Size should panic")
		}
	}()
	RatingInput(RatingConfig{Name: "r", Label: "Rate", Size: RatingSize("huge")})
}

func TestRatingRejectsUnknownGap(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("RatingInput with unknown Gap should panic")
		}
	}()
	RatingInput(RatingConfig{Name: "r", Label: "Rate", Gap: RatingGap("nope")})
}
