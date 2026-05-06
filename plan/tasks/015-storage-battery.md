# 015 — Storage Battery

**Phase:** 2 (Batteries) | **Depends on:** 010

## Goal
Pluggable file storage. Interface in core, local filesystem implementation in battery.

## Deliverables
- [ ] `Storage` interface: `Save(ctx, key, reader) (url, error)`, `Delete(ctx, key) error`, `Get(ctx, key) (io.ReadCloser, error)`, `Exists(ctx, key) bool`, `List(ctx, prefix) ([]string, error)`
- [ ] `LocalStorage` implementation: saves to configurable directory, returns relative URLs
- [ ] File organization: subdirectory by date or entity (`uploads/2024/01/abc.jpg`)
- [ ] URL generation: relative for local, presigned URL interface for cloud
- [ ] Cleanup: `DeleteUnused(ctx, knownKeys)` for orphan removal
- [ ] Storage metrics interface: bytes used, file count

## Acceptance Criteria
- Save + Get round-trips file content correctly
- Delete removes file
- Path traversal in key names is prevented
- Subdirectory organization works
