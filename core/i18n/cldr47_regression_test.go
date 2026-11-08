package i18n_test

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/i18n"
)

func readerIn(tag string) context.Context {
	return i18n.WithContext(context.Background(), i18n.Locale{Tag: tag})
}

// NaN prints as the locale's CLDR 47 symbol, the way the infinities
// already did. Russian's has a no-break space, so the bytes are pinned.
func TestNaNIsLocaleSymbol(t *testing.T) {
	for _, tc := range []struct {
		tag, want string
	}{
		{"en", "NaN"},
		{"ru", "не\u00a0число"},
		{"fi", "epäluku"},
		{"fi-FI", "epäluku"},
		{"ar-EG", "ليس\u00a0رقمًا"},
		{"zz-not-a-locale", "NaN"},
		{"", "NaN"},
	} {
		if got := i18n.Number(readerIn(tc.tag), math.NaN()); got != tc.want {
			t.Errorf("%q: got %q want %q", tc.tag, got, tc.want)
		}
	}
	if got := i18n.Percent(readerIn("ru"), math.NaN()); got != "не\u00a0число\u00a0%" {
		t.Errorf("Percent(NaN) in Russian: got %q", got)
	}
	for _, tc := range []struct {
		tag   string
		value float64
		want  string
	}{
		{"ru", math.Inf(1), "∞"},
		{"ru", math.Inf(-1), "-∞"},
		{"sv", math.Inf(-1), "\u2212∞"},
		{"en", 1234.5, "1,234.5"},
		{"ru", 1234.5, "1\u00a0234,5"},
	} {
		if got := i18n.Number(readerIn(tc.tag), tc.value); got != tc.want {
			t.Errorf("control %s %v: got %q want %q", tc.tag, tc.value, got, tc.want)
		}
	}
}

// A known currency rounds to CLDR 47's standard digits. The first nine
// had none in golang.org/x/text's older table. UYW and "nope" are codes
// the currency library does not know: the documented fallback, two
// digits and no symbol, whatever CLDR lists for the code.
func TestCurrencyCLDR47Digits(t *testing.T) {
	english := readerIn("en")
	for _, tc := range []struct {
		code, want string
	}{
		{"AMD", "AMD\u00a01,234.57"},
		{"GYD", "GYD\u00a01,234.57"},
		{"MNT", "MNT\u00a01,234.57"},
		{"MUR", "MUR\u00a01,234.57"},
		{"TZS", "TZS\u00a01,234.57"},
		{"UZS", "UZS\u00a01,234.57"},
		{"COP", "COP\u00a01,234.57"},
		{"IDR", "IDR\u00a01,234.57"},
		{"PKR", "PKR\u00a01,234.57"},
		{"amd", "AMD\u00a01,234.57"},
		{"EUR", "€1,234.57"},
		{"JPY", "¥1,235"},
		{"BHD", "BHD\u00a01,234.568"},
		{"CLF", "CLF\u00a01,234.5678"},
		{"CLP", "CLP\u00a01,235"},
		{"USD", "$1,234.57"},
		{"UYW", "1,234.57"},
		{"nope", "1,234.57"},
	} {
		if got := i18n.Currency(english, 1234.5678, tc.code); got != tc.want {
			t.Errorf("%s: got %q want %q", tc.code, got, tc.want)
		}
	}
	if got := i18n.Currency(readerIn("fr"), 1234.5678, "AMD"); got != "1\u202f234,57\u00a0AMD" {
		t.Errorf("AMD in French: got %q", got)
	}
}

// The regional first day comes from CLDR 47's weekData: China and the
// UAE start on Monday.
func TestFirstWeekdayCLDR47(t *testing.T) {
	for _, tc := range []struct {
		tag  string
		want time.Weekday
	}{
		{"zh-CN", time.Monday},
		{"zh", time.Monday},
		{"zh-Hans", time.Monday},
		{"ar-AE", time.Monday},
		{"zh-TW", time.Sunday},
		{"zh-HK", time.Sunday},
		{"ar-SA", time.Sunday},
		{"ar", time.Saturday},
		{"ar-EG", time.Saturday},
		{"en-US", time.Sunday},
		{"en-GB", time.Monday},
		{"fr-FR", time.Monday},
		{"dv-MV", time.Friday},
		{"en-AQ", time.Monday},
		{"zh-CN-u-fw-sun", time.Sunday},
		{"ar-AE-u-fw-sat", time.Saturday},
		{"zh-CN-u-fw-xyz", time.Monday},
		{"not a tag", time.Sunday},
	} {
		if got := i18n.FirstWeekday(readerIn(tc.tag)); got != tc.want {
			t.Errorf("%q: got %v want %v", tc.tag, got, tc.want)
		}
	}
}
