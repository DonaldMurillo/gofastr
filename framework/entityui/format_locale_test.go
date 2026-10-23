package entityui

import (
	"context"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/i18n"
)

// A float cell is plain decimal, never Go's exponent form, so a
// million reaches the locale formatter as digits it accepts.
func TestFloatCellIsPlainDecimal(t *testing.T) {
	if got := cell(1e6); got != "1000000" {
		t.Errorf("cell(1e6) = %q, want 1000000", got)
	}
	if got := cell(float32(0.5)); got != "0.5" {
		t.Errorf("cell(float32 0.5) = %q, want 0.5", got)
	}
	en := i18n.WithContext(context.Background(), i18n.Locale{Tag: "en"})
	if got := decimal(en, cell(1e6)); got != "1,000,000.00" {
		t.Errorf("decimal of a million = %q, want 1,000,000.00", got)
	}
}

// An int cell takes the locale's digits and no grouping: a year stays
// one run.
func TestIntCellUsesLocaleDigits(t *testing.T) {
	ar := i18n.WithContext(context.Background(), i18n.Locale{Tag: "ar-EG"})
	if got := integer(ar, "2026"); got != "٢٠٢٦" {
		t.Errorf("ar-EG: got %q want ٢٠٢٦", got)
	}
	if got := integer(nil, "2026"); got != "2026" {
		t.Errorf("English: got %q want 2026", got)
	}
	if got := integer(ar, "not a number"); got != "not a number" {
		t.Errorf("non-integer: got %q, want it as stored", got)
	}
}
