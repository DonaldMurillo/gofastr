# 029 — File/Image Fields

**Phase:** 3 (Framework) | **Depends on:** 019, 010, 015

## Goal
Image and File field types auto-wire upload handling and storage backend.

## Deliverables
- [ ] Image field type: auto-wire Upload handler + Storage backend
- [ ] File field type: same wiring, different validation (broader MIME types)
- [ ] On create: accept multipart upload, save via Storage, store URL in entity field
- [ ] On update: new upload replaces old file (delete old from storage)
- [ ] On delete: remove file from storage
- [ ] File metadata stored alongside: original name, size, MIME type, dimensions (for images)
- [ ] Image variants: configurable sizes (thumbnail, medium, large) via resize hints
- [ ] Entity rendering: auto-render `<img>` tags, file download links

## Acceptance Criteria
- Upload image via entity create → file saved, URL stored
- Update with new file → old file deleted, new saved
- Delete entity → associated files deleted from storage
- Image metadata includes dimensions
