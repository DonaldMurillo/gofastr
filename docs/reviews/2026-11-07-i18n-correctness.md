# i18n correctness review — November 7, 2026

Reviewed revision: `953ccc08256e825bdc17254c463a5695a27092b6`.
This records an independently reproduced functional finding, not a fix or a
release-readiness assessment.

## Confirmed — P2: accepted placeholders render differently in Go and JavaScript

Locations: `core/i18n/i18n.go:844`, `core/i18n/check.go:170`, and
`core-ui/runtime/frag/kernel.js:212`. The corresponding assembled JavaScript
is in `core-ui/runtime/runtime.js:220`.

An app-defined phrase can contain `{{ name }}` or `{{user-name}}`. When the
translation uses the same placeholder, `CheckCatalog` accepts it and
`runtime.StringsJSON` exports it. `Phrase.Text` fills both spellings in Go,
but the actual browser `say` implementation leaves both as literal text.
The canonical `{{name}}` control renders identically in both implementations.

Normal public call path: `i18n.Define` → `MapCatalog.Set` →
`runtime.RegisterStrings` → `runtime.StringsJSON` → runtime `say`.
No malformed key or external service is needed. The visible consequence is
an unresolved placeholder after a client-side update of an app-defined phrase.
The reproduction does not establish that any shipped built-in phrase uses
these spellings.

The checker intentionally treats spaces as part of the name, while Go's
`fill` trims them and JavaScript's `say` accepts only word-character names.
Choose one supported grammar and make interpolation, validation, and the
server-to-client export agree; do not simply remove the existing checker test
without deciding the contract.

### Reproduce using the public Go API and the actual JavaScript function

Run from the repository root. Save the first code block to a temporary
`main.go`, run it with `go run main.go > result.json`, and save the second to
`check.cjs`. Run `node check.cjs result.json` from the repository root.
The JavaScript executes the extracted runtime function in Node; this is not
a full browser or DOM test.

```go
package main

import (
	"context"
	"encoding/json"
	"os"

	"github.com/DonaldMurillo/gofastr/core-ui/runtime"
	"github.com/DonaldMurillo/gofastr/core/i18n"
)

func main() {
	catalog := i18n.NewMapCatalog()
	ctx := i18n.WithTranslator(i18n.WithContext(context.Background(), i18n.Locale{Tag: "fr"}), i18n.NewTranslator(catalog, "en"))
	params := map[string]any{"name": "Ada", "user-name": "Ada"}
	cases := []map[string]any{}
	for index, token := range []string{"name", " name ", "user-name"} {
		key := []string{"review.parity.canonical", "review.parity.spaced", "review.parity.hyphen"}[index]
		phrase := i18n.Define(key, "Hello, {{"+token+"}}!")
		catalog.Set("fr", key, i18n.Message{Text: "Bonjour, {{"+token+"}} !"})
		runtime.RegisterStrings(phrase)
		cases = append(cases, map[string]any{"key": key, "english": phrase.English(), "server": phrase.Text(ctx, params), "params": params})
	}
	result := map[string]any{
		"cases": cases,
		"strings_json": json.RawMessage(runtime.StringsJSON(ctx)),
		"catalog_issue_count": len(i18n.CheckCatalog(catalog, "fr")),
	}
	if err := json.NewEncoder(os.Stdout).Encode(result); err != nil {
		panic(err)
	}
}
```

```javascript
const fs = require('node:fs');
const vm = require('node:vm');
const result = JSON.parse(fs.readFileSync(process.argv[2], 'utf8'));
const source = fs.readFileSync('core-ui/runtime/frag/kernel.js', 'utf8');
const start = source.indexOf('  const say = (k, en, p) => {');
const end = source.indexOf('\n  };', start);
if (start < 0 || end < 0) throw new Error('Cannot locate runtime say');
const say = vm.runInNewContext(source.slice(start, end + '\n  };'.length) + '\n say;', {
  _says: result.strings_json,
  own: (object, property) => Object.prototype.hasOwnProperty.call(object, property),
});
for (const entry of result.cases) {
  entry.client = say(entry.key, entry.english, entry.params);
  entry.matches = entry.client === entry.server;
}
if (result.catalog_issue_count !== 0) throw new Error('Catalog rejected');
if (!result.cases[0].matches) throw new Error('Canonical control failed');
if (result.cases[1].matches || result.cases[2].matches) throw new Error('Mismatch not reproduced');
console.log(JSON.stringify(result.cases, null, 2));
```

Expected reproduction: the first case has `matches: true`; the other two
have `matches: false`. Those assertions establish the discrepancy, not
correct behavior to preserve in a regression test after a fix.

## Observed, not confirmed as a bug — blank translations

A separate public-API probe loaded JSON containing an empty text value and
a plural with an empty `one` form. Both rendered `""`, despite an available
English fallback. `CheckCatalog`, including `Require` for the probe prefix,
reported no issue when the source phrases had no placeholders.

The existing missing-plural-form regression guarantees fallback for an absent
form, not necessarily for a present, deliberately empty form. The documented
catalog interface distinguishes key presence from absence and does not
explicitly prohibit intentional blank translations. Consequently, execution
confirms the behavior but does not establish the reviewer's proposed contract.
Decide whether blank values are valid before treating this as a shipping bug
or changing fallback behavior.

## Independently run checks

- `go test ./core/config/ ./core/settings/ ./core/dotenv/ ./core/i18n/ -count=1`
- `go test ./core-ui/runtime/hosttest/ -count=1 -run '^(TestRuntimeSayCallsMatchPhrases|TestStringsJSONCarriesOnlyTranslations)$'`
- `go vet ./core/config/ ./core/settings/ ./core/dotenv/ ./core/i18n/`

The existing tests can pass while the cross-runtime discrepancy remains.
No production implementation was changed by this review.
