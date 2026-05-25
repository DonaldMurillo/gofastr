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
small, _ := img.Resize(320, 0).WebP(image.WebPOptions{Lossless: true}).Bytes()
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
| WebP   | ✅ | ✅ | Lossless (VP8L). Lossy returns `ErrFormatUnsupported`. |
| HEIC / AVIF | ❌ | ❌ | Out of scope (no pure-Go codec exists) |

Animated input, ICC profiles, and EXIF data beyond orientation are
intentionally out of scope.

## Transformations

```go
img.Resize(width, height, opts...)  // ResizeOption: WithFilter, WithFit, WithoutEnlargement
img.Rotate(degrees)                  // 0 / 90 / 180 / 270 (clockwise)
img.Flip()                           // mirror top↔bottom
img.Flop()                           // mirror left↔right
img.Modulate(image.Modulation{
    Brightness: 1.2,                 // 1.0 = identity
    Saturation: 0.8,                 // 0.0 = grayscale, 1.0 = identity
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
| `WebP(WebPOptions{Lossless})` | Lossless-only; lossy errors |

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

for _, v := range result.Variants {
    _ = storage.Put(v.Name, v.Bytes)         // → battery/storage
}
saveBlurHashColumn(entityID, result.BlurHash) // → entity column
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

Inputs whose reported `width × height` exceed
`Config.MaxPixels` (default 268 MP) return `ErrDecompressionBomb`
before any pixel decoding is attempted. Override per-call:

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

## Common mistakes

- **Calling `BlurHash` on the original size.** The algorithm is
  O(W × H × xComp × yComp). Always `Resize` to a small box (e.g.
  `32 × 24`) first.
- **Expecting WebP-lossy / HEIC / AVIF to work.** They return
  `ErrFormatUnsupported`. There is no pure-Go encoder for AV1, HEVC,
  or VP8 quality-competitive with libvpx — those formats need CGo and
  are out of scope for this package.
- **Expecting WebP-lossless to match `cwebp` file sizes exactly.** The
  pure-Go encoder tries five uniform predictor modes (1, 2, 11, 12, 13)
  per image and emits the smallest output, plus subtract-green and
  LZ77 + an 8-bit color cache. On synthetic gradients and repeating
  patterns we produce output **smaller than PNG** (≈0.37× on smooth
  gradients, ≈0.35× on repeating patches). On natural photos `cwebp`
  will still beat us by 30-50% because of its per-block adaptive mode
  selection (gated on a Huffman-aware cost model), cross-color
  transform, and palette path. The encoder infrastructure for
  per-block mode evaluation is in `framework/image/internal/vp8l/
  predictor.go` — `scoreModeBlock` + `chooseBlockModes` — waiting on
  a proper Huffman cost model.
- **Aliasing `*Image` across goroutines.** Chain methods return new
  `*Image` values, but the underlying pixel buffer in an `image.Image`
  is shared. If you mutate via `GoImage()`, clone first.
- **Forgetting `AutoOrient` on user-uploaded photos.** Phone cameras
  store rotated sensors with an orientation tag, not rotated pixels.
  Saving the JPEG verbatim leaves the rotation only correct in viewers
  that honour EXIF — most thumbnail renderers don't.
