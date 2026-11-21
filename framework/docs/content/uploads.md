# File uploads

Declare `schema.File` or `schema.Image` fields on an entity (or use `core/upload` directly) to accept multipart uploads, validate MIME types and sizes by content sniffing, store files via `battery/storage`, and serve them safely.

## Quickstart

1. Wire a storage backend (`battery/storage` — local disk for single-node, S3 for multi-replica):

```go
import (
	"github.com/DonaldMurillo/gofastr/battery/storage"
	"github.com/DonaldMurillo/gofastr/framework"
)

store := storage.NewLocalStorage("./uploads")
app := framework.NewApp(
	framework.WithDB(db),
	framework.WithFileStorage(store),
)
```

2. Add a `schema.File` or `schema.Image` field to an entity:

```go
app.Entity("posts", framework.EntityConfig{
	Fields: []schema.Field{
		{Name: "title", Type: schema.String, Required: true},
		{Name: "cover", Type: schema.Image},
		{Name: "attachment", Type: schema.File},
	},
})
```

POST or PATCH the entity with `multipart/form-data`: GoFastr validates the file, writes it under a generated random key in storage, and saves that storage key in the database column. EntityUI and `battery/admin` automatically render file inputs and previews.

## Security rules enforced by default

- **Content sniffing, not filenames:** uploaded bytes are inspected (`http.DetectContentType` / magic bytes) against allowed MIME types. A `.png` containing HTML or script is rejected.
- **SVG restriction:** `schema.Image` rejects `image/svg+xml` by default because inline SVG can execute JavaScript.
- **Size caps:** per-file (default 32 MiB) and total multipart body caps (default 64 MiB) apply before buffering into storage.
- **Safe serving:** `upload.ServeHandler(store)` sets `X-Content-Type-Options: nosniff`, forces `Content-Disposition: attachment` for scriptable types (`html`, `svg`, `xml`, etc.), supports `Range`, and refuses path traversal or directory keys.
- **Access control on downloads:** `framework.WithFileStorage` assigns the storage backend; it does **not** mount a download route or inherit entity access checks. `upload.ServeHandler` streams objects directly by key without looking up an entity row. Mount your download route explicitly, and for private files wrap it with an object-level authorization check (verifying the signed-in caller can read the owning entity/row before delegating to `upload.ServeHandler`):

```go
serve := upload.ServeHandler(store)
app.Router().Get("/files/{key...}", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
	if !callerCanReadFile(r.Context(), r.PathValue("key")) {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	serve(w, r)
}))
```

- **Cleanup:** deleting a record (or erasing a user via `App.EraseUserData`) removes associated stored objects.

## Storage keys on JSON and in-process writes

Multipart requests upload new bytes and mint a fresh storage key. JSON requests (`application/json`) and in-process Go writes (`CreateOne` / `UpdateOne`) cannot carry raw file streams — instead they accept an existing storage key string (or `""` to clear an optional field):

- Valid key syntax alone (`DefaultKeyValidator`: no leading `/`, no `..`, no control bytes) does **not** authorize attaching an arbitrary existing storage key over HTTP. GoFastr's media provenance checks enforce that callers can only attach keys already owned by that record/caller (or uploaded through a verified upload flow) rather than stealing another user's or entity's private storage key.
- Replacing a file or clearing the field schedules deletion of the previous object after the database transaction commits.

## Multipart vs. direct-to-S3 presigned uploads

For small-to-medium uploads (avatars, invoices, attachments), multipart through GoFastr keeps validation and DB writes atomic. For large media files or multi-replica video uploads, use `storage.WithPresigner` on `S3Storage` to issue short-lived `PresignedPutURL` / `PresignedGetURL` links (see [Storage](storage.md)).

## Common mistakes

- **Using `schema.File` / `schema.Image` without calling `framework.WithFileStorage`:** upload routes have nowhere to persist bytes and reject uploads.
- **Storing uploads in `LocalStorage` across multiple replicas:** a file uploaded to replica A returns 404 from replica B. Switch to `S3Storage` or a shared volume before scaling out.
- **Accepting arbitrary `Content-Type` headers from the client:** always rely on GoFastr's byte-level sniffing rather than trusting `multipart.FileHeader.Header`.
- **Serving user-uploaded HTML or SVG inline on your app's origin:** causes stored XSS. Keep `nosniff` and `Content-Disposition: attachment` (or serve from a separate sandboxed origin).
