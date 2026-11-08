# Email template candidate adjudication — November 7, 2026

Reviewed revision: `953ccc082`. Status: **not counted**. The behavior
is reproduced, but the reviewer's proposed always-strip contract is not established.

## Candidate: duplicate subject sources leave a marker in body text

`battery/email/template.go:193` extracts a leading `Subject: ` line only
when no subject has already been selected. A `.subject` file takes priority
over inline subject extraction; a text subject prevents later extraction
from the HTML body. A non-selected leading marker therefore stays in its body.

Independent `LoadFromFS` → `Execute` probes reproduce the observation:
- Text-only and HTML-only fixtures extract the marker and render the body.
- Paired text/HTML markers select the text subject and leave the HTML marker.
- A `.subject` file wins over text or HTML markers, which remain in the body.

## Why it is not a confirmed bug

`LoadFromDir` documents the HTML subject as a fallback when no text file
is present, and a `.subject` file as an alternative. The lower-level
`stripSubjectLine` comment explicitly conditions extraction on an empty subject.
The public email guide likewise describes the text marker or a sibling
subject file, not unconditional removal of every body line beginning `Subject: `.

The original claim extrapolated a new rule from single-source controls.
Those controls do not establish how duplicate sources must be treated.
Removing body lines indiscriminately could also remove intentional content
when the author has separately supplied the subject. Decide that contract
before proposing such a change; do not count it toward twenty findings.

## Retained observation probe

Save the block to a temporary `main.go` and run `go run /path/to/main.go`
from the repository root. No network, filesystem writes, or email sending occurs.

```go
package main

import (
    "fmt"
    "testing/fstest"

    "github.com/DonaldMurillo/gofastr/battery/email"
)

func main() {
    fixtures := []struct { name string; files map[string]string }{
        {"text_only", map[string]string{"sample.txt": "Subject: Text subject\nBody for {{.Name}}" }},
        {"html_only", map[string]string{"sample.html": "Subject: HTML subject\n<p>Body for {{.Name}}</p>" }},
        {"paired_markers", map[string]string{"sample.txt": "Subject: Text subject\nText body", "sample.html": "Subject: HTML subject\n<p>HTML body</p>" }},
        {"subject_plus_text_marker", map[string]string{"sample.subject": "Chosen subject", "sample.txt": "Subject: Text subject\nText body" }},
        {"subject_plus_html_marker", map[string]string{"sample.subject": "Chosen subject", "sample.html": "Subject: HTML subject\n<p>HTML body</p>" }},
    }
    for _, fixture := range fixtures {
        filesystem := fstest.MapFS{}
        for name, contents := range fixture.files {
            filesystem[name] = &fstest.MapFile{Data: []byte(contents)}
        }
        templates, err := email.LoadFromFS(filesystem)
        if err != nil { panic(err) }
        result, err := email.Execute(templates["sample"], map[string]any{"Name": "Ada"})
        if err != nil { panic(err) }
        fmt.Printf("%s: subject=%q text=%q html=%q\n", fixture.name, result.Subject, result.TextBody, result.HTMLBody)
    }
}
```

Observed output:

```text
text_only: subject="Text subject" text="Body for Ada" html=""
html_only: subject="HTML subject" text="" html="<p>Body for Ada</p>"
paired_markers: subject="Text subject" text="Text body" html="Subject: HTML subject\n<p>HTML body</p>"
subject_plus_text_marker: subject="Chosen subject" text="Subject: Text subject\nText body" html=""
subject_plus_html_marker: subject="Chosen subject" text="" html="Subject: HTML subject\n<p>HTML body</p>"
```

## Independent validation

Existing loader and execution tests:
`go test -p 2 -count=1 ./battery/email/ -run '^(TestLoadFrom.*|TestExecute.*)$'`.

The broader search/notification review reported no other concrete candidates.
Its differential and ranking probes are reviewer evidence, not independently
rerun coordinator coverage. No production implementation changed.
