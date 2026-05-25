# Image pipeline

`framework/image` is a chainable image pipeline — decode → transform →
encode — implemented in pure Go on top of `image/jpeg`, `image/png`,
`image/gif`, and `golang.org/x/image`. No CGo, no system libraries, no
native build step. The API surface is inspired by Bun.Image; the
implementation is independent.

## Quickstart

```go
import "github.com/DonaldMurillo/gofastr/framework/image"

img, err := image.Open("photo.jpg")
if err != nil {
    return err
}

thumb, err := img.
    AutoOrient().
    Resize(800, 0, image.WithFit(image.FitInside)).
    JPEG(image.JPEGOptions{Quality: 80}).
    Bytes()
```

Chain methods return a new `*Image`, so the same source can be branched
into independent pipelines without aliasing:

```go
big, _   := img.Resize(1600, 0).JPEG().Bytes()
small, _ := img.Resize(320, 0).WebP().Bytes() // zero-value = lossless
```

## Construction

| Function | Use when |
| -------- | -------- |
| `Decode(io.Reader)` | Network or file stream input |
| `DecodeBytes([]byte)` | Already-loaded buffer |
| `Open(path)` | Filesystem path |
| `OpenFS(fs.FS, name)` | `embed.FS` or virtual filesystem |
| `FromImage(image.Image, Format)` | Pixels you generated in-process |

All decoders sniff the format from magic bytes and reject inputs whose
reported `width × height` exceeds `Config.MaxPixels` (default
`DefaultMaxPixels` = 268 MP, matching Bun.Image). Tune via
`DecodeBytesWithConfig` or `DecodeWithConfig`.

## Supported formats

| Format | Decode | Encode | Notes |
| ------ | :----: | :----: | ----- |
| JPEG   | ✅ | ✅ | EXIF orientation parsed for `AutoOrient` |
| PNG    | ✅ | ✅ | Compression level configurable |
| GIF    | ✅ | ✅ | First frame on animated input; 1..256 palette colours |
| BMP    | ✅ | ✅ | — |
| TIFF   | ✅ | ✅ | Compression + predictor configurable |
| WebP   | ✅ | ✅ | Lossless (VP8L). `WebPOptions{Lossy: true}` returns `ErrFormatUnsupported`. Encode dimension cap: 16384×16384. |
| HEIC / AVIF | ❌ | ❌ | Out of scope (no pure-Go codec exists) |

Animated input, ICC profiles, and EXIF data beyond orientation are
intentionally out of scope.

## Transformations

```go
img.Resize(width, height, opts...)  // ResizeOption: WithFilter, WithFit, WithoutEnlargement
img.Rotate(degrees)                  // 0 / 90 / 180 / 270 (clockwise)
img.Flip()                           // mirror top↔bottom
img.Flop()                           // mirror left↔right
// Modulation fields are *float64 so the zero-value Modulation{}
// unambiguously means "no change", and a literal Saturation: 0
// unambiguously means grayscale. Use image.Float64 to construct.
img.Modulate(image.Modulation{
    Brightness: image.Float64(1.2), // 1.0 = identity; nil = unchanged
    Saturation: image.Float64(0.8), // 0.0 = grayscale; nil = unchanged
})
img.AutoOrient()                     // apply EXIF orientation, then clear it
```

### Resize filters

`x/image/draw` ships four kernels; this package exposes them with the
familiar Bun.Image / Sharp naming where it applies:

| Filter | Backed by | When to use |
| ------ | --------- | ----------- |
| `Lanczos3` | `draw.CatmullRom` | Default. Highest quality available pure-Go. |
| `Lanczos2` | `draw.BiLinear`   | Faster, mildly softer. |
| `CatmullRom` | `draw.CatmullRom` | Same as `Lanczos3`. |
| `BiLinear` | `draw.BiLinear`     | Fast, soft. |
| `ApproxBiLinear` | `draw.ApproxBiLinear` | Fastest; visible aliasing. |
| `Nearest` | `draw.NearestNeighbor` | Pixel art, exact down-sampling. |

There is no native Lanczos kernel because the Go team's `x/image` does
not ship one. `Lanczos3` is an alias for `CatmullRom` — visually similar
at typical photo content.

### Fit modes

```go
img.Resize(800, 600, image.WithFit(image.FitFill))     // default; may distort
img.Resize(800, 600, image.WithFit(image.FitInside))   // preserve aspect; fit within
img.Resize(800, 600, image.WithFit(image.FitOutside))  // preserve aspect; overflow
```

`WithoutEnlargement()` skips the resize entirely when the target box
would scale up the source on either axis.

## Encoders

Terminal methods on `*Image` return a configured `*Encoder`. Materialise
the output with `Bytes`, `Write(io.Writer)`, `Base64`, or `DataURL`.

```go
data,   err := img.JPEG(image.JPEGOptions{Quality: 80}).Bytes()
err          = img.PNG().Write(httpRespWriter)
b64,    err := img.GIF(image.GIFOptions{NumColors: 64}).Base64()
durl,   err := img.BMP().DataURL()
```

Per-format option structs:

| Method | Options |
| ------ | ------- |
| `JPEG(JPEGOptions{Quality: 1..100})` | `Quality` default 80 |
| `PNG(PNGOptions{Compression})` | `image/png.CompressionLevel` |
| `GIF(GIFOptions{NumColors})` | 1..256, default 256 |
| `BMP()` | — |
| `TIFF(TIFFOptions{Compression, Predictor})` | from `x/image/tiff` |
| `WebP(WebPOptions{})` | Zero-value lossless; `Lossy: true` errors |

Inspect output before materialising via `Encoder.MIME()` and
`Encoder.Format()`.

## Plug-and-play: VariantSet → PipelineImage

For the common case — "take this upload, produce three sizes plus a
placeholder, hand it to the UI" — there's a declarative helper. The
`VariantSet` is headless (no UI/HTTP dependency); pair it with the
`ui.PipelineImage` component to render.

```go
result, err := image.VariantSet{
    BaseName: "hero",
    Variants: []image.Variant{
        {Width:  320, Format: image.FormatJPEG, Quality: 80, Suffix: "sm"},
        {Width:  800, Format: image.FormatJPEG, Quality: 82, Suffix: "md"},
        {Width: 1600, Format: image.FormatJPEG, Quality: 85, Suffix: "lg"},
        {Width:  320, Format: image.FormatWebP, Suffix: "sm"}, // VP8L
        {Width:  800, Format: image.FormatWebP, Suffix: "md"},
        {Width: 1600, Format: image.FormatWebP, Suffix: "lg"},
    },
    Placeholder: &image.PlaceholderOptions{Width: 24},
    BlurHashX:   4, BlurHashY: 3,
}.Process(img)

// core/upload.Storage is io.Reader-shaped: Save(ctx, key, r).
for _, v := range result.Variants {
    _ = store.Save(ctx, v.Name, bytes.NewReader(v.Bytes))
}
saveBlurHashColumn(entityID, result.BlurHash) // → entity column
```

For high-throughput uploads, use **`ProcessTo`** so only one variant
sits in memory at a time:

```go
sr, err := image.VariantSet{ /* same fields */ }.ProcessTo(img,
    func(h image.VariantHeader, r io.Reader) error {
        return store.Save(ctx, h.Name, r)
    })
// sr.Placeholder and sr.BlurHash carry the metadata; no Bytes buffered.
```

Render the result via `framework/ui`:

```go
ui.PipelineImage(ui.PipelineImageConfig{
    Fallback: "/uploads/hero-md.jpg",
    Alt:      "Sunset over the ocean",
    Width:    800, Height: 600,
    Sources: []ui.PipelineSource{
        {URL: "/uploads/hero-sm.webp",  Width:  320, Type: "image/webp"},
        {URL: "/uploads/hero-md.webp",  Width:  800, Type: "image/webp"},
        {URL: "/uploads/hero-lg.webp",  Width: 1600, Type: "image/webp"},
        {URL: "/uploads/hero-sm.jpg",   Width:  320, Type: "image/jpeg"},
        {URL: "/uploads/hero-md.jpg",   Width:  800, Type: "image/jpeg"},
        {URL: "/uploads/hero-lg.jpg",   Width: 1600, Type: "image/jpeg"},
    },
    Placeholder: result.BlurHash,
    Sizes:       "(min-width: 1024px) 1024px, 100vw",
})
```

`PipelineImage` emits one `<source type="…" srcset="…">` per distinct
`Type` in input order — put the modern format first so legacy browsers
fall through to the `<img>` fallback. The `Placeholder` field accepts
either a `data:` URL (set as `data-placeholder`) or a BlurHash string
(set as `data-blurhash`); style or hydrate either as the calling page
prefers.

## Placeholders

Two placeholder strategies for above-the-fold image loading:

```go
// Tiny base64 data URL — a ~20×N JPEG that browsers render directly.
durl, err := img.Placeholder()  // ~500 bytes typical

durl, err := img.Placeholder(image.PlaceholderOptions{
    Width:   24,  // px (height computed from aspect)
    Quality: 50,
})
```

```go
// BlurHash: ~28-char base83 string that requires a client-side decoder.
hash, err := img.Resize(32, 0).BlurHash(4, 3)  // "LEHV6nWB2yk8…"
```

The BlurHash implementation follows the [blurha.sh][bh-spec]
reference. Resize first; the algorithm cost scales with
`width × height × components`.

[bh-spec]: https://blurha.sh

## Decompression-bomb guard

Inputs whose reported `width × height` exceed `Config.MaxPixels`
(default 268 MP, matches Bun.Image) return `ErrDecompressionBomb`
before any pixel decoding is attempted. Note the WebP-lossless
encoder has a per-dimension cap of 16384 (so 268 MP can be a
16384×16384 square but a 32768×8192 ribbon is encode-rejected even
though it fits within MaxPixels). Override the guard per-call:

```go
img, err := image.DecodeBytesWithConfig(data, image.Config{
    MaxPixels: 64 * 1024 * 1024, // 64 MP
})
```

## EXIF orientation

Decoding a JPEG records the EXIF orientation tag (`1..8`) on the
`*Image`. `Metadata().Orientation` exposes it; `AutoOrient()` applies it
and resets the tag. Only the orientation tag is parsed — full EXIF
support is intentionally out of scope.

**Caveat:** an `*Image` built via `FromImage(...)` carries
`Orientation = 0`, so `AutoOrient()` is a no-op on it. For EXIF
handling, route through `Decode`/`Open`/`OpenFS`.

## Importing alongside the stdlib `image` package

The package name `image` collides with `std/image`. Files inside
this package use `stdimage "image"`. Callers that need both should
alias one side, typically the framework one:

```go
import (
    "image" // stdlib
    fwimage "github.com/DonaldMurillo/gofastr/framework/image"
)
```

## Avatar upload recipe

The typical "user uploads a photo → produce variants → store → render"
flow is goroutine-safe end-to-end (every Encoder caches its output via
`sync.Once`; every `ProcessTo` reader is one-shot). A complete sketch:

```go
import (
    "github.com/DonaldMurillo/gofastr/battery/storage" // in-memory or S3 storage
    "github.com/DonaldMurillo/gofastr/framework/image"
    "github.com/DonaldMurillo/gofastr/framework/ui"
)

// 1. Decode + orient. Reject animated GIFs (avatar = still image).
img, err := image.Decode(r)
if err != nil { /* ... */ }
img = img.AutoOrient()

// 2. Generate variants and stream to storage. ProcessTo streams one
//    encoded buffer at a time and clamps to source width so a tiny
//    upload doesn't fanout into 16× upscaled storage waste.
store := storage.NewMemoryStorage() // or NewLocalStorage / NewS3Storage
set := image.VariantSet{
    RejectAnimated: true,           // ErrAnimatedSource if FrameCount > 1
    BaseName:       userID,
    Variants: []image.Variant{
        {Width:  320, Format: image.FormatJPEG, Quality: 80, Suffix: "sm"},
        {Width:  800, Format: image.FormatJPEG, Quality: 82, Suffix: "md"},
        {Width: 1600, Format: image.FormatWebP, Suffix: "lg"},
    },
    Placeholder: &image.PlaceholderOptions{Width: 24},
    BlurHashX:   4, BlurHashY: 3,
}
headers := []ui.HeaderInfo{}
sr, err := set.ProcessTo(img, func(h image.VariantHeader, r io.Reader) error {
    if err := store.Save(ctx, h.Name, r); err != nil { return err }
    headers = append(headers, ui.HeaderInfo{
        Name: h.Name, Width: h.Width, Height: h.Height, MIME: h.MIME,
    })
    return nil
})

// 3. Render with PipelineImage. PipelineSourcesFromHeaders turns the
//    typed headers into the responsive <source> list.
picture := ui.PipelineImage(ui.PipelineImageConfig{
    Fallback: "/uploads/" + headers[1].Name,
    Alt:      "Avatar",
    Width:    headers[1].Width, Height: headers[1].Height,
    Sources: ui.PipelineSourcesFromHeaders(headers, func(name string) string {
        return "/uploads/" + name
    }),
    Placeholder: sr.BlurHash, // or sr.Placeholder for a data: URL
})
```

`VariantSink`'s `r` is one-shot — stash it for a later goroutine and
the next read returns `ErrReaderClosed`. Drain inside the sink (e.g.,
hand it directly to `storage.Save`).

## Performance notes

- **5-pass VP8L encode**: `WebP().Bytes()` runs every uniform predictor
  mode (1, 2, 11, 12, 13) and ships the smallest. On a 256² photo
  that's ~20 ms / 29 MB of allocations — ~6× slower than a single-mode
  pass. For high-volume hot paths, prefer JPEG (~1 ms) and reserve
  WebP-lossless for low-throughput admin / dashboard flows.
- **`isUniform` short-circuit**: solid-color inputs encode in one
  pass instead of five (~10 ms vs ~50 ms for 1024²). Near-uniform
  inputs with one off-pixel still pay the full 5-pass.
- **BlurHash auto-resizes** to 64 px on the longest side internally;
  callers do not need to pre-Resize.
- **`ProcessTo` releases resize intermediates** between variants so
  peak heap stays near one variant's worth, not all variants summed.
- **`Modulate` fast-paths** `*image.NRGBA` and `*image.RGBA`; for
  other concrete types the slow per-pixel `At()` path applies.

## Common mistakes

- **Calling `BlurHash` on the original size.** The algorithm is
  O(W × H × xComp × yComp). Always `Resize` to a small box (e.g.
  `32 × 24`) first.
- **Expecting WebP-lossy / HEIC / AVIF to work.** They return
  `ErrFormatUnsupported`. There is no pure-Go encoder for AV1, HEVC,
  or VP8 quality-competitive with libvpx — those formats need CGo and
  are out of scope for this package.
- **Expecting WebP-lossless to match `cwebp` file sizes.** The pure-Go
  encoder tries five uniform predictor modes (1, 2, 11, 12, 13) per
  image and emits the smallest output, plus subtract-green and LZ77
  + an 8-bit color cache. Honest comparison against `cwebp -z 9`
  (libwebp 1.6) and `png.BestCompression`:

  | Content (256×256) | PNG-best | Ours WebP-LL | `cwebp -z 9` | ours vs PNG | ours vs cwebp |
  | --- | ---: | ---: | ---: | ---: | ---: |
  | smooth gradient | 579 | 282 | 76 | **0.49×** | **3.71×** |
  | repeating patches | ~800 | 352 | 148 | **0.44×** | **2.38×** |
  | natural photo | ~110k | 125.5k | 111.7k | 1.14× | **1.12×** |
  | white noise | ~197k | 196.8k | 196.7k | 1.00× | 1.00× |

  The framing: **we beat PNG** on smooth and structured content by
  2-3×; **`cwebp` beats us** by another 2-4× on the same content (its
  per-block adaptive mode + cross-color + palette path) but only by
  ~12% on natural photos and ~0% on noise. For PNG-replacement
  delivery in the framework's UI pipeline, our output is competitive;
  for "smallest-possible WebP" you'd still go to `cwebp`.

  The encoder infrastructure for per-block mode evaluation is in
  `framework/image/internal/vp8l/predictor.go` — `scoreModeBlock` +
  `chooseBlockModes` — waiting on a proper Huffman cost model.
- **Aliasing `*Image` across goroutines.** Chain methods return new
  `*Image` values, but the underlying pixel buffer in an `image.Image`
  is shared. If you mutate via `GoImage()`, clone first.
- **Forgetting `AutoOrient` on user-uploaded photos.** Phone cameras
  store rotated sensors with an orientation tag, not rotated pixels.
  Saving the JPEG verbatim leaves the rotation only correct in viewers
  that honour EXIF — most thumbnail renderers don't.
