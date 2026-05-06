# 010 — Upload Primitive

**Phase:** 1 (Core Primitives) | **Tier:** 2 | **Depends on:** 002, 003

## Goal
Multipart file upload handling with validation, storage backend interface, and local filesystem implementation.

## Deliverables
- [ ] `Storage` interface: `Save(ctx, key, reader) error`, `Delete(ctx, key) error`, `Get(ctx, key) (io.ReadCloser, error)`, `Exists(ctx, key) bool`
- [ ] `LocalStorage` implementation: saves to configured directory
- [ ] `UploadHandler` — parse multipart form, validate files, save via Storage
- [ ] File validation: MIME type check, size limit, extension whitelist
- [ ] Security: no path traversal (sanitize filenames), reject executables by default
- [ ] File metadata struct: OriginalName, Size, MimeType, UploadedAt
- [ ] Image field type: basic metadata (dimensions from image.DecodeConfig)
- [ ] Integration with Router: mount upload endpoint

## Acceptance Criteria
- Rejects files over size limit with 413
- Rejects disallowed MIME types with 415
- Sanitizes filenames — path traversal attempts produce safe names
- Local storage saves to disk, serves relative URLs
