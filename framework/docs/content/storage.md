# Storage

`battery/storage` backs file uploads with a storage backend. It
re-exports the `upload.Storage` interface (defined in `core/upload`) and
ships local-filesystem, in-memory, and S3-compatible backends. You construct one and hand it to
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
the absolute storage path. `WithPermissions` sets the saved Unix file mode;
Windows files use protected owner-only ACLs.
Directory ACL changes stop at the storage root, including when Windows path
spellings differ only by letter case.
`WithTempDir` overrides the temp directory used for the atomic write
(a temp dir outside `baseDir` keeps the older resolve-then-open staging
there, because the atomic rename cannot be expressed through the root).

Both `storage.LocalStorage` and `upload.LocalStorage` implement `Close()`.
Close the backend when its owner shuts down, after storage operations finish
and readers returned by `Get` or `GetRange` are closed. This releases the
pinned directory handle, which otherwise prevents Windows from removing the
storage directory.

On a case-insensitive or Unicode-normalization-insensitive filesystem
(macOS's default APFS, most CIFS mounts), two spellings of a key resolve
to one file, so `Save` refuses a key that folds onto an existing object
stored under a byte-different spelling — including through a directory
component (`tenanta/new.txt` is refused when `TenantA/` exists) — with
an `ErrInvalidKey` error (HTTP 400 through the upload serve layer)
naming the stored spelling. The check runs again at the rename that
commits the save, under a lock every local store in the process shares,
so of two overlapping saves under folding spellings the second to
commit is refused rather than overwriting the first. `Delete` refuses a
folded spelling the same way, so erasing a row that names `A/b.png`
cannot remove another user's `a/b.png`. On every operation a key must
also be its own clean form: `a//b`, `a/./b`, `./a`, a trailing `/` and
`.` are refused with `ErrInvalidKey` rather than resolved to the object
they alias. S3 keys are byte-exact and unchanged. Saves from another
process sharing the directory are not serialized. Re-saving the exact
stored key stays an ordinary overwrite. Objects planted straight onto disk by other tools
with folded spellings follow the filesystem's folding, not the store's.
Finding the stored spelling means listing the directory, so the check
lists only where folding is possible: a name with a non-ASCII byte or a
`K`, `;` or `` ` `` (the ASCII targets of Unicode normalization), or one
whose case-swapped spelling opens the same file. On a case-sensitive
filesystem the cost of serving one object no longer grows with the number
of objects beside it; on a folding one each operation still lists every
directory on the key's path.

`LocalStorage` implements `RangeGetter`, so HTTP range requests
(`Range:` bytes) are answered with a `206` from the seekable file
handle. Local is the right default for single-host deployments; under
multiple replicas an upload to replica A is invisible to B unless the
directory is shared storage.

## Memory backend

`storage.NewMemoryStorage()` stores objects in process memory and exposes
seekable reads through `RangeGetter`. `Save` caps each object at 50 MiB.
All operations return a canceled context error before acting; `Save` also
checks cancellation after reading, before replacing the stored object.
Contents disappear when the process exits.

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
