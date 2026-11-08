# Locale fixes: C16, C17, C18

Findings: [2026-11-07-locale-correctness.md](2026-11-07-locale-correctness.md).
Base: main at 7d8cd9eea. Working-tree changes only; nothing committed.

## Coordinator validation

Fresh `go test -p 2 -count=1 ./core/i18n/...` passed on the combined
checkout. The coordinator independently ran
`go run -p 2 ./core/i18n/internal/cldrgen` and verified SHA-256 values of
both generated outputs remained unchanged. This checks reproducibility
against the pinned cached inputs, not an independent Unicode oracle.

The combined docs suite initially failed on a TLS timeout fetching an
existing observability example dependency; no locale assertion failed.
A fresh full docs retry passed (8.708s), as did standard `go vet` on the
i18n package tree, appgen and packcmd.
The repository's own vettool also passed on those packages.

| ID | Status | Fix |
| --- | --- | --- |
| C16 | Fixed | NaN comes from each locale's CLDR 47 `nan` symbol |
| C17 | Fixed | Known currencies round to CLDR 47 `currencyData` standard digits |
| C18 | Fixed | `FirstWeekday` reads a table generated from CLDR 47 `weekData` |

All three were reproduced on 7d8cd9eea before the change (see "Proof the
tests catch the bug" below).

## What changed

The three defects had one root shape: data the project says comes from
CLDR 47 was taken from somewhere else (a hard-coded string, the older
CLDR inside `golang.org/x/text`, and a hand-typed table). The fix moves
all three into `core/i18n/internal/cldrgen`, the generator that already
writes the CLDR 47 locale tables, so a CLDR bump updates them with
everything else.

- **C16.** The generator reads `nan` from each locale's
  `symbols-numberSystem-<default numbering>` block into a new
  `cldrLocale.nan` field, and refuses a locale with none (the existing
  empty-field check covers it). `formatNumber` hands the field to
  `plainDecimal`, which used to return the literal `"NaN"`. Infinity was
  left alone: it is `∞` in all 413 modern locales in CLDR 47.
- **C17.** The generator writes `cldrCurrencyDigits` (CLDR 47
  `fractions._digits` for every code CLDR lists) and
  `cldrCurrencyDefaultDigits` (`DEFAULT`, 2). `Currency` now looks up
  the code `currency.ParseISO` canonicalized, falling back to `DEFAULT`.
  x/text still supplies the symbol and decides whether a code is known,
  so the unknown-code fallback (two digits, no symbol) is unchanged and
  UYW still takes it.
- **C18.** The generator writes `cldrFirstDay` (each region whose
  `firstDay` differs from region 001) and `cldrWorldFirstDay` (001's
  value, Monday). It skips `-alt-` keys (`GB-alt-variant`) and refuses a
  key that is not two capital letters or a day it does not know. The
  hand-typed `firstDayByRegion` in `calendar.go` is deleted. Against the
  old table the generated one differs in exactly two entries: CN
  (was Sunday) and AE (was Saturday) now fall to Monday.

## Paths

- `core/i18n/internal/cldrgen/main.go`: reads the NaN symbol,
  `currencyData.json` and `weekData.json`; writes the new tables.
- `core/i18n/cldr_tables.gen.go`: regenerated; 372 lines added, none
  removed (210 `nan` fields, the `time` import, three tables). The
  locale index did not move.
- `core/i18n/cldr.go`: `nan` field on `cldrLocale`.
- `core/i18n/number.go`: `Currency`, `standardCurrencyDigits`,
  `plainDecimal`.
- `core/i18n/calendar.go`: `FirstWeekday` reads the generated table.
- `core/i18n/cldr47_regression_test.go`: new, external package, public
  API only.
- `framework/docs/content/i18n.md`: currency digits, the unknown-code
  fallback, NaN examples, the `zh`/`ar-AE` week starts and the world
  default.

`core/i18n/cldr_plural_samples_test.go` was regenerated and came out
byte-identical.

## Tests

New, in `core/i18n/cldr47_regression_test.go`:

- `TestNaNIsLocaleSymbol`: en `NaN`, ru `не\u00a0число`, fi and fi-FI
  `epäluku`, ar-EG (Arabic numbering) `ليس\u00a0رقمًا`; an unknown tag and
  no locale fall back to English `NaN`; `Percent` in Russian keeps the
  pattern affix (`не\u00a0число\u00a0%`). Controls: ru ±∞, sv `\u2212∞`,
  en `1,234.5`, ru `1\u00a0234,5`.
- `TestCurrencyCLDR47Digits`: AMD, GYD, MNT, MUR, TZS, UZS, COP, IDR,
  PKR and lower-case `amd` write `1,234.57`; AMD in French writes
  `1\u202f234,57\u00a0AMD`. Controls: EUR 2, JPY 0, BHD 3, CLF 4,
  CLP 0, USD 2. Fallback: UYW and `nope` write `1,234.57` with no
  symbol.
- `TestFirstWeekdayCLDR47`: zh-CN, zh, zh-Hans, ar-AE are Monday.
  Controls: zh-TW, zh-HK, ar-SA, en-US Sunday; ar, ar-EG Saturday;
  en-GB, fr-FR Monday; dv-MV Friday; en-AQ (a region weekData omits)
  Monday. Extensions: `zh-CN-u-fw-sun` Sunday, `ar-AE-u-fw-sat`
  Saturday, `zh-CN-u-fw-xyz` (unknown value) falls through to the
  region, Monday. A malformed tag reads as English, Sunday.

### Proof the tests catch the bug

The new test file was copied into a detached worktree at 7d8cd9eea and
run there. Every defect case failed (ru/fi/fi-FI/ar-EG NaN and Russian
Percent; all nine codes, `amd` and French AMD; zh-CN, zh, zh-Hans,
ar-AE and `zh-CN-u-fw-xyz`). Every control and fallback passed.

### Commands run

```sh
go run ./core/i18n/internal/cldrgen   # before the change: output byte-identical to HEAD
go run ./core/i18n/internal/cldrgen   # after, twice: same SHA-256 both runs
go test -p 2 -count=1 ./core/i18n/...                       # ok
go test -p 2 -count=1 ./framework/docs/                     # ok
go vet ./core/i18n/...                                      # ok
go build -o <scratch>/vettool ./cmd/vettool && go vet -vettool=<scratch>/vettool ./core/i18n/...   # ok
gofmt -l core/i18n                                          # clean
```

`framework/headless` (the date picker reads `FirstWeekday`) could not
run in the shared checkout: another agent's in-progress files there
(`menu_tab_e2e_test.go`, `sortablelist_version_e2e_test.go`) failed one
test and then stopped the package building. In a detached worktree at
7d8cd9eea with only this change copied in,
`go test -p 2 -count=1 ./framework/headless/ ./core/i18n/... ./framework/docs/`
passed. No headless test pins zh or ar-AE.

The reference JSON matches the review's SHA-256 digests for
`currencyData.json` and `weekData.json`.

## Limits and follow-ups

- **COP, IDR and PKR change too.** Sweeping every code
  `currency.ParseISO` accepts (all 17,576 three-letter strings) against
  x/text v0.43.0's `currency.Standard.Rounding` finds nine that differ
  from CLDR 47, not the six the review lists. COP, IDR and PKR had zero
  digits in x/text and have two in CLDR 47. Same root cause, so the
  generated table fixes them and the test pins them; it is not a scope
  extension. No code moved the other way.
- **Cash digits are not used.** Most of the nine have `_cashDigits: 0`
  in CLDR. `Currency` writes standard amounts, as its doc says; a cash
  display would need a new option.
- **UYW is unchanged on purpose.** CLDR 47 gives it four digits, but
  x/text does not recognize the code, so it keeps the documented
  unknown-code fallback (two digits, no symbol). Accepting it would mean
  a CLDR-side known-code list and a symbol source, a separate decision.
- **Symbols still come from x/text's older CLDR.** Only digits moved.
  The en symbols for the nine codes are the ISO code, which matches
  CLDR 47 for en; other locales were not audited.
- **`-alt-variant` week starts are skipped.** `GB-alt-variant` (Sunday)
  is not reachable through a plain tag. `-u-fw-sun` remains the way to
  ask for it.
- **CHANGELOG.md not edited.** Another agent is editing it. A line for
  `[Unreleased]` → Fixed: "`i18n.Number`/`Percent`/`Currency` write NaN
  as the locale's CLDR 47 symbol (`не число` in Russian); `Currency`
  rounds AMD, COP, GYD, IDR, MNT, MUR, PKR, TZS and UZS to CLDR 47's two
  digits; `FirstWeekday` starts zh-CN and ar-AE weeks on Monday."
- The generator needs the network on a cold cache
  (`~/Library/Caches/gofastr-cldr/47.0.0`); it now also fetches
  `currencyData.json` and `weekData.json`.

## Durable lessons

- A table whose comment names a CLDR release should come from the
  generator that pins that release. The hand-typed week table and the
  x/text digit lookup both claimed CLDR 47 and drifted from it.
- When replacing a data source, diff old against new over the whole
  domain, not only the reported cases. The currency sweep turned six
  known cases into nine.
- The file-writing tool turns `\u00a0` escapes in Go source into raw
  U+00A0 bytes. Check pinned no-break spaces with
  `grep -c 'u00a0'` and convert them back to escapes.
