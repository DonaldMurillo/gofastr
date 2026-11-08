# Entity UI correctness review — November 7, 2026

Reviewed revision: `953ccc08256e825bdc17254c463a5695a27092b6`.

## C03 — P2: required single-option enum replaces the entire form with an error

Root: `framework/entityui/record_form.go:775` chooses a segmented control
for any required enum with at most four values. That includes one value,
but `framework/ui/segmented.go:91` requires at least two options and panics.
The record renderer catches the panic and returns the generic failure callout
instead of the record/create form.

Single-option enums are a supported declaration: `framework/entity/entity.go:1094`
rejects empty `Values` only, and `TestEnumNeedsValues` explicitly accepts
a one-value enum both with and without `Required`.

Normal call path: register an entity with a required enum containing
`Values: []string{"only"}` → configure the entity UI with an API path →
`UI.Record(name, id).RenderCtx(ctx)` for an existing record or an empty id
for the create screen. No unusual data, invalid configuration or browser is needed.

### Independently executed evidence

- Required single option, with a default: record and create both produce
  a 266-byte danger callout, with neither the enum nor an unrelated title input.
- Required single option, without a default: the same failure on both screens.
- Optional single option: both inputs appear and there is no failure callout.
- Required two options: both inputs appear and there is no failure callout.

These are four configurations, eight render calls, and one root cause.
The defect prevents use of the affected forms; it does not delete stored data.
No production fix is included.

### Retained reproduction

The following Go overlay test reuses the package's existing isolated SQLite
test fixture but exercises the real public record/create builders. It is a
fresh coordinator probe, independent of the reviewer's logging-only probe.
Its assertions establish today's discrepancy, not behavior to preserve after a fix.
Node, a browser, and external services are not required.

Save the Go block to a temporary `enum_test.go`, then use the shell block below.
An overlay adds the test virtually; it does not write into the checkout.

```go
package entityui

import (
    "strings"
    "testing"

    "github.com/DonaldMurillo/gofastr/core/schema"
    "github.com/DonaldMurillo/gofastr/framework/entity"
)

func TestCoordinatorEnumScreenReproduction(t *testing.T) {
    cases := []struct {
        name string
        required bool
        values []string
        defaultValue any
        broken bool
    }{
        {"single_required_default", true, []string{"only"}, "only", true},
        {"single_required_no_default", true, []string{"only"}, nil, true},
        {"single_optional", false, []string{"only"}, nil, false},
        {"two_required", true, []string{"only", "other"}, "only", false},
    }
    for _, testCase := range cases {
        t.Run(testCase.name, func(t *testing.T) {
            config := entity.EntityConfig{
                Table: testCase.name,
                Fields: []schema.Field{
                    {Name: "title", Type: schema.String, Required: true},
                    {Name: "stage", Type: schema.Enum, Required: testCase.required, Values: testCase.values, Default: testCase.defaultValue},
                },
            }.WithTimestamps(false)
            fixture := newTestUI(t, map[string]entity.EntityConfig{testCase.name: config},
                map[string][]map[string]any{testCase.name: {{"id": "row1", "title": "Example", "stage": "only"}}},
                withAPI(map[string]string{testCase.name: "/api/"+testCase.name}))
            for _, recordID := range []string{"row1", ""} {
                screen := "record"
                if recordID == "" { screen = "create" }
                body := string(fixture.ui.Record(testCase.name, recordID).Base("/records").RenderCtx(fixture.userCtx("/records/"+screen, "", "user1")))
                fieldPresent := strings.Contains(body, `name="stage"`)
                titlePresent := strings.Contains(body, `name="title"`)
                failed := strings.Contains(body, "fui-callout--danger")
                t.Logf("%s: enum_input=%v title_input=%v error_callout=%v bytes=%d", screen, fieldPresent, titlePresent, failed, len(body))
                if testCase.broken {
                    if !failed || fieldPresent || titlePresent { t.Fatalf("Failure did not reproduce: %s", body) }
                } else if failed || !fieldPresent || !titlePresent {
                    t.Fatalf("Control form failed: %s", body)
                }
            }
        })
    }
}
```

```sh
probe="$(mktemp -d /tmp/gofastr-enum-retained.XXXXXX)"
awk '/^```go$/{inside=1;next} /^```$/{inside=0} inside' \
  docs/reviews/2026-11-07-entityui-correctness.md > "$probe/enum_test.go"
printf '{"Replace":{"%s/framework/entityui/zz_coordinator_enum_test.go":"%s/enum_test.go"}}\n' \
  "$PWD" "$probe" > "$probe/overlay.json"
go test -p 2 -count=1 -overlay "$probe/overlay.json" \
  -run '^TestCoordinatorEnumScreenReproduction$' -v ./framework/entityui/
```

The four expected panic-containment logs belong only to the two broken
configurations. All eight assertions must pass for the discrepancy and its
controls to be reproduced.

### Existing checks

- `go test -p 2 -count=1 ./framework/entityui/`
- `go test -p 2 -count=1 ./framework/entity/ -run '^TestEnumNeedsValues$'`
- `go test -p 2 -count=1 ./framework/ui/ -run '^TestSegmented'`
- `go vet -p 2 ./framework/entityui/`

Passing existing tests do not cover this schema-to-control boundary.
Once fixing it, add a regression requiring both forms to render their fields
for a required one-option enum, and retain the optional/two-option controls.
