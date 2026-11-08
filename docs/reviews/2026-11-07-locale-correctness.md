# Locale formatting correctness review — November 7, 2026

Independently reproduced at `953ccc08256e825bdc17254c463a5695a27092b6`.
Three P3 root causes, C16–C18. No production fixes or dependency changes.
The source generator and generated locale data declare CLDR 47.0.0;
reference fixtures below come from that exact Unicode release, not an
arbitrary current browser's ICU version.

## C16 — Number emits invariant NaN instead of the locale's symbol

Root: `core/i18n/number.go:269` returns the fixed string `NaN`; the special
value arm at `number.go:216` does not localize it. The locale data model
has no NaN symbol field. The public contract at
`framework/docs/content/i18n.md:387` says NaN and infinities use the
locale's symbols.

Fresh public `i18n.Number` calls with locale contexts and `math.NaN()`:

| Locale | Observed | CLDR 47 symbol |
| --- | --- | --- |
| en | NaN | NaN |
| ru | NaN | не\u00a0число |
| fi | NaN | epäluku |

The Russian symbol contains U+00A0, not an ordinary space; the independent
probe compares the exact source bytes rather than the reviewer's prose.
English NaN, Russian positive/negative infinity, Swedish minus infinity
(U+2212) and an ordinary English decimal all pass as controls. Only the
NaN localization root is counted, not infinity formatting.

## C17 — Known currencies round to stale minor-unit precision

Root: `core/i18n/number.go:120` takes standard digits from the linked
`golang.org/x/text/currency` snapshot (v0.43.0), rather than the precision
in the project's declared CLDR release. `Currency`'s public comment and
the same guide promise rounding to the currency's own digits.

For each of AMD, GYD, MNT, MUR, TZS and UZS, fresh public
`Currency(enContext, 1234.5678, code)` outputs the code, U+00A0, and
`1,235`. Each code is recognized by `currency.ParseISO`, but standard
rounding returns zero digits. CLDR 47's fractions data specifies two
standard digits for all six (and separately zero cash digits). The
expected numeric display is `1,234.57`, not whole-unit rounding.
One stale precision source, six supported codes, one finding.

Positive controls agree with CLDR 47: EUR=2 digits (`€1,234.57`),
JPY=0 (`¥1,235`), BHD=3, CLF=4 and CLP=0. These are display
rounding errors, not claims of database mutation or lost stored money.

**Narrowed reviewer claim:** UYW is deliberately excluded from accepted
evidence. The linked library does not recognize it; the public API
expressly defines an unknown-code fallback with two digits and no symbol.
That fallback is not a seventh instance of the accepted known-code defect.
No browser Intl vintage is used as the oracle for this finding.

## C18 — FirstWeekday contradicts its declared week-data snapshot

Root: `core/i18n/calendar.go:70` includes CN in the Sunday list;
`calendar.go:74` includes AE in the Saturday list. The table's comment
explicitly identifies CLDR 47 weekData, and the public guide at
`framework/docs/content/i18n.md:408` assigns regional defaults to CLDR.

| Locale | Public FirstWeekday result | CLDR 47 regional firstDay |
| --- | --- | --- |
| zh-CN | Sunday | Monday |
| ar-AE | Saturday | Monday |

Controls US=Sunday, GB/FR=Monday, EG=Saturday and MV=Friday agree with the
same source. The explicit `en-GB-u-fw-sun` override and malformed-tag
English fallback also pass. This is a regional default-table defect,
not timezone conversion or an inferred physical calendar rendering issue.

## Independent evidence and limits

The coordinator wrote a new external-package observation test, injecting it
with a Go overlay and invoking the actual public APIs. Reference JSON is
parsed directly during each run. Tests pin current observed defects plus
passing controls; their successful exit is not a claim the bugs are fixed.
No browser or Node Intl execution is required for these reproductions.
The original reviewer's broader formatting probes were not independently
rerun. Existing package-suite results are recorded in the ledger separately.

Retained source, overlay, log and downloaded data:
`/tmp/gofastr-coordinator-locale/`. Restore observation_test.go below, then:

```sh
mkdir -p /tmp/gofastr-coordinator-locale
base=https://raw.githubusercontent.com/unicode-org/cldr-json/47.0.0/cldr-json
for locale in en ru fi; do
  curl -fsSL "$base/cldr-numbers-full/main/$locale/numbers.json" -o "/tmp/gofastr-coordinator-locale/$locale.json"
done
for name in currencyData weekData; do
  curl -fsSL "$base/cldr-core/supplemental/$name.json" -o "/tmp/gofastr-coordinator-locale/$name.json"
done
printf '{"Replace":{"%s/core/i18n/zz_coordinator_locale_observation_test.go":"/tmp/gofastr-coordinator-locale/observation_test.go"}}\n' "$PWD" > /tmp/gofastr-coordinator-locale/overlay.json
go test -p 2 -count=1 -overlay /tmp/gofastr-coordinator-locale/overlay.json -run '^TestCoordinatorLocale' -v ./core/i18n/
go test -p 2 -count=1 ./core/i18n/
```

### Reference byte digests (SHA-256)

```text
0fb0816663dd8be96ab2ddf9a6733a6fb0d6e3cc2562fdd2d6ecd623c655920f  en.json
768cf1b9b38889426f2d35b27077e5aa75bfcee245c0bb8d19bead0885b866df  ru.json
ee08c315c8cb32cb1076ad31bbf77ae2bd51a022a84665016e2d768664399f8c  fi.json
36f72ed0d2f16cecdf19be0808dac85782baba795f93d6526b56c40d0fa806c6  currencyData.json
d93c0c48cb5a6ef639c7d96092f84d3bebeefc8c8b02740773e17daaba8de4e1  weekData.json
```

### observation_test.go

```go
package i18n_test

import (
	"context"
	"encoding/json"
	"math"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/i18n"
	"golang.org/x/text/currency"
)

func coordinatorLocale(tag string) context.Context {
	return i18n.WithContext(context.Background(), i18n.Locale{Tag: tag})
}

func coordinatorReference(t *testing.T, filename string, destination any) {
	t.Helper()
	data, err := os.ReadFile("/tmp/gofastr-coordinator-locale/" + filename)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, destination); err != nil {
		t.Fatal(err)
	}
}

func TestCoordinatorLocaleNaNObservation(t *testing.T) {
	for _, tag := range []string{"en", "ru", "fi"} {
		data, err := os.ReadFile("/tmp/gofastr-coordinator-locale/" + tag + ".json")
		if err != nil {
			t.Fatal(err)
		}
		var envelope struct {
			Main map[string]struct {
				Numbers map[string]json.RawMessage `json:"numbers"`
			} `json:"main"`
		}
		if err := json.Unmarshal(data, &envelope); err != nil {
			t.Fatal(err)
		}
		var symbols map[string]string
		if err := json.Unmarshal(envelope.Main[tag].Numbers["symbols-numberSystem-latn"], &symbols); err != nil {
			t.Fatal(err)
		}
		got, want := i18n.Number(coordinatorLocale(tag), math.NaN()), symbols["nan"]
		if got != "NaN" || want == "" || (got == want) != (tag == "en") {
			t.Fatalf("%s NaN: got %q; CLDR47 %q", tag, got, want)
		}
		t.Logf("%s: got=%q CLDR47=%q", tag, got, want)
	}
	for _, control := range []struct {
		tag   string
		value float64
		want  string
	}{{"ru", math.Inf(1), "∞"}, {"ru", math.Inf(-1), "-∞"}, {"sv", math.Inf(-1), "−∞"}, {"en", 1234.5, "1,234.5"}} {
		if got := i18n.Number(coordinatorLocale(control.tag), control.value); got != control.want {
			t.Fatalf("control %s: %q != %q", control.tag, got, control.want)
		}
	}
}

func TestCoordinatorLocaleCurrencyObservation(t *testing.T) {
	var source struct {
		Supplemental struct {
			CurrencyData struct {
				Fractions map[string]struct {
					Digits string `json:"_digits"`
				} `json:"fractions"`
			} `json:"currencyData"`
		} `json:"supplemental"`
	}
	coordinatorReference(t, "currencyData.json", &source)
	for _, code := range []string{"AMD", "GYD", "MNT", "MUR", "TZS", "UZS", "EUR", "JPY", "BHD", "CLF", "CLP"} {
		unit, err := currency.ParseISO(code)
		if err != nil {
			t.Fatalf("unsupported fixture %s: %v", code, err)
		}
		got := i18n.Currency(coordinatorLocale("en"), 1234.5678, code)
		digits, _ := currency.Standard.Rounding(unit)
		want := source.Supplemental.CurrencyData.Fractions[code].Digits
		if want == "" {
			want = source.Supplemental.CurrencyData.Fractions["DEFAULT"].Digits
		}
		affected := strings.Contains(" AMD GYD MNT MUR TZS UZS ", " "+code+" ")
		if affected {
			if digits != 0 || want != "2" || !strings.HasSuffix(got, "1,235") {
				t.Fatalf("%s: digits=%d CLDR47=%s output=%q", code, digits, want, got)
			}
		} else if string(rune('0'+digits)) != want {
			t.Fatalf("control %s: digits=%d CLDR47=%s output=%q", code, digits, want, got)
		}
		t.Logf("%s: got=%q fraction-digits=%d CLDR47=%s", code, got, digits, want)
	}
	unit, err := currency.ParseISO("UYW")
	if err != nil {
		t.Logf("UYW omitted from accepted evidence: ParseISO=%v", err)
	} else {
		digits, _ := currency.Standard.Rounding(unit)
		t.Logf("UYW supplementary observation: digits=%d output=%q", digits, i18n.Currency(coordinatorLocale("en"), 1234.5678, "UYW"))
	}
}

func TestCoordinatorLocaleWeekObservation(t *testing.T) {
	var source struct {
		Supplemental struct {
			WeekData struct {
				FirstDay map[string]string `json:"firstDay"`
			} `json:"weekData"`
		} `json:"supplemental"`
	}
	coordinatorReference(t, "weekData.json", &source)
	dayCode := map[string]time.Weekday{"sun": time.Sunday, "mon": time.Monday, "fri": time.Friday, "sat": time.Saturday}
	for _, item := range []struct {
		tag, region string
		observed    time.Weekday
	}{
		{"zh-CN", "CN", time.Sunday}, {"ar-AE", "AE", time.Saturday},
		{"en-US", "US", time.Sunday}, {"en-GB", "GB", time.Monday},
		{"fr-FR", "FR", time.Monday}, {"ar-EG", "EG", time.Saturday}, {"dv-MV", "MV", time.Friday},
	} {
		got := i18n.FirstWeekday(coordinatorLocale(item.tag))
		want := dayCode[source.Supplemental.WeekData.FirstDay[item.region]]
		affected := item.region == "CN" || item.region == "AE"
		if got != item.observed || (got != want) != affected {
			t.Fatalf("%s: got=%s expected-observation=%s CLDR47=%s", item.tag, got, item.observed, want)
		}
		t.Logf("%s: got=%s CLDR47=%s", item.tag, got, want)
	}
	if got := i18n.FirstWeekday(coordinatorLocale("en-GB-u-fw-sun")); got != time.Sunday {
		t.Fatal(got)
	}
	if got := i18n.FirstWeekday(coordinatorLocale("!not_a_tag!")); got != time.Sunday {
		t.Fatal(got)
	}
}
```
