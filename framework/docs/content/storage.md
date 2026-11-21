# Storage (`battery/storage`)

`battery/storage` backs file uploads (`framework.WithFileStorage`) behind one `Storage` interface (`Save`, `Get`, `Delete`, `Exists`) with local-disk, in-memory, and S3-compatible backends.

## Quickstart

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

## Backends

| Backend | Range requests (`206`) | Multi-replica | Notes |
| --- | --- | --- | --- |
| `storage.NewLocalStorage(dir, opts...)` | Yes (`RangeGetter`) | Shared volume only | Atomic temp-file rename + `os.Root` symlink containment; call `Close()` on shutdown |
| `storage.NewMemoryStorage()` | Yes (`RangeGetter`) | No | Tests only; 50 MiB per-object cap |
| `storage.NewS3Storage(bucket, region, opts...)` | No (use presigned URLs) | Yes | Inject any `S3Client` (`WithS3Client`) and optional `Presigner` (`WithPresigner`) |

<!-- gofastr:compile
stmt: _ = store
import "github.com/DonaldMurillo/gofastr/battery/storage"
-->
```go
store := storage.NewLocalStorage("./uploads",
	storage.WithPermissions(0o644),
)
```

Key safety guarantees across both local and S3 backends:
- Strict key validation (`DefaultKeyValidator`): rejects empty keys, leading `/`, `..`, control bytes, and uncleaned aliases (`a//b`, `./a`, trailing `/`) with `upload.ErrInvalidKey`.
- On case-insensitive/Unicode-folding filesystems (macOS APFS), `LocalStorage` refuses keys that fold onto an existing byte-different spelling so one key cannot overwrite or delete another.
- For large S3 uploads/downloads or video streaming, configure `WithPresigner` to mint time-limited `PresignedPutURL` / `PresignedGetURL` links (positive expiry required).

## Common mistakes

- **Using `LocalStorage` across multiple replicas without a shared volume:** uploads on replica A 404 from replica B; move to `S3Storage`.
- **Constructing `NewS3Storage` without `WithS3Client`:** the constructor does not dial; the first operation fails until a client is supplied.
- **Streaming large S3 objects through Go instead of using `WithPresigner`:** `S3Storage` declines `RangeGetter`; use presigned URLs for big files.
- **Forgetting `store.Close()` on `LocalStorage`:** releases pinned `os.Root` directory handles cleanly (especially important on Windows).
