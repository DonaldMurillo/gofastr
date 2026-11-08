# Cache and image correctness review — November 7, 2026

Independently reproduced at `953ccc08256e825bdc17254c463a5695a27092b6`.
Two root causes, both P3. No production fixes are included.

## C06 — P3: Vary variants overwrite the same cache slot

Root: `battery/cache/middleware.go:195`, together with the base key at
`:141` / `:222`. Request variant headers are stored inside the cached value
and checked on lookup, but they do not distinguish storage keys.

The public middleware contract at `middleware.go:88` promises a key that
includes every header named by Vary, so representations do not collide.
An ordinary theme-varying page contradicts that contract:

- Requests: `light`, `light`, `dark`, `light`, all for the same GET URL.
- Actual X-Cache sequence: `MISS`, `HIT`, `MISS`, `MISS`.
- The origin runs three times and the cache contains one entry.
- Independent variant entries would retain light after storing dark, giving
  `MISS`, `HIT`, `MISS`, `HIT` and two origin executions.

The reproduction uses an hour-long TTL, capacity for 32 entries, no sleeping,
and only two variants. Expiry and capacity eviction are not explanations.
Every response body is correct: this is variant retention/cache thrashing,
not a demonstrated wrong-content response or data leak. The severity is low;
a cache without a variant-retention contract could choose a single-slot policy,
but that is not the keying behavior this public middleware documents.
The observation is an executable caching consequence, not just a wording nit.

## C07 — P3: auto-height FitInside can discard the requested width

Root: `framework/image/geometry.go:134` computes an auto-derived dimension
with integer truncation before `:139` applies the fit calculation. A derived
height of zero makes FitInside's scale zero on both axes; the final clamp
then turns both dimensions into one.

`Resize(16, 0, WithFit(FitInside))` is documented to derive height from width.
`Placeholder` documents its default target width of 16 and calls that path.
Normal wide banners therefore receive a square placeholder instead of the
requested-width image:

- Source 1920×36: resize 1×1, decoded JPEG placeholder 1×1; expected 16×1.
- Source 2000×3: the same discrepancy.
- Exact 16:1 control, 2000×125: resize and placeholder 16×1.
- Ordinary 4:1 control, 640×160: resize and placeholder 16×4.
- FitFill and FitOutside controls retain the requested dimensions on every
  source above.

Both direct resize and encoded-placeholder symptoms share one root cause
and count once. All fixtures are small valid in-memory images. No files
are uploaded and no image viewer/browser is involved.

## Retained public-API reproduction

Save this block to a temporary `main.go` and run `go run /path/to/main.go`
from the repository root. Its assertions reproduce current discrepancies
and successful controls; invert the bug expectations when writing fix regressions.

```go
package main

import (
    "bytes"
    "encoding/base64"
    "fmt"
    stdimage "image"
    "image/jpeg"
    "net/http"
    "net/http/httptest"
    "slices"
    "strings"
    "time"

    "github.com/DonaldMurillo/gofastr/battery/cache"
    frameworkimage "github.com/DonaldMurillo/gofastr/framework/image"
)

func checkCache() {
    memory := cache.NewMemoryCache(cache.WithTTL(time.Hour), cache.WithMaxEntries(32))
    defer memory.Close()
    calls := 0
    origin := http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
        calls++
        response.Header().Set("Vary", "X-Theme")
        fmt.Fprint(response, "theme="+request.Header.Get("X-Theme"))
    })
    handler := cache.CacheMiddleware(memory, time.Hour)(origin)
    var states []string
    for _, theme := range []string{"light", "light", "dark", "light"} {
        request := httptest.NewRequest(http.MethodGet, "https://fixture.invalid/page", nil)
        request.Header.Set("X-Theme", theme)
        response := httptest.NewRecorder()
        handler.ServeHTTP(response, request)
        if response.Code != http.StatusOK || response.Body.String() != "theme="+theme { panic("Response body/status control failed") }
        states = append(states, response.Header().Get("X-Cache"))
    }
    fmt.Printf("Vary sequence=%v origin_calls=%d entries=%d; documented independent variants would need two origin calls\n", states, calls, memory.Len())
    if !slices.Equal(states, []string{"MISS", "HIT", "MISS", "MISS"}) || calls != 3 { panic("Reported cache discrepancy did not reproduce") }
    if memory.Len() != 1 { panic("Single-slot storage discrepancy did not reproduce") }
}

func checkImages() {
    cases := []struct { width, height, expectedWidth, expectedHeight int; broken bool }{
        {1920, 36, 16, 1, true},
        {2000, 3, 16, 1, true},
        {2000, 125, 16, 1, false},
        {640, 160, 16, 4, false},
    }
    for _, testCase := range cases {
        source := frameworkimage.FromImage(stdimage.NewRGBA(stdimage.Rect(0, 0, testCase.width, testCase.height)), frameworkimage.FormatPNG)
        inside := source.Resize(16, 0, frameworkimage.WithFit(frameworkimage.FitInside)).Bounds()
        placeholder, err := source.Placeholder()
        if err != nil { panic(err) }
        parts := strings.SplitN(placeholder, ",", 2)
        if len(parts) != 2 { panic("Bad placeholder URL") }
        data, err := base64.StdEncoding.DecodeString(parts[1])
        if err != nil { panic(err) }
        decoded, err := jpeg.DecodeConfig(bytes.NewReader(data))
        if err != nil { panic(err) }
        fmt.Printf("source=%dx%d inside=%dx%d placeholder=%dx%d expected=%dx%d\n", testCase.width, testCase.height, inside.Dx(), inside.Dy(), decoded.Width, decoded.Height, testCase.expectedWidth, testCase.expectedHeight)
        if testCase.broken {
            if inside.Dx() != 1 || inside.Dy() != 1 || decoded.Width != 1 || decoded.Height != 1 { panic("Reported geometry discrepancy did not reproduce") }
        } else if inside.Dx() != testCase.expectedWidth || inside.Dy() != testCase.expectedHeight || decoded.Width != testCase.expectedWidth || decoded.Height != testCase.expectedHeight {
            panic("Aspect-ratio control failed")
        }
        for _, fit := range []frameworkimage.Fit{frameworkimage.FitFill, frameworkimage.FitOutside} {
            control := source.Resize(16, 0, frameworkimage.WithFit(fit)).Bounds()
            if control.Dx() != testCase.expectedWidth || control.Dy() != testCase.expectedHeight { panic("Other-fit control failed") }
        }
    }
}

func main() { checkCache(); checkImages() }
```

## Scope and independently run checks

- `go test -p 2 -count=1 ./battery/cache/ ./battery/storage/ ./framework/file/ ./framework/image/`
- `go vet -p 2 ./battery/cache/ ./framework/image/`

Redis/S3 runtime behavior was not exercised. The cache observation uses the
public middleware and memory backend through httptest; image evidence uses
public Resize/Placeholder calls and decoding the actual JPEG data URL.
