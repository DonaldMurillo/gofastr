# File uploads

CRUD endpoints accept `multipart/form-data` for entities with `Image`
or `File` fields. Uploads are streamed through the configured
`upload.Storage` backend; only the resulting URL is stored on the
record.

## Wire it up

```go
import "github.com/gofastr/gofastr/core/upload"

storage, _ := upload.NewLocal("./uploads", "/uploads/")
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

Both types behave identically except that the framework emits an
image-aware widget for `Image` fields in the UI host.

## Validation

File-field validators run before storage so an invalid file does not
consume bytes:

- Size limit (set via `EntityConfig.FileField(name).MaxBytes`).
- Allowed MIME types (set via `AllowedMIMETypes`).
- Required (set on the field).

A failing validator returns `400 Bad Request` with a `fields` map
identifying the offending field.

## Storage backends

`upload.Storage` is the interface:

```go
type Storage interface {
    Save(ctx context.Context, key string, body io.Reader) (string, error)
    Delete(ctx context.Context, key string) error
}
```

Built-in implementations live in `core/upload`:

- `upload.NewLocal(dir, urlPrefix)` — writes to a local directory and
  serves files from `urlPrefix`. Suitable for tests and single-host
  deployments.
- (Add S3 / GCS / Azure adapters in your own code by implementing
  `Storage`.)

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
