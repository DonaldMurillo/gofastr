# Storage

`battery/storage` backs file uploads with a storage backend. It
re-exports the `upload.Storage` interface (defined in `core/upload`) and
ships two implementations: a local-filesystem backend and an
S3-compatible backend. You construct one and hand it to
`framework.WithFileStorage`.

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

## The Storage interface

<!-- gofastr:compile
import "context"
import "io"
-->
```go
type Storage interface {
	Save(ctx context.Context, key string, r io.Reader) error
	Delete(ctx context.Context, key string) error
	Get(ctx context.Context, key string) (io.ReadCloser, error)
	Exists(ctx context.Context, key string) (bool, error)
}
```

`Save` writes the contents of `r` under `key`; `Get` returns a reader;
`Delete` removes the object; `Exists` reports presence. Keys are opaque
strings the upload layer assigns. A backend rejects a key that escapes
its namespace (path traversal, empty key) with an error; it does not
rewrite the key.

## Local backend

<!-- gofastr:compile
stmt: _ = store
import "github.com/DonaldMurillo/gofastr/battery/storage"
-->
```go
store := storage.NewLocalStorage("./uploads",
	storage.WithPermissions(0o644),
)
```

`NewLocalStorage(baseDir, opts...)` roots storage at `baseDir` (created
if missing). Writes are atomic: data lands in a temp file in the same
directory, then renames into place. Containment is enforced on the
symlink-resolved path at every operation, with the syscalls performed
through an `os.Root` over the resolved root so a symlink planted *after*
resolution is refused by the kernel too: a key that reaches through a
symlinked directory or leaf to a file outside `baseDir` is refused with
a key error instead of read, written, or deleted; errors never carry
the absolute storage path. `WithPermissions` sets the saved file mode;
`WithTempDir` overrides the temp directory used for the atomic write
(a temp dir outside `baseDir` keeps the older resolve-then-open staging
there, because the atomic rename cannot be expressed through the root).

On a case-insensitive or Unicode-normalization-insensitive filesystem
(macOS's default APFS, most CIFS mounts), two spellings of a key resolve
to one file, so `Save` refuses a key that folds onto an existing object
stored under a byte-different spelling — including through a directory
component (`tenanta/new.txt` is refused when `TenantA/` exists) — with
an `ErrInvalidKey` error (HTTP 400 through the upload serve layer)
naming the stored spelling. Re-saving the exact stored key stays an
ordinary overwrite. Objects planted straight onto disk by other tools
with folded spellings follow the filesystem's folding, not the store's.

`LocalStorage` implements `RangeGetter`, so HTTP range requests
(`Range:` bytes) are answered with a `206` from the seekable file
handle. Local is the right default for single-host deployments; under
multiple replicas an upload to replica A is invisible to B unless the
directory is shared storage.

## S3 backend

```go
store := storage.NewS3Storage("my-bucket", "us-east-1",
	storage.WithS3Client(myClient),
	storage.WithPresigner(myPresigner),
)
```

`NewS3Storage(bucket, region, opts...)` returns an `S3Storage`. It talks
to S3-compatible stores through a minimal `S3Client` interface: the AWS
SDK is not imported for you; pass a client via `WithS3Client`, or set a
custom endpoint with `WithS3Endpoint`. Without a client,
`Save` / `Get` / `Delete` / `Exists` cannot run; the constructor does
not dial.

For direct browser uploads and downloads, pass a `Presigner` via
`WithPresigner`. `PresignedGetURL` and `PresignedPutURL` then mint
time-limited URLs that route traffic around the app entirely.
`S3Storage` does not implement `RangeGetter`: a network backend would
have to buffer the whole object to `Seek`; presigned URLs are the
intended path for large transfers.

## Choosing

| Backend       | Range requests        | Multi-replica             | Notes                              |
|---------------|-----------------------|---------------------------|------------------------------------|
| `LocalStorage` | yes (`206`)          | only with a shared volume | atomic writes; single-host default |
| `S3Storage`   | no (use presigned URLs) | yes                     | client + optional presigner injected |

## Common mistakes

- **Pointing multiple replicas at a local directory.** Uploads land on one host and `Get` 404s from the others. Move uploads to S3 (or mount a shared volume) before scaling past one replica.
- **Constructing `S3Storage` without a client.** `NewS3Storage` does not dial; it stores the config. The first `Save` / `Get` fails unless `WithS3Client` supplied an implementation.
- **Expecting `S3Storage` to serve range requests.** It declines `RangeGetter`; a `<video>` or large download piped through the app buffers the whole object. Use `WithPresigner` and hand the browser a presigned URL.
- **Retrying a key the backend rejected.** Backends reject traversal and empty keys with an error; they do not sanitize the key. Generate keys from trusted input (the upload layer does this) and surface the error rather than retrying the same value.
