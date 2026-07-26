# File uploads

CRUD endpoints accept `multipart/form-data` for entities with `Image`
or `File` fields. Uploads are streamed through the configured
`upload.Storage` backend; only the resulting URL is stored on the
record.

## Wire it up

```go
import "github.com/DonaldMurillo/gofastr/core/upload"

storage := upload.NewLocalStorage("./uploads")
app := framework.NewApp(
    framework.WithDB(db),
    framework.WithFileStorage(storage),
)
app.Entity("users", framework.EntityConfig{
    Fields: []schema.Field{
        {Name: "name",   Type: schema.String, Required: true},
        {Name: "avatar", Type: schema.Image},
        {Name: "resume", Type: schema.File},
    },
})
```

`WithFileStorage` is required if any entity declares an `Image` or
`File` field. Without it, multipart requests on those entities return
`server has no file storage configured`.

## Posting an upload

```bash
curl -X POST http://localhost:8080/users \
  -F 'name=Carol' \
  -F 'avatar=@/path/to/photo.png' \
  -F 'resume=@/path/to/cv.pdf'
```

The framework:

1. Parses the multipart form (up to `MaxMultipartMemory = 32 MiB` in
   memory, spills the rest to temp files).
2. Coerces non-file values to the schema field's Go type
   (`Int` → `int64`, `Bool` → `bool`, etc.).
3. Streams each file part matching an `Image`/`File` field through
   `Storage`, scoped by entity name and field name.
4. Stores the returned URL string on the record.

The record persisted to the database looks like:

```json
{ "id": "u1", "name": "Carol",
  "avatar": "/uploads/users/avatar/abc123.png",
  "resume": "/uploads/users/resume/def456.pdf" }
```

## Field-name casing

Multipart field names are **taken literally** as column names — there
is no JSON-case translation. If your entity's column is `avatar_url`,
the multipart field must be `avatar_url`, regardless of `JSONCase`
config. (JSON requests are reverse-cased; multipart is not.)

## Field types

| Type            | Wire form           | DB column     |
|-----------------|---------------------|---------------|
| `schema.Image`  | multipart file part | `TEXT` URL    |
| `schema.File`   | multipart file part | `TEXT` URL    |

The two differ in two ways: the UI host emits an image-aware widget for
`Image` fields, and only `Image` fields run the image pipeline described
below. A `File` field is any binary — a PDF, a CSV — so decoding it as an
image would fail every upload.

## Automatic renditions and placeholders

By default an upload is stored as-is: one file, one URL. Adding
`WithImagePipeline` makes every `schema.Image` upload also produce
resized renditions and a placeholder, with no per-entity upload handler:

```go
import (
    "github.com/DonaldMurillo/gofastr/framework/image"
    "github.com/DonaldMurillo/gofastr/framework/imagefield"
)

app := framework.NewApp(
    framework.WithFileStorage(store),
    framework.WithImagePipeline(imagefield.MustNew(imagefield.Config{
        Variants: []image.Variant{
            {Width: 480, Format: image.FormatWebP, Suffix: "sm"},
            {Width: 960, Format: image.FormatWebP, Suffix: "md"},
            {Width: 480, Format: image.FormatJPEG, Quality: 82, Suffix: "sm"},
            {Width: 960, Format: image.FormatJPEG, Quality: 82, Suffix: "md"},
        },
        BlurHashX: 4, BlurHashY: 3,
    })),
)
```

`imagefield` is a separate package on purpose: `framework/image` carries
every image decoder plus the WebP encoder, so importing it from the upload
path unconditionally would put all of it in the binary of every app with a
CRUD handler. Only apps that ask for the pipeline link it.

### Where the derived values go

Renditions are written through the same storage backend as the original,
into the same directory, sharing its unique base name — an original at
`products/cover/a1b2-photo.png` yields `products/cover/a1b2-photo-sm.webp`
and so on, so two uploads of `photo.png` never collide.

The metadata lands in **sibling columns**, named for the image field:

| Column                 | Type            | Contents                                    |
|------------------------|-----------------|---------------------------------------------|
| `<field>_blurhash`     | `schema.String` | ~28-char BlurHash                           |
| `<field>_placeholder`  | `schema.String` | LQIP `data:` URL (only if `Placeholder` set) |
| `<field>_variants`     | `schema.JSON`   | `[{storage_ref, mime, width, height}, …]`, ascending by width |

**Columns you do not declare are skipped.** Nothing errors, nothing is
lost — you adopt a column by adding it to the entity. So an entity with a
`cover` image field opts into the hash alone by declaring:

```go
Fields: []schema.Field{
    {Name: "cover", Type: schema.Image},
    {Name: "cover_blurhash", Type: schema.String},
}
```

### Rendering the result

`<field>_variants` maps onto a responsive `<picture>`, and
`<field>_blurhash` becomes the placeholder that paints before it:

```go
durl, _ := image.BlurHashDataURL(row.CoverBlurHash, image.BlurHashRenderConfig{})

var headers []ui.HeaderInfo
for _, v := range row.CoverVariants {
    headers = append(headers, ui.HeaderInfo{
        Name: v.StorageRef, Width: v.Width, Height: v.Height, MIME: v.MIME,
    })
}

ui.PipelineImage(ui.PipelineImageConfig{
    Fallback:    "/uploads/" + row.Cover,
    Alt:         row.Name,
    Sources:     ui.PipelineSourcesFromHeaders(headers, func(n string) string { return "/uploads/" + n }),
    Placeholder: durl,
})
```

See [image.md](image.md) for the pipeline itself and for the
BlurHash-versus-LQIP trade-off.

### Config reference

| Field            | Effect                                                      |
|------------------|-------------------------------------------------------------|
| `Variants`       | Renditions to produce and store. `Suffix` (or width) distinguishes each key. |
| `BlurHashX/Y`    | BlurHash component counts, 1..9. Both zero = no hash; setting only one is an error. 4×3 landscape, 3×4 portrait. |
| `Placeholder`    | Also store an LQIP data URL. Usually redundant next to a BlurHash — ~28 bytes versus a few hundred, and both render identically. |
| `RejectAnimated` | Fail the upload on a multi-frame source instead of silently keeping frame one. Worth setting on avatars. |
| `AllowUpscale`   | Permit renditions wider than the source. Off by default, so a small upload does not fan out into pixel-multiplied files. |
| `MaxPixels`      | Override the decompression-bomb guard (default 64 MP).      |

`New` returns an error for a config that could not produce anything, or
that the pipeline would reject at process time anyway — so wiring
mistakes surface at startup rather than on the first upload. `MustNew`
panics instead, for package-level wiring.

### Per-field configuration

One app-wide config cannot describe every image. An avatar wants portrait
BlurHash components and animated sources rejected; a hero cover wants wide
renditions. Compose a default with overrides:

```go
framework.WithImagePipeline(covers),                        // default
framework.WithImagePipelineFor("users", "avatar", avatars),  // this field only
framework.WithImagePipelineFor("posts", "attachment", nil),  // opt out entirely
```

A field with no entry falls through to the default. An explicit `nil` opts
that one field out without unpicking the app-wide config.

### Doing it yourself

The declarative wiring is a convenience over interfaces that stay open —
none of it is a closed system, and every layer below remains callable.

**Your own deriver.** `WithImagePipeline` takes a `file.ImageDeriver`, not a
concrete type. `imagefield` is one implementation; implement the interface
directly to crop to a focal point, watermark, push to a CDN, or name keys
your own way:

```go
type ImageDeriver interface {
    DeriveImage(ctx context.Context, store upload.Storage,
        data []byte, primaryRef string) (*file.ImageDerivatives, error)
}
```

Whatever you return is validated and then spread across the sibling columns
exactly as `imagefield`'s output would be, so you keep the declarative half
while owning the processing.

**Driving the upload path directly**, outside auto-CRUD — your own handler,
your own storage keys, the full `FileField` including `.Image` rather than
just the URL string:

```go
ff, err := file.ProcessFileField(ctx, store, part, filename, "products", "cover",
    file.WithImageDeriver(deriver))
// ff.Image.Variants / .BlurHash / .Placeholder
```

**Ignoring all of it** and calling the pipeline yourself. `VariantSet` is
headless and unchanged; see [image.md](image.md). Nothing about the upload
path is required to use it — generated covers, imported batches, and one-off
scripts have no upload to hang off.

**Storing the metadata somewhere else.** The sibling columns are a
convention, not a requirement. Declare none of them and the derived values
are dropped; do the persistence in a `BeforeCreate` hook, a separate table,
or a JSON blob of your own shape instead.

The one thing the framework will not do is reach into `framework/image` from
`framework/file` or `framework/crud` — that edge would link every image
codec into every app with a CRUD handler, which is why the dependency is
inverted through `ImageDeriver` in the first place.

### Scope of this seam

`ImageDeriver` is the only per-field transform the framework has, and it is
image-shaped on purpose: it runs on write, for `schema.Image` fields, and
puts its output in sibling columns. It is not a general field hook.

For anything else — normalising a string on write, masking a value on read,
composing several transforms on one field — use a `BeforeCreate`/
`BeforeUpdate` hook or an `entity.ValidatorFunc` today. Note that schema
validation runs *after* `BeforeCreate`, so a hook can normalise a value into
validity.

A general per-field middleware chain is being designed in
[issue #144](https://github.com/DonaldMurillo/gofastr/issues/144). The
blocking question there is the read half: a transform applied after the query
has already filtered and sorted on the stored value silently breaks
`?field=`, `?sort=`, `?q=`, cursor pagination, and export. Until that is
settled, `ImageDeriver` stays narrow rather than growing sideways.

### Failure behavior

A source that cannot be decoded **fails the whole request** — the row is
not written and no file is kept. That is deliberate: the column is
declared as an image, and a silent success would surface much later as a
page with no `srcset` and no placeholder, with nothing in the logs
pointing back at the upload. The original is stored before renditions are
derived, so a derive failure never leaves a half-written primary file.

Renditions stream one at a time, so peak memory stays near a single
rendition rather than all of them summed — this runs inside a request, on
bytes a client chose.

### Cost: this is synchronous

Deriving happens in the upload request, and it is CPU-bound. Measured on an
M-series laptop, from a generated source:

| Renditions                  | Source | Derive |
|-----------------------------|--------|--------|
| JPEG ×2, 1200 px source     | 45 KB  | ~53 ms |
| JPEG ×2, 3000 px source     | 196 KB | ~184 ms |
| WebP ×2 + JPEG ×2, 3000 px  | 196 KB | ~537 ms |

The jump is the WebP encoder: it is lossless-only and runs five predictor
passes, shipping the smallest (see [image.md](image.md) → Performance
notes). So:

- For a **hot upload path**, prefer JPEG renditions. Two JPEG widths plus a
  BlurHash is under 200 ms even from a large source.
- Reserve **WebP** for low-volume flows — admin uploads, one-off imports —
  or accept the half-second.
- Each upload holds a request slot for that whole time and does not
  parallelise away. If uploads are frequent, derive out-of-band instead:
  store the original in the request, then produce renditions in a
  `battery/queue` job and patch the sibling columns when it finishes. The
  pieces are the same; only the timing moves.

There is no built-in async mode — `WithImagePipeline` is deliberately the
simple synchronous one, because it is correct for the common case (a user
uploading their own avatar or a cover image) and needs no queue.

## Validation

Uploads are bounded and content-checked before anything is stored:

- **Size** is capped at `file.MaxProcessFileSize` (32 MiB), and the
  multipart parser buffers at most `crud.MaxMultipartMemory` (32 MiB) in
  memory before spilling to a temp file. An oversize body returns
  `file.ErrFileFieldTooLarge` without reading past the limit.
- **Content shape** is sniffed from the bytes — never the filename or the
  client's `Content-Type`, both of which an attacker controls. SVG/XML,
  HTML, and executable magic bytes (PE, ELF, Mach-O) are rejected with
  `file.ErrFileFieldUnsafeContent`.
- **Required** is enforced from the field declaration, like any other
  field.

These are global limits, not per-field ones: there is no per-field byte
cap or MIME allow-list today. To bound a specific field more tightly, use
a `BeforeCreate` hook or an `entity.ValidatorFunc` — both run before the
row is written — or set `imagefield.Config.MaxPixels` to tighten the
decode guard for image fields.

A failing validator returns `400 Bad Request` with a `fields` map
identifying the offending field.

Uploaded filenames are sanitized to a safe storage key (path
separators and control characters stripped, length capped at
`MaxFilenameBytes` on a UTF-8 rune boundary). `SanitizeFilename`
bounds the *input* it inspects to `SanitizeFilenameInputBound`
(`4 × MaxFilenameBytes`) so a multi-megabyte attacker-supplied
filename can't force unbounded pre-truncation work (DoS).

## Storage backends

`upload.Storage` is the interface:

```go
type Storage interface {
    Save(ctx context.Context, key string, r io.Reader) error
    Delete(ctx context.Context, key string) error
    Get(ctx context.Context, key string) (io.ReadCloser, error)
    Exists(ctx context.Context, key string) (bool, error)
}
```

Built-in implementations:

- `upload.NewLocalStorage(dir)` (`core/upload`) — writes files under a
  local directory. It only *stores*; it does not serve. Wire downloads
  with `upload.ServeHandler`, which sniffs the content type, blocks
  traversal (delegated to the backend's key sanitization), and
  neutralizes HTML/SVG to a forced download so an uploaded document
  can't execute as script in a victim's browser (stored-XSS guard).
  Suitable for tests and single-host deployments.
- `battery/storage` — local, S3, and in-memory backends behind the same
  interface, plus a declarative factory registry.
- (Add GCS / Azure adapters in your own code by implementing
  `Storage`.)

## Serving stored files

`LocalStorage` (and the `battery/storage` backends) only *store* bytes;
none of them serves files over HTTP. To download a stored file, mount
`upload.ServeHandler` on the router with a catch-all key:

```go
app.Router().Get("/uploads/{key...}", upload.ServeHandler(storage))
```

`ServeHandler` resolves the key from the `{key...}` wildcard, sniffs the
content type from the first 512 bytes (never trusting the client or the
key), and sets `X-Content-Type-Options: nosniff` on every response.
Scriptable content — HTML or SVG, by sniffed type *or* key extension —
is forced to `application/octet-stream` with
`Content-Disposition: attachment`, so an uploaded document can't execute
as script in a victim's browser (stored-XSS guard). Path-traversal
defense is delegated to the backend's key sanitization; the handler
echoes no filesystem path on any error.

### Range requests and resumable downloads

`Storage.Get` returns an `io.ReadCloser`, which erases seekability — and
`http.ServeContent` needs an `io.ReadSeeker` to answer a `Range:` header.
Backends that hold their bytes locally declare the capability instead:

```go
type RangeGetter interface {
    GetRange(ctx context.Context, key string) (io.ReadSeekCloser, error)
}
```

`ServeHandler` type-asserts for it. When the backend implements it, range
requests get a `206` with `Accept-Ranges: bytes`, so a download interrupted
1.8 GB into a 2 GB file resumes instead of restarting. When it doesn't, the
handler serves whole bodies exactly as before — declining is legal.

| Backend | `RangeGetter` |
| --- | --- |
| `upload.LocalStorage`, `storage.LocalStorage` | yes — already opens an `*os.File` |
| `storage.MemoryStorage` | yes — the bytes are already resident |
| `storage.S3Storage` | no — a network backend would have to buffer the whole object to satisfy `Seek`. Use `WithPresigner` so the transfer bypasses the app entirely. |

Implement it on your own backend only if seeking is genuinely cheap there,
and route key validation through the same code path as `Get` — a capability
that skipped the traversal check would be a path-traversal hole with a
performance justification.

## Content checksums

The `battery/storage` package provides opt-in SHA-256 helpers that wrap
any `Storage` backend — integrity verification, dedup, or
content-addressed keys without touching the backend or buffering the
whole stream:

```go
import "github.com/DonaldMurillo/gofastr/battery/storage"

// Write once: stream is teed through a SHA-256 hasher as it saves.
res, err := storage.SaveWithChecksum(ctx, st, key, body)
// res.Size   -> bytes written
// res.SHA256 -> lowercase hex digest of the stored content

// Later: confirm the object still matches.
if err := storage.VerifyChecksum(ctx, st, key, res.SHA256); err != nil {
    log.Print(err) // wraps storage.ErrChecksumMismatch on mismatch
}
```

`SaveWithChecksum` tees the stream through a SHA-256 hasher while it is
written, so the content is read exactly once and nothing is buffered.
`VerifyChecksum` re-reads the object and compares digests; it accepts
upper- or lowercase hex and returns an error wrapping
`storage.ErrChecksumMismatch` (carrying the key and the `got`/`want`
digests) on a mismatch, the underlying error if the object can't be
read, and a clear validation error if the expected digest isn't 64 hex
characters.

## Common mistakes

- **Forgetting `WithFileStorage`.** Multipart requests on an `Image`/
  `File` entity will error. JSON requests still work — they just can't
  set those fields.
- **Sending a JSON body with a base64 file.** Not supported. Use
  multipart, or store the file out-of-band and PATCH the URL in.
- **Trusting client-supplied URLs.** Multipart writes the URL the
  server gets back from `Storage.Save`, not anything the client sent.
  Don't try to set a file URL via a JSON request expecting the server
  to honour it as-is — that path uses the column verbatim and won't
  validate or upload anything.
- **Camelcasing multipart names.** They are literal column names. Use
  snake_case if your DB columns are snake_case.
- **Expecting sibling columns to appear on their own.** `WithImagePipeline`
  fills `<field>_blurhash` / `<field>_placeholder` / `<field>_variants`
  only when the entity declares them. A missing column is skipped
  silently, so a hash that "isn't saving" is usually an undeclared column.
- **Wiring `WithImagePipeline` without `WithFileStorage`.** Renditions are
  written through the same backend as the original; there is nowhere to
  put them otherwise.
- **Putting an image on a `File` field and expecting renditions.** Only
  `schema.Image` runs the pipeline.
- **Reaching for `framework/image` from `framework/file` or
  `framework/crud`.** Both are below it in the layering, and the edge
  would link every decoder into every app with a CRUD handler. That is why
  `file.ImageDeriver` is an interface and `framework/imagefield`
  implements it.
