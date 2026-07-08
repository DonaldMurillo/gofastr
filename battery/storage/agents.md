# battery/storage

File / object storage abstraction matching `framework/upload.Storage`.
Three built-in backends: `LocalStorage`, `MemoryStorage` (tests),
`S3Storage` (works with S3, MinIO, R2, anything S3-compatible).

**Use this when** the prompt mentions: file upload, save a file,
attachments, S3, MinIO, "store user uploads", avatar upload, image
hosting.

**Import:** `github.com/DonaldMurillo/gofastr/battery/storage`

**Shape (local disk):**
```go
st := storage.NewLocalStorage("/var/uploads",
    storage.WithPermissions(0o644),
)
app := framework.NewApp(
    framework.WithFileStorage(st),
)
```

**Shape (S3):**
```go
st := storage.NewS3Storage("my-bucket", "us-east-1",
    storage.WithS3Client(client),
    storage.WithS3Endpoint("https://minio.local"), // optional, S3-compat
)
```

**AI-typical anti-pattern** — if you're about to write any of these,
stop and use `LocalStorage` / `S3Storage` instead:
- `os.WriteFile(filepath.Join("uploads", name), body, 0o644)` —
  no path-traversal guard (`name="../../../etc/passwd"`), no temp
  file, breaks on the first multi-host deploy
- `io.Copy` into a `*os.File` whose name comes from the upload's
  `Content-Disposition` header
- A `mkdir uploads` in `main.go` because "we'll move to S3 later"
- Anything that calls `aws-sdk-go` directly when this app's storage
  needs are just put/get/delete by key

Hand the result to `framework.WithFileStorage` — `Image` / `File`
entity fields then upload through it for free, and the S3 swap is
a constructor change.

**Declarative wiring:** `storage.Register(StorageType, factory)` +
`storage.New(t, configMap)` lets host apps select a backend from a
config file at boot.

**Content checksums:** `storage.SaveWithChecksum(ctx, s, key, r)` writes
through any `Storage` while teeing a SHA-256 hasher, returning
`(SaveResult{Size, SHA256}, err)`. `storage.VerifyChecksum(ctx, s, key,
hex)` re-reads and compares, returning `storage.ErrChecksumMismatch`
(wrapped, with got/want digests) on mismatch. Use these for integrity
checks, dedup, or content-addressed keys instead of hand-rolling
`io.TeeReader` + `sha256` around `Save`.
