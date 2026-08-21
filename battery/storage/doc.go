/*
Package storage provides file/object storage backends for the upload
battery.

Three concrete backends ship, each constructed directly (there is no
registry or factory indirection):

  - NewLocalStorage(dir, ...) , the local filesystem.
  - NewMemoryStorage(...)     , an in-process store for tests and ephemeral data.
  - NewS3Storage(bucket, reg) , S3 (or S3-compatible) object storage.

All implement the Storage interface (a re-export of upload.Storage), which
covers save / get / delete / list. LocalStorage and MemoryStorage also
implement RangeGetter (re-export of upload.RangeGetter) so HTTP range
requests can be answered; S3Storage declines and is instead paired with a
presigner (WithPresigner) so uploads/downloads bypass the app entirely.

Content checksums: SaveWithChecksum writes an object plus a checksum sidecar
so a later read can detect bit-rot or an interrupted write.

Keys are validated (DefaultKeyValidator) to reject path-traversal and other
forbidden sequences before they reach a backend.
*/
package storage
