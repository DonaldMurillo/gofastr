# Image processing (`framework/image`)

`framework/image` provides pure-Go image decoding, EXIF orientation normalization, resizing, cropping, format conversion (JPEG, PNG, WebP), and placeholder generation — zero CGO and zero external binaries like ImageMagick.

## Quickstart

```go
import "github.com/DonaldMurillo/gofastr/framework/image"

img, err := image.Decode(r) // auto-applies JPEG EXIF orientation
if err != nil {
	return err
}

thumb, err := img.Resize(400, 400).JPEG(85).Bytes()
```

Generate a branded gradient placeholder (useful with `uihost.WithAppIcon` before you have a logo):

```go
placeholder, err := image.NewGradient(512, 512, "#4338CA", "#0E7C86")
pngBytes, err := placeholder.PNG().Bytes()
```

## Operations

- **Decode & normalize:** reads JPEG, PNG, GIF, and WebP, automatically rotating JPEG pixels according to their EXIF orientation tag and stripping EXIF metadata (including GPS coordinates) on re-encode.
- **Resize & crop:** `Resize` (fit within bounds preserving aspect ratio), center-crop / cover thumbnails for avatars and square icons.
- **Encode:** `.JPEG(quality)`, `.PNG()`, and WebP output to `[]byte` or an `io.Writer`.

## Safety limits (image bombs)

Decoding untrusted user uploads is a classic memory-exhaustion vector (a tiny compressed PNG that expands to a 50,000 × 50,000 pixel bitmap). `framework/image` enforces bounds **before** allocating pixel buffers:
- Checks `image.DecodeConfig` dimensions first and refuses images exceeding maximum width, height, or total pixel count.
- Caps input byte reads so truncated or oversized payloads fail fast.
- Strips EXIF/ICC/comment metadata on output so user photos never leak camera location or embed polyglot payloads.

## Using with uploads and icons

- Pair `framework/image` with `schema.Image` hooks or a background `battery/queue` job to generate thumbnails and responsive variants after upload (see [Uploads](uploads.md)).
- `uihost.WithAppIcon(logoBytes)` uses `framework/image` internally to derive 32/180/192/512px PNGs and `/favicon.ico` from one source image (see [SEO](seo.md)).

## Common mistakes

- **Decoding raw `multipart.File` directly with stdlib `image.Decode`:** stdlib does not enforce pixel-dimension ceilings or apply JPEG EXIF orientation, leaving you exposed to decompression bombs and sideways phone photos.
- **Re-serving original user JPEGs without stripping EXIF:** leaks GPS coordinates and device metadata. Re-encode through `framework/image` for public avatars and photos.
- **Generating 6 thumbnail sizes synchronously in the upload handler:** resize the primary preview inline if small, and move heavier variant matrices to `battery/queue`.
- **Expecting SVG rasterization:** `framework/image` handles raster formats only; SVG uploads are rejected by `schema.Image` by default for XSS safety.
